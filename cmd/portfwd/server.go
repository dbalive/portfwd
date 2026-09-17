package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

type hub struct {
	mu              sync.Mutex
	cfg             FileConfig
	tun             *Tunnel
	ctx             context.Context
	cancel          context.CancelFunc
	gen             int
	logs            []logEntry
	connected       bool
	connecting      bool
	forwards        map[string]string
	lastErr         string
	shutdown        chan struct{}
	shutOnce        sync.Once
	openSSH         bool
	connectedJumpID string
}

type logEntry struct {
	Text  string `json:"text"`
	Level string `json:"level"`
}

func newLogEntry(line, level string) logEntry {
	if level == "" {
		level = "info"
	}
	return logEntry{Text: time.Now().Format("15:04:05") + "  " + line, Level: level}
}

type publicState struct {
	Config          FileConfig        `json:"config"`
	Connected       bool              `json:"connected"`
	Connecting      bool              `json:"connecting"`
	SOCKS           string            `json:"socks"`
	Logs            []logEntry        `json:"logs"`
	Forwards        map[string]string `json:"forwards"`
	Error           string            `json:"error"`
	Exe             string            `json:"exe"`
	ProxyCommand    string            `json:"proxyCommand"`
	SSHExample      string            `json:"sshExample"`
	SSHConfig       string            `json:"sshConfig"`
	Apps            detectedApps      `json:"apps"`
	OpenSSH         bool              `json:"openSSH"`
	ConnectedJumpID string            `json:"connectedJumpId"`
}

func newHub() *hub {
	return &hub{
		cfg:      loadConfig(),
		forwards: map[string]string{},
		shutdown: make(chan struct{}),
		logs:     []logEntry{newLogEntry("连上跳板后，用浏览器、本机 SSH 或 Xshell / SecureCRT 走同一个 SOCKS。", "info")},
	}
}

func (h *hub) log(line string) {
	h.addLog(line, "info")
}

func (h *hub) logErr(line string) {
	h.addLog(line, "error")
}

func (h *hub) addLog(line, level string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, newLogEntry(line, level))
	if len(h.logs) > 200 {
		h.logs = h.logs[len(h.logs)-200:]
	}
}

func (h *hub) clearLogs() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = nil
}

func (h *hub) state() publicState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked(false)
}

func (h *hub) takeState() publicState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked(true)
}

func (h *hub) snapshotLocked(takeOpenSSH bool) publicState {
	socks := ""
	if h.tun != nil {
		socks = h.tun.SOCKSAddr()
	}
	if socks == "" {
		port, err := parsePort(h.cfg.SOCKSPort)
		if err != nil {
			port = "1080"
		}
		socks = "127.0.0.1:" + port
	}
	fwd := map[string]string{}
	for k, v := range h.forwards {
		fwd[k] = v
	}
	logs := append([]logEntry(nil), h.logs...)
	cfg := h.cfg
	cfg.Tasks = append([]Task(nil), h.cfg.Tasks...)
	cfg.Jumps = append([]Jump(nil), h.cfg.Jumps...)
	exe := currentExe()
	openSSH := false
	if takeOpenSSH {
		openSSH = h.openSSH
		h.openSSH = false
	}
	return publicState{
		Config:          cfg,
		Connected:       h.connected,
		Connecting:      h.connecting,
		SOCKS:           socks,
		Logs:            logs,
		Forwards:        fwd,
		Error:           h.lastErr,
		Exe:             exe,
		ProxyCommand:    sshProxyCommand(exe, socks),
		SSHExample:      sshExample(exe, socks),
		SSHConfig:       sshConfigSnippet(exe, socks),
		Apps:            detectApps(),
		OpenSSH:         openSSH,
		ConnectedJumpID: h.connectedJumpID,
	}
}

func (h *hub) requestOpenSSH() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openSSH = true
}

func (h *hub) connectionFlags() (connected, connecting bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connected, h.connecting
}

