const $ = (id) => document.getElementById(id);
let state = { config: { tasks: [] }, forwards: {}, apps: {} };
let booted = false;
let localJumps = [];
let jumpRowsKey = "";

const PASS_TOGGLE_SVG = `<svg class="eye" viewBox="0 0 24 24" aria-hidden="true">
  <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" fill="none" stroke="currentColor" stroke-width="1.8"/>
  <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" stroke-width="1.8"/>
</svg>
<svg class="eye-off" viewBox="0 0 24 24" aria-hidden="true">
  <path d="M3 3l18 18" fill="none" stroke="currentColor" stroke-width="1.8"/>
  <path d="M9.9 5.2C10.6 5.1 11.3 5 12 5c6.5 0 10 7 10 7a18.6 18.6 0 0 1-4.2 4.8M6.1 6.6A18.5 18.5 0 0 0 2 12s3.5 7 10 7c1.4 0 2.7-.3 3.9-.8" fill="none" stroke="currentColor" stroke-width="1.8"/>
  <path d="M9.1 9.1a3 3 0 0 0 4.1 4.4" fill="none" stroke="currentColor" stroke-width="1.8"/>
</svg>`;

function jumpsFromConfig(c) {
  const jumps = (c && c.jumps) || [];
  if (jumps.length) return jumps.map((j) => ({ ...j }));
  return [{
    id: "",
    sshHost: (c && c.sshHost) || "",
    sshPort: (c && c.sshPort) || "22",
    user: (c && c.user) || "",
    password: (c && c.remember && c.password) ? c.password : "",
    socksPort: (c && c.socksPort) || "1080",
  }];
}

function mergeJumpDrafts(saved, draft) {
  const out = [];
  const taken = new Set();
  for (const raw of draft || []) {
    const d = { ...raw };
    if (d.id) {
      const s = (saved || []).find((x) => x.id === d.id);
      if (s) {
        out.push({
          ...s,
          sshHost: d.sshHost,
          sshPort: d.sshPort || s.sshPort,
          user: d.user,
          password: d.password,
          socksPort: d.socksPort || s.socksPort,
          name: d.name || s.name,
        });
        taken.add(s.id);
        continue;
      }
    }
    if (d.sshHost && d.user) {
      const s = (saved || []).find((x) => !taken.has(x.id) && x.sshHost === d.sshHost && x.user === d.user && String(x.sshPort || "22") === String(d.sshPort || "22"));
      if (s) {
        out.push({ ...s, ...d, id: s.id });
        taken.add(s.id);
        continue;
      }
    }
    out.push(d);
  }
  for (const s of saved || []) {
    if (s.id && !taken.has(s.id)) out.push(s);
  }
  if (out.length) return out;
  if (saved && saved.length) return saved.slice();
  return draft || [];
}

function applyConfig(c) {
  $("target-host").value = c.targetHost || "";
  $("remember").checked = !!c.remember;
  const saved = jumpsFromConfig(c);
  const draft = document.querySelector(".jump-row") ? collectJumpRows() : [];
  localJumps = draft.length ? mergeJumpDrafts(saved, draft) : saved;
  if (jumpRowsFocused() && document.querySelector(".jump-row")) {
    renderJumps();
    return;
  }
  jumpRowsKey = "";
  rebuildJumpRows(localJumps);
}

function collectJumpRows() {
  return [...document.querySelectorAll(".jump-row")].map((row) => ({
    id: row.dataset.id || "",
    name: row.dataset.name || "",
    sshHost: row.querySelector(".j-host").value.trim(),
    sshPort: row.querySelector(".j-port").value.trim() || "22",
    user: row.querySelector(".j-user").value.trim(),
    password: row.querySelector(".j-pass").value,
    socksPort: row.querySelector(".j-socks").value.trim() || "1080",
  }));
}

function filledJumps() {
  return collectJumpRows().filter((j) => j.sshHost && j.user);
}

function settingsFromForm() {
  const rows = collectJumpRows();
  const s = rows[0] || {};
  return {
    sshHost: s.sshHost || "",
    sshPort: s.sshPort || "22",
    user: s.user || "",
    password: s.password || "",
    socksPort: s.socksPort || "1080",
  };
}

function socksAddr() {
  if (state.socks) return state.socks;
  const row = document.querySelector(".jump-row .j-socks");
  return "127.0.0.1:" + ((row && row.value.trim()) || "1080");
}

