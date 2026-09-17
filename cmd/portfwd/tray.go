package main

import (
	"errors"
	"strings"
	"time"
)

const (
	trayIDConnect    = 1001
	trayIDDisconnect = 1002
	trayIDSettings   = 1003
	trayIDExit       = 1004
	trayIDBrowser    = 1005
	trayIDSSH        = 1006
	trayIDJumpBase   = 1100
	trayClickGap     = 400 * time.Millisecond
)

var quitUILoop func()

func shouldDeleteTrayIcon(hwnd uintptr) bool {
	return hwnd != 0
}

func acceptTrayShow(now, last time.Time, gap time.Duration) bool {
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= gap
}

type trayItem struct {
	ID      int
	Label   string
	Enabled bool
}

func trayItems(connected, connecting bool) []trayItem {
	canConnect := !connected && !connecting
	canDisconnect := connected || connecting
	canTools := connected && !connecting
	return []trayItem{
		{ID: trayIDConnect, Label: "连接", Enabled: canConnect},
		{ID: trayIDDisconnect, Label: "断开", Enabled: canDisconnect},
		{ID: trayIDBrowser, Label: "打开浏览器", Enabled: canTools},
		{ID: trayIDSSH, Label: "配置SSH", Enabled: canTools},
		{ID: trayIDSettings, Label: "设置", Enabled: true},
		{ID: trayIDExit, Label: "退出", Enabled: true},
	}
}

func settingsFromFileConfig(c FileConfig) Settings {
	return Settings{
		JumpID:    c.ActiveJumpID,
		SSHHost:   c.SSHHost,
		SSHPort:   c.SSHPort,
		User:      c.User,
		Password:  c.Password,
		SOCKSPort: c.SOCKSPort,
	}
}

var errNeedSettings = errString("请先打开设置填写跳板")
var errNeedSSHHost = errString("请先填写目标主机")
var errNoOpenSSH = errString("未安装 OpenSSH")

func sshConfigNeedsUI(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errNeedSSHHost) {
		return true
	}
	var hostErr sshHostError
	return errors.As(err, &hostErr)
}

func trayJumpChoices(jumps []Jump) []Jump {
	out := make([]Jump, 0, len(jumps))
	for _, j := range jumps {
		j = j.Normalize()
		if j.SSHHost == "" {
			continue
		}
		out = append(out, j)
	}
	return out
}

func trayJumpMenuLabel(j Jump) string {
	return j.Normalize().SSHHost
}

func shouldPickTrayJump(n int) bool {
	return n > 1
}

func shouldAttachTrayJumpSubmenu(n int) bool {
	return n > 1
}

func savedJumpSettings(cfg FileConfig, id string) (Settings, error) {
	if strings.TrimSpace(id) != "" {
		j, ok := jumpByID(cfg.Jumps, id)
		if !ok || strings.TrimSpace(j.SSHHost) == "" {
			return Settings{}, errNeedSettings
		}
		return j.Settings(), nil
	}
	choices := trayJumpChoices(cfg.Jumps)
	if len(choices) == 1 {
		return choices[0].Settings(), nil
	}
	s := settingsFromFileConfig(cfg)
	if strings.TrimSpace(s.SSHHost) == "" {
		return Settings{}, errNeedSettings
	}
	return s, nil
}

func (h *hub) savedJumps() []Jump {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Jump(nil), h.cfg.Jumps...)
}

func (h *hub) connectFromSaved() error {
	return h.connectFromSavedJump("")
}

func (h *hub) connectFromSavedJump(id string) error {
	h.mu.Lock()
	cfg := h.cfg
	connected := h.connected
	connecting := h.connecting
	h.mu.Unlock()
	if connected || connecting {
		return nil
	}
	s, err := savedJumpSettings(cfg, id)
	if err != nil {
		return err
	}
	go h.connect(s)
	return nil
}

func (h *hub) connectFromJump(j Jump) error {
	j = j.Normalize()
	if j.ID != "" {
		return h.connectFromSavedJump(j.ID)
	}
	h.mu.Lock()
	busy := h.connected || h.connecting
	h.mu.Unlock()
	if busy {
		return nil
	}
	if j.SSHHost == "" {
		return errNeedSettings
	}
	go h.connect(j.Settings())
	return nil
}
