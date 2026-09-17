package main

import (
	"fmt"
	"net"
	"strings"
)

type Task struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       string `json:"user"`
	RemoteHost string `json:"remoteHost"`
	RemotePort string `json:"remotePort"`
	LocalPort  string `json:"localPort"`
}

func (t Task) Validate() error {
	if strings.TrimSpace(t.RemoteHost) == "" {
		return fmt.Errorf("目标主机不能为空")
	}
	if _, err := parsePort(t.RemotePort); err != nil {
		return fmt.Errorf("目标端口：%w", err)
	}
	if _, err := parsePort(t.LocalPort); err != nil {
		return fmt.Errorf("本机端口：%w", err)
	}
	if err := validSSHUser(t.User); err != nil {
		return err
	}
	return nil
}

func (t Task) RemoteAddr() string {
	port := strings.TrimSpace(t.RemotePort)
	if port == "" {
		port = "22"
	}
	return net.JoinHostPort(strings.TrimSpace(t.RemoteHost), port)
}

func (t Task) ListenAddr() string {
	return "127.0.0.1:" + strings.TrimSpace(t.LocalPort)
}

func (t Task) DisplayName() string {
	if s := strings.TrimSpace(t.Name); s != "" {
		return s
	}
	return t.ListenAddr() + " -> " + t.RemoteAddr()
}

func uniqueLocalPorts(tasks []Task) error {
	seen := map[string]bool{}
	for _, t := range tasks {
		port, err := parsePort(t.LocalPort)
		if err != nil {
			return fmt.Errorf("本机端口：%w", err)
		}
		if seen[port] {
			return fmt.Errorf("本机端口 %s 已被另一条转发占用", port)
		}
		seen[port] = true
	}
	return nil
}

func nextLocalPort(tasks []Task) string {
	used := map[string]bool{}
	for _, t := range tasks {
		p, err := parsePort(t.LocalPort)
		if err != nil {
			continue
		}
		used[p] = true
	}
	for n := 10022; n < 60000; n++ {
		p := fmt.Sprintf("%d", n)
		if !used[p] {
			return p
		}
	}
	return "10022"
}
