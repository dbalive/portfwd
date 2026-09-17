package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func useTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := configPathFn
	configPathFn = func() (string, error) {
		return filepath.Join(dir, "config.json"), nil
	}
	t.Cleanup(func() { configPathFn = old })
}

func TestJumpValidateRequiresHostAndUser(t *testing.T) {
	if err := (Jump{SSHHost: "", User: "a"}).Validate(); err == nil {
		t.Fatal("empty host")
	}
	if err := (Jump{SSHHost: "10.1.1.1", User: ""}).Validate(); err == nil {
		t.Fatal("empty user")
	}
	j := Jump{SSHHost: "10.1.1.1", User: "ops", SSHPort: "22", SOCKSPort: "1080"}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateJumpsFromFlatFields(t *testing.T) {
	c := migrateJumps(FileConfig{
		SSHHost:   "192.168.1.1",
		SSHPort:   "22",
		User:      "root",
		Password:  "x",
		SOCKSPort: "1080",
	})
	if len(c.Jumps) != 1 {
		t.Fatalf("jumps %+v", c.Jumps)
	}
	if c.Jumps[0].SSHHost != "192.168.1.1" || c.Jumps[0].User != "root" || c.Jumps[0].Password != "x" {
		t.Fatalf("%+v", c.Jumps[0])
	}
	if c.ActiveJumpID == "" || c.ActiveJumpID != c.Jumps[0].ID {
		t.Fatalf("active %q", c.ActiveJumpID)
	}
}

func TestMigrateJumpsKeepsExisting(t *testing.T) {
	c := migrateJumps(FileConfig{
		SSHHost: "10.9.9.9",
		User:    "a",
		Jumps: []Jump{{
			ID: "j1", SSHHost: "192.168.1.2", SSHPort: "2222", User: "test", SOCKSPort: "1080",
		}},
		ActiveJumpID: "j1",
	})
	if len(c.Jumps) != 1 || c.Jumps[0].SSHHost != "192.168.1.2" {
		t.Fatalf("%+v", c.Jumps)
	}
	if c.SSHHost != "192.168.1.2" || c.SSHPort != "2222" || c.User != "test" {
		t.Fatalf("flat %+v", c)
	}
}

func TestUpsertJumpUpdatesByID(t *testing.T) {
	c := FileConfig{Jumps: []Jump{{ID: "j1", SSHHost: "10.1.1.1", SSHPort: "22", User: "a", Password: "old", SOCKSPort: "1080"}}}
	c, j := upsertJumpFromSettings(c, Settings{JumpID: "j1", SSHHost: "10.2.2.2", SSHPort: "2222", User: "b", SOCKSPort: "1080"})
	if j.ID != "j1" || j.SSHHost != "10.2.2.2" || j.User != "b" || j.Password != "old" {
		t.Fatalf("%+v", j)
	}
	if len(c.Jumps) != 1 || c.ActiveJumpID != "j1" || c.SSHHost != "10.2.2.2" {
		t.Fatalf("%+v", c)
	}
}

func TestUpsertJumpFromSettingsIgnoresIncomplete(t *testing.T) {
	c := FileConfig{Jumps: []Jump{{ID: "j1", SSHHost: "10.1.1.1", SSHPort: "22", User: "a", SOCKSPort: "1080"}}}
	got, j := upsertJumpFromSettings(c, Settings{SSHHost: "", User: "", SSHPort: "22", SOCKSPort: "1080"})
	if len(got.Jumps) != 1 || got.Jumps[0].ID != "j1" {
		t.Fatalf("jumps %+v", got.Jumps)
	}
	if j.SSHHost != "" || j.ID != "" {
		t.Fatalf("empty upsert %+v", j)
	}
}

func TestSetRememberOnDoesNotAddIncompleteJump(t *testing.T) {
	useTempConfig(t)
	h := &hub{cfg: defaultConfig(), forwards: map[string]string{}, shutdown: make(chan struct{})}
	h.setRemember(rememberReq{Remember: true, SSHPort: "22", SOCKSPort: "1080"})
	if len(h.cfg.Jumps) != 0 {
		t.Fatalf("jumps %+v", h.cfg.Jumps)
	}
}

func TestSetRememberOnDoesNotDuplicateExistingJump(t *testing.T) {
	useTempConfig(t)
	h := &hub{cfg: defaultConfig(), forwards: map[string]string{}, shutdown: make(chan struct{})}
	h.cfg.Jumps = []Jump{{ID: "j1", SSHHost: "10.1.1.1", SSHPort: "22", User: "ops", SOCKSPort: "1080"}}
	h.cfg.ActiveJumpID = "j1"
	h.setRemember(rememberReq{Remember: true, SSHHost: "10.1.1.1", SSHPort: "22", User: "ops", SOCKSPort: "1080"})
	if len(h.cfg.Jumps) != 1 || h.cfg.Jumps[0].ID != "j1" {
		t.Fatalf("jumps %+v", h.cfg.Jumps)
	}
}

func TestUpsertJumpAddsWhenUnknown(t *testing.T) {
	c := FileConfig{Jumps: []Jump{{ID: "j1", SSHHost: "10.1.1.1", SSHPort: "22", User: "a", SOCKSPort: "1080"}}}
	c, j := upsertJumpFromSettings(c, Settings{SSHHost: "192.168.1.2", SSHPort: "2222", User: "test", Password: "p", SOCKSPort: "1080"})
	if j.ID == "" || j.ID == "j1" {
		t.Fatalf("id %q", j.ID)
	}
	if len(c.Jumps) != 2 || c.ActiveJumpID != j.ID {
		t.Fatalf("%+v", c)
	}
}

func TestApplyRememberOffClearsJumps(t *testing.T) {
	in := FileConfig{
		Remember:     true,
		SSHHost:      "10.1.1.1",
		User:         "ops",
		Password:     "secret",
		ActiveJumpID: "j1",
		Jumps:        []Jump{{ID: "j1", SSHHost: "10.1.1.1", User: "ops", Password: "secret"}},
		Tasks:        []Task{{ID: "a"}},
	}
	got := applyRememberOff(in)
	if got.ActiveJumpID != "" || len(got.Jumps) != 0 || got.SSHHost != "" {
		t.Fatalf("%+v", got)
	}
}

func TestPersistableConfigKeepsJumpsWhenRemembering(t *testing.T) {
	in := FileConfig{
		Remember:     true,
		SSHHost:      "10.1.1.1",
		SSHPort:      "22",
		User:         "ops",
		Password:     "secret",
		SOCKSPort:    "1080",
		ActiveJumpID: "j1",
		Jumps:        []Jump{{ID: "j1", SSHHost: "10.1.1.1", SSHPort: "22", User: "ops", Password: "secret", SOCKSPort: "1080"}},
	}
	got := persistableConfig(in)
	if len(got.Jumps) != 1 || got.Jumps[0].Password != "secret" || got.ActiveJumpID != "j1" {
		t.Fatalf("%+v", got)
	}
}

func TestJumpKeyIgnoresName(t *testing.T) {
	a := Jump{SSHHost: "10.1.1.1", SSHPort: "22", User: "a", Name: "one"}
	b := Jump{SSHHost: "10.1.1.1", SSHPort: "22", User: "a", Name: "two"}
	if jumpKey(a) != jumpKey(b) {
		t.Fatal(jumpKey(a))
	}
	if !strings.Contains(jumpKey(a), "10.1.1.1") {
		t.Fatal(jumpKey(a))
	}
}
