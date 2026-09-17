package main

import (
	"strings"
	"testing"
)

func TestParseSSHHostSpecExactIP(t *testing.T) {
	got, err := parseSSHHostSpec("10.1.1.9")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostLine != "10.1.1.9" || got.HostName != "10.1.1.9" || got.Pattern {
		t.Fatalf("%+v", got)
	}
}

func TestParseSSHHostSpecLastOctetRange(t *testing.T) {
	got, err := parseSSHHostSpec("192.168.1.1~10")
	if err != nil {
		t.Fatal(err)
	}
	want := "192.168.1.1 192.168.1.2 192.168.1.3 192.168.1.4 192.168.1.5 192.168.1.6 192.168.1.7 192.168.1.8 192.168.1.9 192.168.1.10"
	if got.HostLine != want {
		t.Fatalf("HostLine=%q", got.HostLine)
	}
	if got.HostName != "" {
		t.Fatalf("range must not set HostName: %+v", got)
	}
	if !got.Pattern {
		t.Fatal("expected pattern")
	}
	if got.Example != "192.168.1.1" {
		t.Fatalf("Example=%q", got.Example)
	}
}

func TestParseSSHHostSpecPrefix(t *testing.T) {
	got, err := parseSSHHostSpec("192.168.")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "192.168.*" || got.HostName != "" || !got.Pattern {
		t.Fatalf("%+v", got)
	}
}

func TestParseSSHHostSpecMultiplePrefixes(t *testing.T) {
	got, err := parseSSHHostSpec("10.50 10.60. 10.70")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.50*", "10.60.*", "10.70*"}
	if strings.Join(got.Hosts, " ") != strings.Join(want, " ") {
		t.Fatalf("Hosts=%q", got.Hosts)
	}
	if got.HostName != "" || !got.Pattern {
		t.Fatalf("patterns must not set HostName: %+v", got)
	}
}

func TestParseSSHHostSpecRejectsBadRange(t *testing.T) {
	for _, in := range []string{"", "192.168.1.10~1", "192.168.1.1~300", "192.168.1.1~", "~10", "192.168.1.1~10a", "192.168.1.1~010", "192.168.1.1~+10", "::ffff:192.168.1.1~10"} {
		if _, err := parseSSHHostSpec(in); err == nil {
			t.Fatalf("expected error for %q", in)
		}
	}
}

func TestApplyOpenSSHConfigRangeHasNoHostName(t *testing.T) {
	_, snippet, err := applyOpenSSHConfig("", "192.168.1.1~10", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snippet, "Host 192.168.1.1 192.168.1.2") {
		t.Fatalf("snippet %q", snippet)
	}
	if !strings.Contains(snippet, "192.168.1.10") {
		t.Fatalf("snippet %q", snippet)
	}
	if strings.Contains(snippet, "HostName") {
		t.Fatalf("pattern must not set HostName:\n%s", snippet)
	}
}

func TestApplyOpenSSHConfigPrefixHasNoHostName(t *testing.T) {
	_, snippet, err := applyOpenSSHConfig("", "192.168.", "ops", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snippet, "Host 192.168.*\n") {
		t.Fatalf("snippet %q", snippet)
	}
	if strings.Contains(snippet, "HostName") {
		t.Fatalf("prefix must not set HostName:\n%s", snippet)
	}
}

func TestApplyOpenSSHConfigMultiplePrefixes(t *testing.T) {
	_, snippet, err := applyOpenSSHConfig("", "10.50 10.60. 10.70", "", "", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snippet, "Host 10.50* 10.60.* 10.70*\n") {
		t.Fatalf("want one Host line with all patterns:\n%s", snippet)
	}
	if strings.Count(snippet, "Host ") != 1 {
		t.Fatalf("must be a single Host line:\n%s", snippet)
	}
	if strings.Contains(snippet, "HostName") {
		t.Fatalf("address segments must not set HostName:\n%s", snippet)
	}
}

func TestApplyOpenSSHConfigHostAlwaysOneLine(t *testing.T) {
	cases := []struct {
		in, host string
	}{
		{"10.1.1.9", "Host 10.1.1.9\n"},
		{"192.168.1.1~3", "Host 192.168.1.1 192.168.1.2 192.168.1.3\n"},
		{"10.50 10.60. 10.70", "Host 10.50* 10.60.* 10.70*\n"},
		{"10.1.1.9 10.50 192.168.1.1~2", "Host 10.1.1.9 10.50* 192.168.1.1 192.168.1.2\n"},
	}
	for _, tt := range cases {
		_, snippet, err := applyOpenSSHConfig("", tt.in, "", "", "app.exe", "127.0.0.1:1080")
		if err != nil {
			t.Fatalf("%q: %v", tt.in, err)
		}
		if !strings.Contains(snippet, tt.host) {
			t.Fatalf("%q: missing %q in\n%s", tt.in, tt.host, snippet)
		}
		if strings.Count(snippet, "Host ") != 1 {
			t.Fatalf("%q: Host must be one line:\n%s", tt.in, snippet)
		}
	}
}

func TestSSHManualHintAsksUserToConnect(t *testing.T) {
	spec, err := parseSSHHostSpec("192.168.1.1~10")
	if err != nil {
		t.Fatal(err)
	}
	got := sshManualHint("root", spec, "22")
	if !strings.Contains(got, "请手动打开") || !strings.Contains(got, "ssh root@192.168.1.1") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "cmd.exe") {
		t.Fatalf("must not launch cmd: %q", got)
	}
}

func TestSSHManualHintPrefixUsesRunnableHost(t *testing.T) {
	spec, err := parseSSHHostSpec("192.168.")
	if err != nil {
		t.Fatal(err)
	}
	got := sshManualHint("root", spec, "22")
	if strings.Contains(got, "192.168.x.x") || strings.Contains(got, "@192.168.*") {
		t.Fatalf("hint must not use a non-address: %q", got)
	}
	if !strings.Contains(got, "ssh root@192.168.1.1") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "匹配") {
		t.Fatalf("prefix hint should say to substitute a matching address: %q", got)
	}
}
