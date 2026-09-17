package main

import (
	"strings"
	"testing"
)

func TestParseProxyArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		socks string
		host  string
		port  string
		ok    bool
	}{
		{name: "host port", args: []string{"10.1.1.1", "22"}, socks: "127.0.0.1:1080", host: "10.1.1.1", port: "22", ok: true},
		{name: "explicit socks", args: []string{"127.0.0.1:1081", "10.1.1.2", "22"}, socks: "127.0.0.1:1081", host: "10.1.1.2", port: "22", ok: true},
		{name: "flag", args: []string{"--socks", "127.0.0.1:1082", "10.1.1.3", "22"}, socks: "127.0.0.1:1082", host: "10.1.1.3", port: "22", ok: true},
		{name: "ipv6 host", args: []string{"127.0.0.1:1080", "::1", "22"}, socks: "127.0.0.1:1080", host: "::1", port: "22", ok: true},
		{name: "bracket ipv6", args: []string{"[::1]", "22"}, socks: "127.0.0.1:1080", host: "::1", port: "22", ok: true},
		{name: "missing", args: []string{"10.1.1.1"}, ok: false},
		{name: "empty", args: nil, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			socks, host, port, err := parseProxyArgs(tt.args, "127.0.0.1:1080")
			if tt.ok {
				if err != nil {
					t.Fatal(err)
				}
				if socks != tt.socks || host != tt.host || port != tt.port {
					t.Fatalf("got %s %s %s", socks, host, port)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSSHProxyCommandQuotesSpacedPath(t *testing.T) {
	got := sshProxyCommand(`C:\Program Files\portfwd.exe`, "127.0.0.1:1080")
	if !strings.Contains(got, `"C:/Program Files/portfwd.exe"`) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "proxy 127.0.0.1:1080 %h %p") {
		t.Fatalf("got %q", got)
	}
}

func TestSSHExample(t *testing.T) {
	got := sshExample(`C:\tools\portfwd.exe`, "127.0.0.1:1080")
	if !strings.Contains(got, "ssh -o ProxyCommand=") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "user@内网主机") {
		t.Fatalf("got %q", got)
	}
}

func TestSSHConfigSnippet(t *testing.T) {
	got := sshConfigSnippet(`C:\tools\portfwd.exe`, "127.0.0.1:1080")
	if !strings.Contains(got, "Host 10.* 192.168.*") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "ProxyCommand") || !strings.Contains(got, "%h %p") {
		t.Fatalf("got %q", got)
	}
}

func TestPublicStateIncludesSSHUsage(t *testing.T) {
	st := newHub().state()
	if st.ProxyCommand == "" || st.SSHExample == "" || st.SSHConfig == "" {
		t.Fatalf("missing usage: %+v", st)
	}
	if !strings.Contains(st.SSHExample, "ProxyCommand") {
		t.Fatal(st.SSHExample)
	}
	if !strings.HasPrefix(st.SOCKS, "127.0.0.1:") {
		t.Fatalf("socks %q", st.SOCKS)
	}
}
