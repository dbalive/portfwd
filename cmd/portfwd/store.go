package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

type FileConfig struct {
	Remember     bool   `json:"remember"`
	SSHHost      string `json:"sshHost"`
	SSHPort      string `json:"sshPort"`
	User         string `json:"user"`
	Password     string `json:"password,omitempty"`
	SOCKSPort    string `json:"socksPort"`
	ActiveJumpID string `json:"activeJumpId,omitempty"`
	Jumps        []Jump `json:"jumps"`
	TargetHost   string `json:"targetHost"`
	TargetUser   string `json:"targetUser"`
	TargetPort   string `json:"targetPort"`
	Tasks        []Task `json:"tasks"`
}

func defaultConfig() FileConfig {
	return FileConfig{
		Remember:   false,
		SSHHost:    "",
		SSHPort:    "22",
		SOCKSPort:  "1080",
		TargetPort: "22",
		Jumps:      []Jump{},
		Tasks:      []Task{},
	}
}

func applyRememberOff(c FileConfig) FileConfig {
	c.Remember = false
	c.SSHHost = ""
	c.SSHPort = "22"
	c.User = ""
	c.Password = ""
	c.SOCKSPort = "1080"
	c.TargetHost = ""
	c.TargetUser = ""
	c.TargetPort = "22"
	c.ActiveJumpID = ""
	c.Jumps = []Jump{}
	c.Tasks = []Task{}
	return c
}

func persistableConfig(c FileConfig) FileConfig {
	if c.Remember {
		if c.SSHPort == "" {
			c.SSHPort = "22"
		}
		if c.SOCKSPort == "" {
			c.SOCKSPort = "1080"
		}
		if c.Tasks == nil {
			c.Tasks = []Task{}
		}
		if c.Jumps == nil {
			c.Jumps = []Jump{}
		}
		return c
	}
	out := applyRememberOff(c)
	out.Password = ""
	return out
}

func parseConfigJSON(b []byte) FileConfig {
	c := defaultConfig()
	if json.Unmarshal(b, &c) != nil {
		return defaultConfig()
	}
	rememberSet := strings.Contains(string(b), `"remember"`)
	if !rememberSet {
		c.Remember = true
	}
	if c.SSHPort == "" {
		c.SSHPort = "22"
	}
	if c.SOCKSPort == "" {
		c.SOCKSPort = "1080"
	}
	if c.TargetPort == "" {
		c.TargetPort = "22"
	}
	if c.Tasks == nil {
		c.Tasks = []Task{}
	}
	if c.Jumps == nil {
		c.Jumps = []Jump{}
	}
	if !c.Remember {
		c = applyRememberOff(c)
		c.Password = ""
		return c
	}
	return migrateJumps(c)
}

func configPath() (string, error) {
	if fn := configPathFn; fn != nil {
		return fn()
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "PortFwd")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadConfig() FileConfig {
	p, err := configPath()
	if err != nil {
		return defaultConfig()
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return defaultConfig()
	}
	c := parseConfigJSON(b)
	changed := false
	for i := range c.Tasks {
		if strings.TrimSpace(c.Tasks[i].ID) == "" {
			c.Tasks[i].ID = newID()
			changed = true
		}
	}
	c, jumpChanged := ensureJumpIDs(c)
	if changed || jumpChanged {
		_ = saveConfig(c)
	}
	return c
}

func saveConfig(c FileConfig) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(persistableConfig(c), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

var idSeq atomic.Int64

var configPathFn func() (string, error)

func newID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), idSeq.Add(1))
}
