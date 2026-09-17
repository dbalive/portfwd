package main

import (
	"fmt"
	"strings"
)

type Jump struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SSHHost   string `json:"sshHost"`
	SSHPort   string `json:"sshPort"`
	User      string `json:"user"`
	Password  string `json:"password,omitempty"`
	SOCKSPort string `json:"socksPort"`
}

func (j Jump) Normalize() Jump {
	j.ID = strings.TrimSpace(j.ID)
	j.Name = strings.TrimSpace(j.Name)
	j.SSHHost = strings.TrimSpace(j.SSHHost)
	j.SSHPort = strings.TrimSpace(j.SSHPort)
	j.User = strings.TrimSpace(j.User)
	j.SOCKSPort = strings.TrimSpace(j.SOCKSPort)
	if j.SSHPort == "" {
		j.SSHPort = "22"
	}
	if j.SOCKSPort == "" {
		j.SOCKSPort = "1080"
	}
	return j
}

func (j Jump) Validate() error {
	j = j.Normalize()
	if j.SSHHost == "" {
		return fmt.Errorf("跳板不能为空")
	}
	if j.User == "" {
		return fmt.Errorf("用户不能为空")
	}
	if _, err := parsePort(j.SSHPort); err != nil {
		return fmt.Errorf("跳板端口：%w", err)
	}
	if _, err := parsePort(j.SOCKSPort); err != nil {
		return fmt.Errorf("SOCKS 端口：%w", err)
	}
	if err := validSSHUser(j.User); err != nil {
		return err
	}
	return nil
}

func (j Jump) Settings() Settings {
	j = j.Normalize()
	return Settings{
		JumpID:    j.ID,
		SSHHost:   j.SSHHost,
		SSHPort:   j.SSHPort,
		User:      j.User,
		Password:  j.Password,
		SOCKSPort: j.SOCKSPort,
	}
}

func (j Jump) Label() string {
	j = j.Normalize()
	if j.Name != "" {
		return j.Name
	}
	return j.SSHHost + ":" + j.SSHPort
}

func jumpKey(j Jump) string {
	j = j.Normalize()
	return strings.ToLower(j.SSHHost) + "|" + j.SSHPort + "|" + j.User
}

func jumpByID(jumps []Jump, id string) (Jump, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Jump{}, false
	}
	for _, j := range jumps {
		if j.ID == id {
			return j, true
		}
	}
	return Jump{}, false
}

func jumpFromFlat(c FileConfig) Jump {
	return Jump{
		SSHHost:   c.SSHHost,
		SSHPort:   c.SSHPort,
		User:      c.User,
		Password:  c.Password,
		SOCKSPort: c.SOCKSPort,
	}.Normalize()
}

func syncFlatFromActive(c FileConfig) FileConfig {
	j, ok := jumpByID(c.Jumps, c.ActiveJumpID)
	if !ok && len(c.Jumps) > 0 {
		j = c.Jumps[0]
		c.ActiveJumpID = j.ID
	}
	if !ok && len(c.Jumps) == 0 {
		return c
	}
	c.SSHHost = j.SSHHost
	c.SSHPort = j.SSHPort
	c.User = j.User
	c.Password = j.Password
	c.SOCKSPort = j.SOCKSPort
	return c
}

func migrateJumps(c FileConfig) FileConfig {
	if c.Jumps == nil {
		c.Jumps = []Jump{}
	}
	for i := range c.Jumps {
		c.Jumps[i] = c.Jumps[i].Normalize()
	}
	if len(c.Jumps) == 0 {
		j := jumpFromFlat(c)
		if j.SSHHost != "" && j.User != "" {
			if j.ID == "" {
				j.ID = newID()
			}
			c.Jumps = []Jump{j}
			c.ActiveJumpID = j.ID
		}
	}
	if strings.TrimSpace(c.ActiveJumpID) == "" && len(c.Jumps) > 0 {
		c.ActiveJumpID = c.Jumps[0].ID
	}
	if _, ok := jumpByID(c.Jumps, c.ActiveJumpID); !ok && len(c.Jumps) > 0 {
		c.ActiveJumpID = c.Jumps[0].ID
	}
	return syncFlatFromActive(c)
}

func ensureJumpIDs(c FileConfig) (FileConfig, bool) {
	changed := false
	for i := range c.Jumps {
		c.Jumps[i] = c.Jumps[i].Normalize()
		if c.Jumps[i].ID == "" {
			c.Jumps[i].ID = newID()
			changed = true
		}
	}
	if strings.TrimSpace(c.ActiveJumpID) == "" && len(c.Jumps) > 0 {
		c.ActiveJumpID = c.Jumps[0].ID
		changed = true
	}
	return c, changed
}

func upsertJumpFromSettings(c FileConfig, s Settings) (FileConfig, Jump) {
	if c.Jumps == nil {
		c.Jumps = []Jump{}
	}
	j := Jump{
		ID:        strings.TrimSpace(s.JumpID),
		SSHHost:   s.SSHHost,
		SSHPort:   s.SSHPort,
		User:      s.User,
		Password:  s.Password,
		SOCKSPort: s.SOCKSPort,
	}.Normalize()
	if j.ID != "" {
		for i := range c.Jumps {
			if c.Jumps[i].ID == j.ID {
				if j.Password == "" {
					j.Password = c.Jumps[i].Password
				}
				if j.Name == "" {
					j.Name = c.Jumps[i].Name
				}
				c.Jumps[i] = j
				c.ActiveJumpID = j.ID
				return syncFlatFromActive(c), j
			}
		}
	}
	key := jumpKey(j)
	for i := range c.Jumps {
		if jumpKey(c.Jumps[i]) == key {
			j.ID = c.Jumps[i].ID
			if j.Password == "" {
				j.Password = c.Jumps[i].Password
			}
			if j.Name == "" {
				j.Name = c.Jumps[i].Name
			}
			c.Jumps[i] = j
			c.ActiveJumpID = j.ID
			return syncFlatFromActive(c), j
		}
	}
	if j.ID == "" {
		j.ID = newID()
	}
	c.Jumps = append(c.Jumps, j)
	c.ActiveJumpID = j.ID
	return syncFlatFromActive(c), j
}
