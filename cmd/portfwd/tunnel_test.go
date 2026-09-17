package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSite(t *testing.T) {
	got, err := normalizeSite("192.168.10.10")
	if err != nil || got != "https://192.168.10.10" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = normalizeSite("https://192.168.10.10")
	if err != nil || got != "https://192.168.10.10" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := normalizeSite("javascript:alert(1)"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := normalizeSite(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestPrepareSettings(t *testing.T) {
	addr, socks, err := prepareSettings(Settings{
		SSHHost:   "192.168.1.1",
		SSHPort:   "22",
		User:      "root",
		SOCKSPort: "1080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if addr != "192.168.1.1:22" || socks != "1080" {
		t.Fatalf("%s %s", addr, socks)
	}
	if _, _, err := prepareSettings(Settings{SSHHost: "192.168.1.1", SSHPort: "22", SOCKSPort: "1080"}); err == nil {
		t.Fatal("expected empty user error")
	}
	if _, _, err := prepareSettings(Settings{SSHHost: "192.168.1.1", SSHPort: "22", User: "u", SOCKSPort: "1080&x"}); err == nil {
		t.Fatal("expected socks port error")
	}
}

func TestQuietSOCKSErrHidesDNSReject(t *testing.T) {
	err := fmt.Errorf("ssh: rejected: connect failed (Name or service not known)")
	if !quietSOCKSErr(err) {
		t.Fatal("dns reject must be hidden")
	}
	if !quietSOCKSErr(context.DeadlineExceeded) {
		t.Fatal("dial timeout must be hidden")
	}
	if quietSOCKSErr(fmt.Errorf("connection refused")) {
		t.Fatal("real connect errors should still show")
	}
}

func TestNextTimeoutCountKillsAfterLimit(t *testing.T) {
	n, kill := nextTimeoutCount(0, nil)
	if n != 0 || kill {
		t.Fatalf("success %d %v", n, kill)
	}
	n, kill = nextTimeoutCount(2, context.DeadlineExceeded)
	if n != 3 || !kill {
		t.Fatalf("limit %d %v", n, kill)
	}
	n, kill = nextTimeoutCount(3, fmt.Errorf("connection refused"))
	if n != 3 || kill {
		t.Fatalf("refused %d %v", n, kill)
	}
}

func TestTunnelWaitDeadOnClose(t *testing.T) {
	tnl := &Tunnel{dead: make(chan struct{}), locals: map[string]net.Listener{}}
	done := make(chan struct{})
	go func() {
		tnl.WaitDead()
		close(done)
	}()
	tnl.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitDead")
	}
	tnl.Close()
}

func TestQuietSOCKS(t *testing.T) {
	if !quietSOCKS("example.com:443") || !quietSOCKS("10.1.1.1:80") {
		t.Fatal("web ports should be quiet")
	}
	if quietSOCKS("10.1.1.1:22") {
		t.Fatal("ssh should be logged")
	}
}

func TestIsRemoteDNSError(t *testing.T) {
	if !isRemoteDNSError(fmt.Errorf("ssh: rejected: connect failed (Name or service not known)")) {
		t.Fatal("jump DNS miss")
	}
	if isRemoteDNSError(fmt.Errorf("connection refused")) {
		t.Fatal("refused is not DNS")
	}
}

func TestResolveLocalTarget(t *testing.T) {
	old := lookupHost
	t.Cleanup(func() { lookupHost = old })
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		if host != "console.example.internal" {
			t.Fatalf("host %s", host)
		}
		return []string{"192.168.10.22"}, nil
	}
	got, err := resolveLocalTarget("console.example.internal:443")
	if err != nil || got != "192.168.10.22:443" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := resolveLocalTarget("10.1.1.1:443"); err == nil {
		t.Fatal("IP should not look up DNS")
	}
}

func TestDialSSHTargetFallsBackToLocalDNS(t *testing.T) {
	old := lookupHost
	t.Cleanup(func() { lookupHost = old })
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		return []string{"192.168.10.22"}, nil
	}
	f := &seqDialer{fail: map[string]error{
		"console.example.internal:443": fmt.Errorf("ssh: rejected: connect failed (Name or service not known)"),
	}}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	f.ok = map[string]net.Conn{"192.168.10.22:443": c1}
	got, err := dialSSHTarget(f, "tcp", "console.example.internal:443")
	if err != nil || got != c1 {
		t.Fatalf("got %v %v", got, err)
	}
	if len(f.calls) != 2 || f.calls[0] != "console.example.internal:443" || f.calls[1] != "192.168.10.22:443" {
		t.Fatalf("calls %v", f.calls)
	}
}

type seqDialer struct {
	calls []string
	fail  map[string]error
	ok    map[string]net.Conn
}

func (f *seqDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	f.calls = append(f.calls, address)
	if err := f.fail[address]; err != nil {
		return nil, err
	}
	if c := f.ok[address]; c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("unexpected %s", address)
}

func TestNormalizeSiteKeepsPort(t *testing.T) {
	got, err := normalizeSite("https://192.168.10.10:8443/path")
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := url.Parse(got); u.Host != "192.168.10.10:8443" || u.Path != "/path" {
		t.Fatalf("got %q", got)
	}
	_ = strings.TrimSpace(got)
}