function hasApp(name) {
  const apps = state.apps || {};
  return !!(apps[name] && String(apps[name]).trim());
}

function render() {
  const connected = !!state.connected;
  const connecting = !!state.connecting;
  $("btn-ssh").disabled = !connected || !hasApp("ssh");
  $("btn-ssh").title = hasApp("ssh") ? "预览 OpenSSH 配置" : "未检测到本机 OpenSSH";

  const chip = $("socks-chip");
  const host = (state.config && state.config.sshHost) || "";
  const dot = `<span class="chip-dot" aria-hidden="true"></span>`;
  if (connecting) {
    chip.innerHTML = `${dot}<span class="chip-label">正在连接…</span>`;
    chip.className = "chip busy";
  } else if (connected) {
    const hostHTML = host ? `<span class="chip-host">${esc(host)}</span><span class="chip-sep" aria-hidden="true"></span>` : "";
    chip.innerHTML = `${dot}<span class="chip-label">已连接</span>${hostHTML}<span class="chip-socks">${esc(socksAddr())}</span>`;
    chip.className = "chip on";
  } else {
    chip.innerHTML = `${dot}<span class="chip-label">未连接</span>`;
    chip.className = "chip";
  }

  renderLogs();
  renderJumps();
  renderTasks();
}

function syncPaneScroll(el) {
  if (!el) return;
  el.classList.remove("can-scroll");
  const need = el.scrollHeight > el.clientHeight + 1;
  el.classList.toggle("can-scroll", need);
}

function syncPaneScrolls() {
  syncPaneScroll($("log"));
  syncPaneScroll(document.querySelector(".table-wrap"));
}

let lastLogKey = "";

function logLineText(l) {
  return typeof l === "string" ? l : (l.text || "");
}

function logKey(logs) {
  return (logs || []).map((l) => {
    const level = typeof l === "object" ? (l.level || "") : "";
    return level + "\n" + logLineText(l);
  }).join("\0");
}

function logHasSelection() {
  const el = $("log");
  const sel = window.getSelection();
  if (!el || !sel || sel.isCollapsed || !sel.rangeCount) return false;
  return el.contains(sel.anchorNode) && el.contains(sel.focusNode);
}

function jumpRowHTML(j, i) {
  const extra = i > 0 ? " extra" : "";
  const id = esc(j.id || "");
  const name = esc(j.name || "");
  return `<div class="bar jump jump-row${extra}" data-id="${id}" data-name="${name}">
    <label class="w-host"><span class="lab">跳板</span><input class="j-host" spellcheck="false" value="${esc(j.sshHost || "")}" /></label>
    <label class="w-port"><span class="lab">端口</span><input class="j-port" value="${esc(j.sshPort || "22")}" /></label>
    <label class="w-user"><span class="lab">用户</span><input class="j-user" value="${esc(j.user || "")}" /></label>
    <label class="w-user w-pass"><span class="lab">密码</span>
      <span class="pass-wrap">
        <input class="j-pass" type="password" autocomplete="current-password" value="${esc(j.password || "")}" />
        <span class="pass-toggle" role="button" tabindex="-1" title="显示密码" aria-label="显示密码" aria-pressed="false">${PASS_TOGGLE_SVG}</span>
      </span>
    </label>
    <label class="w-port"><span class="lab">SOCKS</span><input class="j-socks" value="${esc(j.socksPort || "1080")}" /></label>
    <button type="button" class="primary" data-act="connect">连接</button>
    <button type="button" data-act="disconnect">断开</button>
    <button type="button" data-act="browser" disabled>打开浏览器</button>
    ${i === 0
      ? `<button type="button" class="jump-pm" data-act="add" title="添加跳板">+</button><button type="button" class="jump-pm" data-act="remove" title="删除一行">-</button>`
      : `<span class="jump-pm-spacer"></span><span class="jump-pm-spacer"></span>`}
  </div>`;
}

function rebuildJumpRows(jumps) {
  const el = $("jump-rows");
  if (!el) return;
  const list = jumps && jumps.length ? jumps : jumpsFromConfig({});
  el.innerHTML = list.map((j, i) => jumpRowHTML(j, i)).join("");
  jumpRowsKey = list.map((j) => j.id || "new").join("\0") + "#" + list.length;
  syncJumpRowButtons();
}

