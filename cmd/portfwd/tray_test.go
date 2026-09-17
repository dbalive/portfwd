package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrayIconDeleteNeedsLiveHWND(t *testing.T) {
	if shouldDeleteTrayIcon(0) {
		t.Fatal("NIM_DELETE with a zero hwnd leaves a ghost icon until the mouse passes over it")
	}
	if !shouldDeleteTrayIcon(1) {
		t.Fatal("delete the icon while the tray window still exists")
	}
}

func TestAcceptTrayShowCoalescesDoubleAndTripleClick(t *testing.T) {
	gap := 400 * time.Millisecond
	t0 := time.Unix(1, 0)
	if !acceptTrayShow(t0, time.Time{}, gap) {
		t.Fatal("first click should open")
	}
	if acceptTrayShow(t0.Add(80*time.Millisecond), t0, gap) {
		t.Fatal("double-click must not open again")
	}
	if acceptTrayShow(t0.Add(200*time.Millisecond), t0, gap) {
		t.Fatal("triple-click must not open again")
	}
	if !acceptTrayShow(t0.Add(gap), t0, gap) {
		t.Fatal("a later click may show the same window")
	}
}

func TestShouldLaunchAppWindowOnlyWhenIdle(t *testing.T) {
	if shouldLaunchAppWindow(4321, false, false) {
		t.Fatal("existing UI process must not start another")
	}
	if shouldLaunchAppWindow(0, true, false) {
		t.Fatal("a launch already in flight must not start another")
	}
	if shouldLaunchAppWindow(0, false, true) {
		t.Fatal("an already visible UI must not start another")
	}
	if !shouldLaunchAppWindow(0, false, false) {
		t.Fatal("no window yet should start one")
	}
}

func TestShouldRescanAppWindowOnlyIfCachedGone(t *testing.T) {
	if shouldRescanAppWindow(true) {
		t.Fatal("a live hwnd must not EnumWindows on every poll")
	}
	if !shouldRescanAppWindow(false) {
		t.Fatal("a dead hwnd should rescan")
	}
}

func TestIsPortFwdUITitle(t *testing.T) {
	if !isPortFwdUITitle("动态端口转发", "") {
		t.Fatal("page title")
	}
	if !isPortFwdUITitle("动态端口转发 - 个人", "http://127.0.0.1:9") {
		t.Fatal("browser may suffix the title")
	}
	if !isPortFwdUITitle("http://127.0.0.1:9/", "http://127.0.0.1:9") {
		t.Fatal("title can still be the app URL while loading")
	}
	if isPortFwdUITitle("Microsoft Edge", "http://127.0.0.1:9") {
		t.Fatal("unrelated window")
	}
	if isPortFwdUITitle("", "http://127.0.0.1:9") {
		t.Fatal("empty title")
	}
}

func TestTrayItemsOrderAndLabels(t *testing.T) {
	items := trayItems(false, false)
	want := []string{"连接", "断开", "打开浏览器", "配置SSH", "设置", "退出"}
	if len(items) != len(want) {
		t.Fatalf("got %d items", len(items))
	}
	for i, label := range want {
		if items[i].Label != label {
			t.Fatalf("item %d: got %q want %q", i, items[i].Label, label)
		}
	}
	if !items[0].Enabled || items[1].Enabled || items[2].Enabled || items[3].Enabled {
		t.Fatalf("idle: only connect on: %+v", items)
	}
	on := trayItems(true, false)
	if on[0].Enabled || !on[1].Enabled || !on[2].Enabled || !on[3].Enabled {
		t.Fatalf("connected: disconnect/browser/ssh on: %+v", on)
	}
	busy := trayItems(false, true)
	if busy[0].Enabled || !busy[1].Enabled || busy[2].Enabled || busy[3].Enabled {
		t.Fatalf("connecting: connect and browser/ssh off: %+v", busy)
	}
}