func (h *hub) connect(s Settings) {
	h.mu.Lock()
	if h.connecting {
		h.mu.Unlock()
		return
	}
	if h.connected {
		h.mu.Unlock()
		h.stopTunnel(false)
		h.mu.Lock()
	}
	h.connecting = true
	h.gen++
	gen := h.gen
	h.lastErr = ""
	h.cfg, _ = upsertJumpFromSettings(h.cfg, s)
	s.JumpID = h.cfg.ActiveJumpID
	s.SSHHost = h.cfg.SSHHost
	s.SSHPort = h.cfg.SSHPort
	s.User = h.cfg.User
	if strings.TrimSpace(s.Password) == "" {
		s.Password = h.cfg.Password
	} else {
		h.cfg.Password = s.Password
	}
	s.SOCKSPort = h.cfg.SOCKSPort
	pendingID := h.cfg.ActiveJumpID
	_ = saveConfig(h.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	h.cancel = cancel
	h.mu.Unlock()

	tun, err := StartSOCKS(ctx, Config{
		Settings: s,
		Log:      h.log,
	})

	h.mu.Lock()
	superseded := h.gen != gen
	h.connecting = false
	if superseded {
		h.mu.Unlock()
		if tun != nil {
			tun.Close()
		}
		return
	}
	if err != nil {
		h.lastErr = err.Error()
		h.connected = false
		h.connectedJumpID = ""
		h.tun = nil
		h.mu.Unlock()
		h.logErr("失败: " + err.Error())
		return
	}
	h.tun = tun
	h.connected = true
	h.connectedJumpID = pendingID
	h.lastErr = ""
	h.forwards = map[string]string{}
	h.mu.Unlock()
	go h.watchTunnel(gen, tun)
	h.log("已连跳板 " + s.SSHHost + "。固定端口转发在下方点启动即可，不必使用 SOCKS。OpenSSH 请点「配置 SSH」后再手动连接。")
}

func (h *hub) watchTunnel(gen int, tun *Tunnel) {
	if tun == nil {
		return
	}
	tun.WaitDead()
	h.mu.Lock()
	if h.gen != gen || h.tun != tun {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.logErr("跳板转发超时或 SSH 已断开。请关掉多余的代理浏览器窗口后重新连接。")
	h.disconnect()
}

func (h *hub) disconnect() {
	h.stopTunnel(true)
}

func (h *hub) stopTunnel(logMsg bool) {
	h.mu.Lock()
	h.gen++
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	tun := h.tun
	h.tun = nil
	h.connected = false
	h.connectedJumpID = ""
	h.forwards = map[string]string{}
	h.mu.Unlock()
	tun.Close()
	if logMsg {
		h.log("已断开")
	}
	if err := clearOpenSSHConfig(sshUserConfigPath()); err != nil {
		h.logErr("清理 OpenSSH 配置失败：" + err.Error())
	}
}

func (h *hub) requestShutdown() {
	h.shutOnce.Do(func() {
		if err := clearOpenSSHConfig(sshUserConfigPath()); err != nil {
			h.logErr("退出时清理 OpenSSH 配置失败：" + err.Error())
		}
		close(h.shutdown)
		if quitUILoop != nil {
			quitUILoop()
		}
	})
}

func (h *hub) saveTarget(host, user, port string) {
	h.mu.Lock()
	h.cfg.TargetHost = strings.TrimSpace(host)
	h.cfg.TargetUser = strings.TrimSpace(user)
	if strings.TrimSpace(port) != "" {
		h.cfg.TargetPort = strings.TrimSpace(port)
	}
	cfg := h.cfg
	h.mu.Unlock()
	_ = saveConfig(cfg)
}

func (h *hub) saveTasks(tasks []Task) error {
	for i := range tasks {
		if err := tasks[i].Validate(); err != nil {
			return err
		}
		if tasks[i].ID == "" {
			tasks[i].ID = newID()
		}
	}
	if err := uniqueLocalPorts(tasks); err != nil {
		return err
	}
	h.mu.Lock()
	h.cfg.Tasks = tasks
	cfg := h.cfg
	h.mu.Unlock()
	return saveConfig(cfg)
}

func (h *hub) saveJumps(jumps []Jump) error {
	out := make([]Jump, 0, len(jumps))
	for _, j := range jumps {
		j = j.Normalize()
		if err := j.Validate(); err != nil {
			return err
		}
		if j.ID == "" {
			j.ID = newID()
		}
		out = append(out, j)
	}
	h.mu.Lock()
	connectedID := h.connectedJumpID
	h.cfg.Jumps = out
	if _, ok := jumpByID(out, h.cfg.ActiveJumpID); !ok {
		if len(out) > 0 {
			h.cfg.ActiveJumpID = out[0].ID
		} else {
			h.cfg.ActiveJumpID = ""
		}
	}
	h.cfg = syncFlatFromActive(h.cfg)
	still := false
	for _, j := range out {
		if j.ID == connectedID {
			still = true
			break
		}
	}
	cfg := h.cfg
	drop := h.connected && !still
	h.mu.Unlock()
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if drop {
		h.disconnect()
	}
	return nil
}

type rememberReq struct {
	Remember   bool   `json:"remember"`
	SSHHost    string `json:"sshHost"`
	SSHPort    string `json:"sshPort"`
	User       string `json:"user"`
	Password   string `json:"password"`
	SOCKSPort  string `json:"socksPort"`
	TargetHost string `json:"targetHost"`
}

func (h *hub) setRemember(in rememberReq) {
	h.mu.Lock()
	oldTasks := append([]Task(nil), h.cfg.Tasks...)
	if in.Remember {
		h.cfg.Remember = true
		s := Settings{
			JumpID:    h.cfg.ActiveJumpID,
			SSHHost:   strings.TrimSpace(in.SSHHost),
			SSHPort:   strings.TrimSpace(in.SSHPort),
			User:      strings.TrimSpace(in.User),
			Password:  in.Password,
			SOCKSPort: strings.TrimSpace(in.SOCKSPort),
		}
		h.cfg, _ = upsertJumpFromSettings(h.cfg, s)
		h.cfg.TargetHost = strings.TrimSpace(in.TargetHost)
		cfg := h.cfg
		h.mu.Unlock()
		_ = saveConfig(cfg)
		return
	}
	h.cfg = applyRememberOff(h.cfg)
	cfg := h.cfg
	h.mu.Unlock()
	for _, t := range oldTasks {
		h.stopForward(t)
	}
	_ = saveConfig(cfg)
}

func (h *hub) taskByID(id string) (Task, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.cfg.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func (h *hub) startForward(t Task) error {
	if err := t.Validate(); err != nil {
		return err
	}
	port, err := parsePort(t.LocalPort)
	if err != nil {
		return err
	}
	h.mu.Lock()
	tun := h.tun
	h.mu.Unlock()
	if tun == nil {
		return errNotConnected
	}
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("任务缺少编号，请先保存")
	}
	if err := tun.AddLocal(port, t.RemoteAddr()); err != nil {
		return err
	}
	h.mu.Lock()
	h.forwards[t.ID] = t.RemoteAddr()
	h.mu.Unlock()
	h.log(t.ListenAddr() + " -> " + t.RemoteAddr())
	return nil
}

func (h *hub) stopForward(t Task) {
	port, err := parsePort(t.LocalPort)
	if err != nil {
		port = strings.TrimSpace(t.LocalPort)
	}
	h.mu.Lock()
	tun := h.tun
	h.mu.Unlock()
	if tun != nil {
		tun.RemoveLocal(port)
	}
	h.mu.Lock()
	delete(h.forwards, t.ID)
	delete(h.forwards, port)
	h.mu.Unlock()
	h.log("已停止 " + t.ListenAddr())
}

func (h *hub) openBrowser() error {
	h.mu.Lock()
	connected := h.connected
	socks := ""
	if h.tun != nil {
		socks = h.tun.SOCKSAddr()
	}
	h.mu.Unlock()
	if !connected || socks == "" {
		return errNotConnected
	}
	if err := startProxiedBrowser(socks); err != nil {
		if errors.Is(err, errBrowserMissing) {
			copied := copyToClipboard("socks5://"+socks) == nil
			return fmt.Errorf("%s", browserMissingMessage(socks, copied))
		}
		return err
	}
	h.log("已打开代理浏览器")
	return nil
}

func (h *hub) openTerminal(in terminalReq) error {
	h.mu.Lock()
	connected := h.connected
	h.mu.Unlock()
	if !connected {
		return errNotConnected
	}
	app := strings.ToLower(strings.TrimSpace(in.App))
	if app == "ssh" {
		return fmt.Errorf("请点击「配置 SSH」写入配置，再手动打开 CMD 连接")
	}
	return fmt.Errorf("请在下方固定端口转发中填写本机端口后，用行内 Xshell / SecureCRT 打开")
}

func (h *hub) openTaskTerminal(t Task, app string) error {
	app = strings.ToLower(strings.TrimSpace(app))
	if app != "xshell" && app != "securecrt" {
		return fmt.Errorf("不支持的终端")
	}
	h.mu.Lock()
	_, running := h.forwards[t.ID]
	h.mu.Unlock()
	if !running {
		if err := h.startForward(t); err != nil {
			return err
		}
	}
	apps := detectApps()
	exe, name := apps.Xshell, "Xshell"
	if app == "securecrt" {
		exe, name = apps.SecureCRT, "SecureCRT"
	}
	if exe == "" {
		return fmt.Errorf("未安装 %s", name)
	}
	req := terminalReq{App: app, Host: "127.0.0.1", Port: t.LocalPort, User: strings.TrimSpace(t.User)}
	if err := launchTerminal(exe, req); err != nil {
		return err
	}
	h.log("已打开 " + name + " → 127.0.0.1:" + t.LocalPort + " → " + t.RemoteAddr())
	return nil
}

type sshPrepareResult struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Command string `json:"command"`
}

func (h *hub) prepareSSH(in terminalReq) (sshPrepareResult, error) {
	h.mu.Lock()
	connected := h.connected
	socks := ""
	if h.tun != nil {
		socks = h.tun.SOCKSAddr()
	}
	if socks == "" {
		port := strings.TrimSpace(h.cfg.SOCKSPort)
		if port == "" {
			port = "1080"
		}
		socks = "127.0.0.1:" + port
	}
	host := strings.TrimSpace(in.Host)
	if host == "" {
		host = h.cfg.TargetHost
	}
	h.mu.Unlock()
	if !connected {
		return sshPrepareResult{}, errNotConnected
	}
	spec, err := parseSSHHostSpec(host)
	if err != nil {
		return sshPrepareResult{}, err
	}
	apps := detectApps()
	if apps.SSH == "" {
		return sshPrepareResult{}, errNoOpenSSH
	}
	h.mu.Lock()
	h.cfg.TargetHost = host
	cfg := h.cfg
	h.mu.Unlock()
	_ = saveConfig(cfg)
	path := sshUserConfigPath()
	snippet := sshManagedBlockSpec(spec, "", "", currentExe(), socks)
	return sshPrepareResult{
		Path:    path,
		Snippet: snippet,
		Command: "",
	}, nil
}

func (h *hub) saveSSHSnippet(snippet string) error {
	if detectApps().SSH == "" {
		return errNoOpenSSH
	}
	if sshWriteHook != nil {
		sshWriteHook()
	}
	path := sshUserConfigPath()
	sshConfigMu.Lock()
	defer sshConfigMu.Unlock()
	h.mu.Lock()
	connected := h.connected
	h.mu.Unlock()
	if !connected {
		return errNotConnected
	}
	if err := writeOpenSSHSnippetLocked(path, snippet); err != nil {
		return err
	}
	h.log("已保存 OpenSSH 配置 " + path)
	return nil
}

var sshWriteHook func()

func (h *hub) writeSSHFromSaved() error {
	h.mu.Lock()
	connected := h.connected
	host := strings.TrimSpace(h.cfg.TargetHost)
	socks := ""
	if h.tun != nil {
		socks = h.tun.SOCKSAddr()
	}
	if socks == "" {
		port := strings.TrimSpace(h.cfg.SOCKSPort)
		if port == "" {
			port = "1080"
		}
		socks = "127.0.0.1:" + port
	}
	h.mu.Unlock()
	if !connected {
		return errNotConnected
	}
	if host == "" {
		return errNeedSSHHost
	}
	spec, err := parseSSHHostSpec(host)
	if err != nil {
		return err
	}
	if detectApps().SSH == "" {
		return errNoOpenSSH
	}
	snippet := sshManagedBlockSpec(spec, "", "", currentExe(), socks)
	return h.saveSSHSnippet(snippet)
}

func (h *hub) openShell(kind string) error {
	if err := openInteractiveShell(kind); err != nil {
		return err
	}
	h.log("已打开 " + kind)
	return nil
}

var errNotConnected = errString("尚未连接代理")

type errString string

func (e errString) Error() string { return string(e) }

func startServer() (addr string, h *hub, wait, closer func(), err error) {
	h = newHub()
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return "", nil, nil, nil, err
	}
	files := http.FileServer(http.FS(sub))
	mux := http.NewServeMux()
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		ico, err := appIconICO()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(ico)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.takeState())
	})
	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in Settings
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		go h.connect(in)
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		h.disconnect()
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/logs/clear", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		h.clearLogs()
		writeJSON(w, h.state())
	})
	mux.HandleFunc("/api/browser", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		if err := h.openBrowser(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in struct {
			TargetHost string `json:"targetHost"`
			TargetUser string `json:"targetUser"`
			TargetPort string `json:"targetPort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		h.saveTarget(in.TargetHost, in.TargetUser, in.TargetPort)
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/remember", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in rememberReq
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		h.setRemember(in)
		writeJSON(w, h.state())
	})
	mux.HandleFunc("/api/ssh-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in terminalReq
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		in.App = "ssh"
		got, err := h.prepareSSH(in)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, got)
	})
	mux.HandleFunc("/api/ssh-config/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in struct {
			Snippet string `json:"snippet"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.saveSSHSnippet(in.Snippet); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"ok": "1", "path": sshUserConfigPath()})
	})
	mux.HandleFunc("/api/shell", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in struct {
			App string `json:"app"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.openShell(in.App); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/terminal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		var in terminalReq
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.openTerminal(in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		h.requestShutdown()
		writeJSON(w, map[string]string{"ok": "1"})
	})
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method", 405)
			return
		}
		var tasks []Task
		if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.saveTasks(tasks); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, h.state())
	})
	mux.HandleFunc("/api/jumps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method", 405)
			return
		}
		var jumps []Jump
		if err := json.NewDecoder(r.Body).Decode(&jumps); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.saveJumps(jumps); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, h.state())
	})
	mux.HandleFunc("/api/task/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", 405)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/task/")
		parts := strings.Split(rest, "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		id, action := parts[0], parts[1]
		t, ok := h.taskByID(id)
		if !ok {
			http.Error(w, "找不到任务", 404)
			return
		}
		var err error
		switch action {
		case "forward":
			err = h.startForward(t)
		case "stop":
			h.stopForward(t)
		case "xshell", "securecrt":
			err = h.openTaskTerminal(t, action)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, h.state())
	})
	ln, err := listenLocalUI()
	if err != nil {
		return "", nil, nil, nil, err
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	wait = func() { <-h.shutdown }
	closer = func() {
		h.disconnect()
		h.requestShutdown()
		_ = srv.Close()
	}
	return ln.Addr().String(), h, wait, closer, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