function jumpRowsFocused() {
  const el = document.activeElement;
  return !!(el && el.closest && el.closest("#jump-rows"));
}

function syncJumpRowButtons() {
  const rows = [...document.querySelectorAll(".jump-row")];
  const n = rows.length;
  const cid = state.connectedJumpId || "";
  const connected = !!state.connected;
  const connecting = !!state.connecting;
  rows.forEach((row) => {
    const on = !!(connected && row.dataset.id && row.dataset.id === cid);
    const connectBtn = row.querySelector("[data-act=connect]");
    const discBtn = row.querySelector("[data-act=disconnect]");
    const browBtn = row.querySelector("[data-act=browser]");
    const minusBtn = row.querySelector("[data-act=remove]");
    if (connectBtn) connectBtn.disabled = connecting || on;
    if (discBtn) discBtn.disabled = !on && !connecting;
    if (browBtn) browBtn.disabled = !connected;
    if (minusBtn) minusBtn.disabled = n < 2;
  });
}

function renderJumps() {
  if (!document.querySelector(".jump-row")) {
    rebuildJumpRows(localJumps);
    return;
  }
  const saved = (state.config && state.config.jumps) || [];
  [...document.querySelectorAll(".jump-row")].forEach((row) => {
    if (row.dataset.id) return;
    const host = row.querySelector(".j-host").value.trim();
    const user = row.querySelector(".j-user").value.trim();
    const port = row.querySelector(".j-port").value.trim() || "22";
    const match = saved.find((j) => j.sshHost === host && j.user === user && String(j.sshPort || "22") === port);
    if (match) row.dataset.id = match.id;
  });
  syncJumpRowButtons();
}

function connectPayload(j) {
  if (!j) j = collectJumpRows()[0] || {};
  return {
    id: j.id || "",
    sshHost: j.sshHost || "",
    sshPort: j.sshPort || "22",
    user: j.user || "",
    password: j.password || "",
    socksPort: j.socksPort || "1080",
  };
}

function renderLogs(force) {
  const el = $("log");
  const logs = state.logs || [];
  const key = logKey(logs);
  if (!force && key === lastLogKey) return;
  if (!force && logHasSelection()) return;
  lastLogKey = key;
  el.innerHTML = logs.map((l) => {
    const text = logLineText(l);
    const err = typeof l === "object" ? l.level === "error" : /失败|错误/.test(text);
    return `<div class="${err ? "err" : ""}">${esc(text)}</div>`;
  }).join("");
  el.scrollTop = el.scrollHeight;
  requestAnimationFrame(syncPaneScrolls);
}

function renderTasks() {
  const box = $("tasks");
  const tasks = (state.config && state.config.tasks) || [];
  const connected = !!state.connected;
  if (!tasks.length) {
    box.innerHTML = '<tr><td class="empty" colspan="5">还没有固定转发。点添加填写本机端口和目标，先连跳板再点 Xshell / SecureCRT。</td></tr>';
    requestAnimationFrame(syncPaneScrolls);
    return;
  }
  box.innerHTML = tasks.map((t, i) => {
    const on = !!(t.id && state.forwards && state.forwards[t.id]);
    const dest = (t.user ? esc(t.user) + "@" : "") + esc(t.remoteHost) + ":" + esc(t.remotePort);
    const xDisabled = !connected || !hasApp("xshell") ? "disabled" : "";
    const cDisabled = !connected || !hasApp("securecrt") ? "disabled" : "";
    const xTitle = hasApp("xshell") ? "经本机端口转发打开 Xshell" : "未检测到 Xshell";
    const cTitle = hasApp("securecrt") ? "经本机端口转发打开 SecureCRT" : "未检测到 SecureCRT";
    return `<tr data-i="${i}">
      <td><span class="badge ${on ? "on" : ""}">${on ? "转发中" : "未启动"}</span></td>
      <td>127.0.0.1:${esc(t.localPort)}</td>
      <td>${dest}</td>
      <td>${esc(t.name || "")}</td>
      <td class="c-op"><div class="ops">
        <button data-act="xshell" ${xDisabled} title="${xTitle}">Xshell</button>
        <button data-act="securecrt" ${cDisabled} title="${cTitle}">SecureCRT</button>
        <button data-act="edit">编辑</button>
        <button data-act="del">删除</button>
      </div></td>
    </tr>`;
  }).join("");
  requestAnimationFrame(syncPaneScrolls);
}

