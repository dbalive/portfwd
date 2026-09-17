package main

import (
	"strings"
	"testing"
)

func TestApplyRememberOffClearsJumpIdentity(t *testing.T) {
	in := FileConfig{
		Remember:   true,
		SSHHost:    "10.1.1.1",
		SSHPort:    "2222",
		User:       "ops",
		Password:   "secret",
		SOCKSPort:  "2080",
		TargetHost: "10.50 10.60.",
		Tasks:      []Task{{ID: "a", RemoteHost: "10.2.2.2", RemotePort: "22", LocalPort: "10022"}},
	}
	got := applyRememberOff(in)
	if got.Remember {
		t.Fatal("remember should be off")
	}
	if got.SSHHost != "" || got.User != "" || got.Password != "" {
		t.Fatalf("jump identity must clear: %+v", got)
	}
	if got.SSHPort != "22" || got.SOCKSPort != "1080" {
		t.Fatalf("ports must reset: %+v", got)
	}
	if got.TargetHost != "" || len(got.Tasks) != 0 {
		t.Fatalf("target and tasks must clear: %+v", got)
	}
}

func TestPersistableConfigOmitsSecretsWhenNotRemembering(t *testing.T) {
	in := FileConfig{
		Remember:   false,
		SSHHost:    "10.1.1.1",
		User:       "ops",
		Password:   "secret",
		SSHPort:    "2222",
		SOCKSPort:  "2080",
		TargetHost: "10.50",
		Tasks:      []Task{{ID: "a"}},
	}
	got := persistableConfig(in)
	if got.Password != "" || got.SSHHost != "" || got.User != "" || len(got.Tasks) != 0 {
		t.Fatalf("must not persist extras: %+v", got)
	}
	if got.SSHPort != "22" || got.SOCKSPort != "1080" {
		t.Fatalf("must persist reset ports: %+v", got)
	}
	if got.TargetHost != "" {
		t.Fatalf("target must stay empty: %+v", got)
	}
}

func TestPersistableConfigKeepsAllWhenRemembering(t *testing.T) {
	in := FileConfig{
		Remember:   true,
		SSHHost:    "10.1.1.1",
		SSHPort:    "2222",
		User:       "ops",
		Password:   "secret",
		SOCKSPort:  "2080",
		TargetHost: "10.50",
		Tasks:      []Task{{ID: "a", RemoteHost: "10.2.2.2", RemotePort: "22", LocalPort: "10022"}},
	}
	got := persistableConfig(in)
	if got.Password != "secret" || got.SOCKSPort != "2080" || got.TargetHost != "10.50" || len(got.Tasks) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Jumps == nil {
		t.Fatal("jumps slice")
	}
}

func TestParseConfigJSONMissingRememberKeepsOldData(t *testing.T) {
	got := parseConfigJSON([]byte(`{"sshHost":"10.9.9.9","tasks":[{"id":"a","remoteHost":"10.2.2.2","remotePort":"22","localPort":"10022"}]}`))
	if !got.Remember {
		t.Fatal("old config without remember should keep saved hosts")
	}
	if got.SSHHost != "10.9.9.9" || len(got.Tasks) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestParseConfigJSONLoadsJumps(t *testing.T) {
	got := parseConfigJSON([]byte(`{"remember":true,"jumps":[{"id":"j1","sshHost":"192.168.1.2","sshPort":"2222","user":"test","socksPort":"1080"}],"activeJumpId":"j1"}`))
	if len(got.Jumps) != 1 || got.SSHHost != "192.168.1.2" || got.User != "test" || got.SSHPort != "2222" {
		t.Fatalf("%+v", got)
	}
}

func TestParseConfigJSONRememberFalseClearsExtras(t *testing.T) {
	got := parseConfigJSON([]byte(`{"remember":false,"sshHost":"10.9.9.9","user":"a","password":"x","sshPort":"2222","socksPort":"2080","targetHost":"10.50","tasks":[{"id":"a"}]}`))
	if got.Remember || got.Password != "" || got.SSHHost != "" || got.User != "" || len(got.Tasks) != 0 {
		t.Fatalf("%+v", got)
	}
	if got.SSHPort != "22" || got.SOCKSPort != "1080" || got.TargetHost != "" {
		t.Fatalf("%+v", got)
	}
}

func TestLogEntryMarksErrors(t *testing.T) {
	e := newLogEntry("失败: timeout", "error")
	if e.Level != "error" || !strings.Contains(e.Text, "失败") {
		t.Fatalf("%+v", e)
	}
}
