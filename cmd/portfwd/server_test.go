package main

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLiveStateIncludesProxyCommand(t *testing.T) {
	addr, _, _, closer, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closer()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st publicState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.SSHExample, "ProxyCommand") {
		t.Fatalf("ssh example %q", st.SSHExample)
	}
	if !strings.Contains(st.ProxyCommand, "proxy") || !strings.Contains(st.ProxyCommand, "%h %p") {
		t.Fatalf("proxy command %q", st.ProxyCommand)
	}
	if !strings.Contains(st.SOCKS, "127.0.0.1") {
		t.Fatalf("socks %q", st.SOCKS)
	}
}

func TestWatchTunnelDisconnectsOnDead(t *testing.T) {
	h := newHub()
	tun := &Tunnel{dead: make(chan struct{}), locals: map[string]net.Listener{}}
	h.tun = tun
	h.connected = true
	h.gen = 1
	done := make(chan struct{})
	go func() {
		h.watchTunnel(1, tun)
		close(done)
	}()
	tun.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTunnel")
	}
	if h.connected || h.tun != nil {
		t.Fatal("still connected")
	}
	found := false
	for _, l := range h.logs {
		if strings.Contains(l.Text, "跳板转发超时") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("logs %#v", h.logs)
	}
}

func TestClearLogs(t *testing.T) {
	h := newHub()
	if len(h.logs) == 0 {
		t.Fatal("want welcome log")
	}
	h.log("keep then drop")
	h.clearLogs()
	if len(h.logs) != 0 {
		t.Fatalf("cleared %#v", h.logs)
	}
}

func TestClearLogsAPI(t *testing.T) {
	addr, _, _, closer, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closer()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post("http://"+addr+"/api/logs/clear", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st publicState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Logs) != 0 {
		t.Fatalf("logs %#v", st.Logs)
	}
}