function esc(s) {
  return String(s || "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

async function api(path, opt) {
  const r = await fetch(path, opt);
  if (!r.ok) throw new Error(await r.text());
  const t = await r.text();
  return t ? JSON.parse(t) : {};
}

async function refresh() {
  try {
    state = await api("/api/state");
    if (!booted && state.config) {
      applyConfig(state.config);
      booted = true;
    }
    render();
    if (state.openSSH) {
      openSSHDialog();
    }
  } catch (e) {}
}

function copyTextFallback(text) {
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.left = "-9999px";
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand("copy");
  } finally {
    ta.remove();
  }
}

function copyText(s) {
  const text = String(s || "");
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text).catch(() => copyTextFallback(text));
  }
  copyTextFallback(text);
  return Promise.resolve();
}

function sshBody() {
  return {
    host: $("target-host").value.trim(),
  };
}

async function connectJump(j) {
  if (state.connecting) return;
  await api("/api/connect", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(connectPayload(j)) });
}

function isEnterKey(ev) {
  return ev.key === "Enter" || ev.code === "Enter" || ev.code === "NumpadEnter" || ev.keyCode === 13;
}

$("jump-form").addEventListener("keydown", (ev) => {
  if (!isEnterKey(ev)) return;
  ev.preventDefault();
  ev.stopPropagation();
  const row = ev.target.closest(".jump-row");
  connectJump(row ? collectJumpRows()[[...document.querySelectorAll(".jump-row")].indexOf(row)] : null);
}, true);
$("jump-form").addEventListener("submit", (ev) => {
  ev.preventDefault();
  const row = ev.submitter && ev.submitter.closest(".jump-row");
  if (row) {
    const i = [...document.querySelectorAll(".jump-row")].indexOf(row);
    connectJump(collectJumpRows()[i]);
    return;
  }
  connectJump(collectJumpRows()[0]);
});
$("jump-rows").addEventListener("click", async (ev) => {
  const toggle = ev.target.closest(".pass-toggle");
  if (toggle) {
    const input = toggle.closest(".pass-wrap").querySelector(".j-pass");
    const on = input.type === "password";
    input.type = on ? "text" : "password";
    toggle.classList.toggle("on", on);
    toggle.title = on ? "隐藏密码" : "显示密码";
    toggle.setAttribute("aria-label", toggle.title);
    toggle.setAttribute("aria-pressed", on ? "true" : "false");
    return;
  }
  const btn = ev.target.closest("button");
  if (!btn) return;
  const row = btn.closest(".jump-row");
  if (!row) return;
  const act = btn.dataset.act;
  const i = [...document.querySelectorAll(".jump-row")].indexOf(row);
  const j = collectJumpRows()[i];
  try {
    if (act === "connect") await connectJump(j);
    if (act === "disconnect") await api("/api/disconnect", { method: "POST" });
    if (act === "browser") await api("/api/browser", { method: "POST" });
    if (act === "add") addJumpRow();
    if (act === "remove") await removeLastJumpRow();
  } catch (e) {
    alert(e.message);
  }
});

function addJumpRow() {
  localJumps = collectJumpRows();
  localJumps.push({ id: "", sshHost: "", sshPort: "22", user: "", password: "", socksPort: "1080" });
  rebuildJumpRows(localJumps);
}
async function removeLastJumpRow() {
  const jumps = collectJumpRows();
  if (jumps.length < 2) return;
  jumps.pop();
  localJumps = jumps;
  rebuildJumpRows(localJumps);
  const filled = jumps.filter((x) => x.sshHost && x.user);
  if (filled.length) await saveJumps(filled);
}
async function openSSHDialog() {
  try {
    const r = await api("/api/ssh-config", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(sshBody()) });
    $("ssh-path").textContent = "文件：" + (r.path || "");
    $("ssh-snippet").value = r.snippet || "";
    $("ssh-mask").classList.remove("hidden");
    $("ssh-save").onclick = async () => {
      try {
        await api("/api/ssh-config/save", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ snippet: $("ssh-snippet").value }) });
        $("ssh-mask").classList.add("hidden");
      } catch (e) { alert(e.message); }
    };
    $("ssh-cancel").onclick = () => $("ssh-mask").classList.add("hidden");
  } catch (e) { alert(e.message); }
}
$("btn-ssh").onclick = () => openSSHDialog();
$("btn-cmd").onclick = async () => {
  try { await api("/api/shell", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ app: "cmd" }) }); }
  catch (e) { alert(e.message); }
};
$("socks-chip").onclick = async () => {
  if (!state.connected) return;
  await copyText("socks5://" + socksAddr());
};