func TestSettingsFromFileConfigCopiesJumpFields(t *testing.T) {
	s := settingsFromFileConfig(FileConfig{
		SSHHost: "10.1.1.1", SSHPort: "2222", User: "ops", Password: "x", SOCKSPort: "1080",
	})
	if s.SSHHost != "10.1.1.1" || s.SSHPort != "2222" || s.User != "ops" || s.Password != "x" || s.SOCKSPort != "1080" {
		t.Fatalf("%+v", s)
	}
}

func TestRequestShutdownQuitsUILoop(t *testing.T) {
	called := false
	prev := quitUILoop
	quitUILoop = func() { called = true }
	t.Cleanup(func() { quitUILoop = prev })
	h := &hub{cfg: FileConfig{}, forwards: map[string]string{}, shutdown: make(chan struct{})}
	h.requestShutdown()
	select {
	case <-h.shutdown:
	default:
		t.Fatal("shutdown channel")
	}
	if !called {
		t.Fatal("closing the window must stop the tray loop")
	}
}

func TestConnectFromSavedEmptyHostNeedsUI(t *testing.T) {
	h := &hub{cfg: FileConfig{}, forwards: map[string]string{}, shutdown: make(chan struct{})}
	if err := h.connectFromSaved(); err != errNeedSettings {
		t.Fatalf("got %v", err)
	}
}

func TestTrayJumpPickerShowsOnlyHost(t *testing.T) {
	jumps := trayJumpChoices([]Jump{
		{ID: "a", Name: "生产", SSHHost: "192.168.1.2", SSHPort: "2222", User: "ops", SOCKSPort: "1080"},
		{ID: "b", SSHHost: "", User: "ops"},
		{ID: "c", SSHHost: "192.168.1.1", SSHPort: "22", User: "root", SOCKSPort: "1081"},
	})
	if len(jumps) != 2 {
		t.Fatalf("got %d jumps", len(jumps))
	}
	if trayJumpMenuLabel(jumps[0]) != "192.168.1.2" {
		t.Fatalf("picker must show only the jump host, got %q", trayJumpMenuLabel(jumps[0]))
	}
	if trayJumpMenuLabel(jumps[0]) == jumps[0].Label() {
		t.Fatal("do not show the jump name or port")
	}
	if !shouldPickTrayJump(len(jumps)) || shouldPickTrayJump(1) {
		t.Fatal("ask only when there are multiple jumps")
	}
	if !shouldAttachTrayJumpSubmenu(2) || shouldAttachTrayJumpSubmenu(1) {
		t.Fatal("hover submenu only when there are multiple jumps")
	}
	s, err := savedJumpSettings(FileConfig{Jumps: jumps, SSHHost: "192.168.1.2", SSHPort: "2222", User: "ops", SOCKSPort: "1080"}, "c")
	if err != nil || s.SSHHost != "192.168.1.1" || s.SOCKSPort != "1081" {
		t.Fatalf("pick jump c: %+v %v", s, err)
	}
}

func TestRequestOpenSSHConsumedOnlyByTakeState(t *testing.T) {
	h := &hub{cfg: FileConfig{SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{})}
	h.requestOpenSSH()
	if connected, connecting := h.connectionFlags(); connected || connecting {
		t.Fatalf("idle flags %v %v", connected, connecting)
	}
	if h.state().OpenSSH {
		t.Fatal("snapshot must not expose or consume openSSH")
	}
	if !h.takeState().OpenSSH {
		t.Fatal("want openSSH once on takeState")
	}
	if h.takeState().OpenSSH {
		t.Fatal("openSSH should be consumed")
	}
}

func TestBrowserMissingMessageIncludesSOCKSAndInstallHint(t *testing.T) {
	got := browserMissingMessage("127.0.0.1:1080", true)
	if !strings.Contains(got, "socks5://127.0.0.1:1080") || !strings.Contains(got, "Chrome") || !strings.Contains(got, "Edge") || !strings.Contains(got, "已复制") {
		t.Fatalf("%s", got)
	}
	miss := browserMissingMessage("127.0.0.1:1080", false)
	if strings.Contains(miss, "已复制") || !strings.Contains(miss, "socks5://127.0.0.1:1080") {
		t.Fatalf("%s", miss)
	}
}