function hideLogMenu() {
  $("log-menu").classList.add("hidden");
}
function logMenuText() {
  if (logHasSelection()) return String(window.getSelection());
  return (state.logs || []).map(logLineText).join("\n");
}
function placeLogMenu(x, y) {
  const menu = $("log-menu");
  menu.classList.remove("hidden");
  const pad = 4;
  const maxX = Math.max(pad, window.innerWidth - menu.offsetWidth - pad);
  const maxY = Math.max(pad, window.innerHeight - menu.offsetHeight - pad);
  menu.style.left = Math.max(pad, Math.min(x, maxX)) + "px";
  menu.style.top = Math.max(pad, Math.min(y, maxY)) + "px";
}
document.querySelector(".dock").addEventListener("contextmenu", (ev) => {
  ev.preventDefault();
  ev.stopPropagation();
  placeLogMenu(ev.clientX, ev.clientY);
});
$("log-menu").addEventListener("contextmenu", (ev) => ev.preventDefault());
$("log-menu").addEventListener("mousedown", (ev) => ev.stopPropagation());
$("log-menu").addEventListener("click", async (ev) => {
  const btn = ev.target.closest("button");
  if (!btn) return;
  const act = btn.dataset.act;
  hideLogMenu();
  try {
    if (act === "copy") await copyText(logMenuText());
    if (act === "clear") {
      window.getSelection().removeAllRanges();
      state = await api("/api/logs/clear", { method: "POST" });
      renderLogs(true);
    }
  } catch (e) {
    alert(e.message);
  }
});
document.addEventListener("mousedown", (ev) => {
  const menu = $("log-menu");
  if (menu.classList.contains("hidden") || menu.contains(ev.target)) return;
  hideLogMenu();
});
document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape") hideLogMenu();
});
$("log").addEventListener("scroll", hideLogMenu);

$("btn-add").onclick = () => editTask({ name: "", user: "", remoteHost: "", remotePort: "22", localPort: nextLocalPortFromState() });

async function saveJumps(jumps) {
  const draft = collectJumpRows();
  const hasDraft = draft.some((j) => !j.sshHost || !j.user) || draft.length > (jumps || []).length;
  state = await api("/api/jumps", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(jumps) });
  localJumps = mergeJumpDrafts(jumpsFromConfig(state.config), draft);
  if (hasDraft || jumpRowsFocused()) {
    renderJumps();
    renderLogs();
    renderTasks();
    return;
  }
  rebuildJumpRows(localJumps);
  render();
}

$("tasks").onclick = async (ev) => {
  const btn = ev.target.closest("button");
  if (!btn) return;
  const row = ev.target.closest("tr");
  if (!row || row.dataset.i == null) return;
  const i = Number(row.dataset.i);
  const t = state.config.tasks[i];
  const act = btn.dataset.act;
  try {
    if (act === "xshell" || act === "securecrt") {
      state = await api("/api/task/" + t.id + "/" + act, { method: "POST" });
      render();
    }
    if (act === "edit") editTask(t, i);
    if (act === "del") {
      const tasks = state.config.tasks.filter((_, j) => j !== i);
      await saveTasks(tasks);
    }
  } catch (e) {
    alert(e.message);
  }
};

function editTask(t, index) {
  t = { ...t };
  $("edit-fields").innerHTML = [
    ["localPort", "本机端口", t.localPort || "10022"],
    ["remoteHost", "目标主机", t.remoteHost || ""],
    ["remotePort", "目标端口", t.remotePort || "22"],
    ["user", "用户（可空，空则打开后自行输入）", t.user || ""],
    ["name", "备注（可空）", t.name || ""],
  ].map(([id, lab, val]) => `<label>${lab}<input id="e-${id}" value="${esc(val)}" /></label>`).join("");
  $("edit-mask").classList.remove("hidden");
  $("edit-ok").onclick = async () => {
    t.name = $("e-name").value.trim();
    t.user = $("e-user").value.trim();
    t.localPort = $("e-localPort").value.trim();
    t.remoteHost = $("e-remoteHost").value.trim();
    t.remotePort = $("e-remotePort").value.trim();
    const tasks = (state.config.tasks || []).slice();
    if (typeof index === "number") tasks[index] = t;
    else tasks.push(t);
    $("edit-mask").classList.add("hidden");
    try { await saveTasks(tasks); } catch (e) { alert(e.message); }
  };
  $("edit-cancel").onclick = () => $("edit-mask").classList.add("hidden");
}

function nextLocalPortFromState() {
  const used = new Set(((state.config && state.config.tasks) || []).map((t) => String(parseInt(t.localPort, 10))));
  for (let p = 10022; p < 60000; p++) {
    if (!used.has(String(p))) return String(p);
  }
  return "10022";
}

async function saveTasks(tasks) {
  state = await api("/api/tasks", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(tasks) });
  render();
}

function persistTarget() {
  api("/api/target", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      targetHost: $("target-host").value.trim(),
    }),
  }).catch(() => {});
}

["target-host"].forEach((id) => {
  $(id).addEventListener("change", persistTarget);
});

function rememberBody(on) {
  return {
    remember: on,
    ...settingsFromForm(),
    targetHost: $("target-host").value.trim(),
  };
}

document.querySelector(".remember-box").addEventListener("click", (ev) => ev.stopPropagation());
$("remember").addEventListener("click", (ev) => ev.stopPropagation());
$("remember").addEventListener("change", async () => {
  try {
    const on = $("remember").checked;
    if (on) {
      const filled = filledJumps();
      if (filled.length) await saveJumps(filled);
    }
    state = await api("/api/remember", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(rememberBody(on)),
    });
    applyConfig(state.config);
    render();
  } catch (e) {
    alert(e.message);
  }
});
$("jump-rows").addEventListener("change", () => {
  if (!$("remember").checked) return;
  const filled = filledJumps();
  if (!filled.length) return;
  saveJumps(filled).catch(() => {});
});

(function setupSplits() {
  const log = $("log");
  const tableSplit = $("split-table");
  const logSplit = $("split-log");
  const table = document.querySelector(".static-section");
  if (!log || !tableSplit || !logSplit || !table) return;
  const LOG_MIN = 80, LOG_MAX = 480, TABLE_MIN = 120;
  const saved = Number(localStorage.getItem("portfwd-log-h"));
  if (saved >= LOG_MIN && saved <= LOG_MAX) log.style.height = saved + "px";

  function logH() {
    return log.getBoundingClientRect().height;
  }
  function clampLog(h, growWindow) {
    if (!growWindow) {
      const extra = table.getBoundingClientRect().height - TABLE_MIN;
      h = Math.min(h, logH() + extra);
    }
    return Math.max(LOG_MIN, Math.min(LOG_MAX, h));
  }
  function applyLog(h) {
    log.style.height = h + "px";
    localStorage.setItem("portfwd-log-h", String(Math.round(h)));
    requestAnimationFrame(syncPaneScrolls);
  }
  window.addEventListener("resize", () => requestAnimationFrame(syncPaneScrolls));
  if (typeof ResizeObserver !== "undefined") {
    const ro = new ResizeObserver(() => syncPaneScrolls());
    ro.observe(log);
    document.querySelectorAll(".table-wrap").forEach((w) => ro.observe(w));
  }
  function bind(grip, mode) {
    let lastY = 0, drag = false;
    grip.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      drag = true;
      lastY = ev.clientY;
      grip.classList.add("drag");
      grip.setPointerCapture(ev.pointerId);
    });
    grip.addEventListener("pointermove", (ev) => {
      if (!drag) return;
      const dy = ev.clientY - lastY;
      lastY = ev.clientY;
      if (mode === "log-down") {
        if (dy <= 0) return;
        const next = clampLog(logH() + dy, true);
        const applied = next - logH();
        if (applied <= 0) return;
        applyLog(next);
        try { window.resizeBy(0, applied); } catch (e) {}
        return;
      }
      applyLog(clampLog(logH() - dy, false));
    });
    const end = () => {
      if (!drag) return;
      drag = false;
      grip.classList.remove("drag");
    };
    grip.addEventListener("pointerup", end);
    grip.addEventListener("pointercancel", end);
  }
  bind(tableSplit, "table");
  bind(logSplit, "log-down");
})();

refresh();
setInterval(refresh, 800);