func TestWriteSSHFromSavedRequiresConnection(t *testing.T) {
	h := &hub{cfg: FileConfig{TargetHost: "10.1.1.1", SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{})}
	if err := h.writeSSHFromSaved(); err != errNotConnected {
		t.Fatalf("got %v", err)
	}
}

func TestWriteSSHFromSavedEmptyHostNeedsUI(t *testing.T) {
	h := &hub{cfg: FileConfig{SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{}), connected: true}
	if err := h.writeSSHFromSaved(); err != errNeedSSHHost {
		t.Fatalf("got %v", err)
	}
	if !sshConfigNeedsUI(errNeedSSHHost) || sshConfigNeedsUI(errNotConnected) {
		t.Fatal("empty host should open UI; disconnect should not")
	}
}

func TestWriteSSHFromSavedRejectsBadHost(t *testing.T) {
	h := &hub{cfg: FileConfig{TargetHost: "???", SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{}), connected: true}
	err := h.writeSSHFromSaved()
	var hostErr sshHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("got %v", err)
	}
	if !sshConfigNeedsUI(err) {
		t.Fatal("bad host should open UI")
	}
}

func TestSSHConfigNeedsUIOnlyForHostProblems(t *testing.T) {
	if sshConfigNeedsUI(errNoOpenSSH) || sshConfigNeedsUI(errNotConnected) {
		t.Fatal("missing OpenSSH / disconnect must not open the editor")
	}
	if sshConfigNeedsUI(fmt.Errorf("OpenSSH 配置中有未闭合的 PortFwd 标记，已中止以免破坏其余配置")) {
		t.Fatal("broken existing config must not open the editor")
	}
	if !sshConfigNeedsUI(sshHostError{"地址段无效，示例：192.168.1.1~10"}) {
		t.Fatal("range errors should open UI")
	}
}

func TestSaveSSHSnippetRequiresConnection(t *testing.T) {
	if detectApps().SSH == "" {
		t.Skip("no OpenSSH")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	h := &hub{cfg: FileConfig{SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{})}
	snippet := sshManagedBlockSpec(sshHostSpec{HostLine: "10.1.1.9", Hosts: []string{"10.1.1.9"}}, "", "", "app.exe", "127.0.0.1:1080")
	if err := h.saveSSHSnippet(snippet); err != errNotConnected {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "config")); !os.IsNotExist(err) {
		t.Fatalf("must not write while disconnected: %v", err)
	}
}

func TestWriteSSHFromSavedWritesWhenValid(t *testing.T) {
	if detectApps().SSH == "" {
		t.Skip("no OpenSSH")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	h := &hub{cfg: FileConfig{TargetHost: "10.1.1.9", SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{}), connected: true}
	if err := h.writeSSHFromSaved(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "Host 10.1.1.9") || !strings.Contains(text, "ProxyCommand") {
		t.Fatalf("%s", text)
	}
}

func TestWriteSSHFromSavedDoesNotRestoreAfterDisconnect(t *testing.T) {
	if detectApps().SSH == "" {
		t.Skip("no OpenSSH")
	}
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	h := &hub{cfg: FileConfig{TargetHost: "10.1.1.9", SOCKSPort: "1080"}, forwards: map[string]string{}, shutdown: make(chan struct{}), connected: true}
	if err := h.writeSSHFromSaved(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sshWriteHook = nil })
	sshWriteHook = func() { h.disconnect() }
	if err := h.writeSSHFromSaved(); err != errNotConnected {
		t.Fatalf("got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "BEGIN PortFwd") {
		t.Fatal("must not restore OpenSSH config after disconnect")
	}
}
