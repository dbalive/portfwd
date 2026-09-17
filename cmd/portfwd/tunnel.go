package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	socksDialTimeout   = 8 * time.Second
	socksMaxConcurrent = 6
	socksTimeoutLimit  = 3
)

var (
	errTunnelClosed = errors.New("disconnected")
	errSOCKSBusy    = errors.New("SOCKS busy")
)

type Settings struct {
	JumpID    string `json:"id,omitempty"`
	SSHHost   string `json:"sshHost"`
	SSHPort   string `json:"sshPort"`
	User      string `json:"user"`
	Password  string `json:"password"`
	SOCKSPort string `json:"socksPort"`
}

type Config struct {
	Settings
	Log func(string)
}

type Tunnel struct {
	client       *ssh.Client
	socks        net.Listener
	locals       map[string]net.Listener
	logf         func(string)
	closed       bool
	mu           sync.Mutex
	dead         chan struct{}
	deadOnce     sync.Once
	dialSem      chan struct{}
	dialTimeouts int
}

func (t *Tunnel) SOCKSAddr() string {
	if t == nil || t.socks == nil {
		return ""
	}
	return t.socks.Addr().String()
}

func (t *Tunnel) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	for _, ln := range t.locals {
		_ = ln.Close()
	}
	t.locals = nil
	socks := t.socks
	client := t.client
	t.socks = nil
	t.client = nil
	t.mu.Unlock()
	if socks != nil {
		_ = socks.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	t.signalDead()
}

func (t *Tunnel) signalDead() {
	if t == nil {
		return
	}
	t.deadOnce.Do(func() {
		if t.dead != nil {
			close(t.dead)
		}
	})
}

func (t *Tunnel) WaitDead() {
	if t == nil || t.dead == nil {
		return
	}
	<-t.dead
}

func (t *Tunnel) AddLocal(localPort, remote string) error {
	if t == nil {
		return fmt.Errorf("not connected")
	}
	port, err := parsePort(localPort)
	if err != nil {
		return err
	}
	remote = withDefaultPort(remote, "22")
	t.mu.Lock()
	if t.closed || t.client == nil {
		t.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	if t.locals == nil {
		t.locals = map[string]net.Listener{}
	}
	if _, ok := t.locals[port]; ok {
		t.mu.Unlock()
		return fmt.Errorf("local port %s already forwarded", port)
	}
	t.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if t.closed || t.client == nil {
		t.mu.Unlock()
		_ = ln.Close()
		return fmt.Errorf("not connected")
	}
	t.locals[port] = ln
	client := t.client
	logf := t.logf
	t.mu.Unlock()
	go acceptForward(ln, client, remote, logf)
	return nil
}

func (t *Tunnel) AddLocalAuto(remote string) (string, error) {
	if t == nil {
		return "", fmt.Errorf("not connected")
	}
	remote = withDefaultPort(remote, "22")
	t.mu.Lock()
	if t.closed || t.client == nil {
		t.mu.Unlock()
		return "", fmt.Errorf("not connected")
	}
	t.mu.Unlock()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	t.mu.Lock()
	if t.closed || t.client == nil {
		t.mu.Unlock()
		_ = ln.Close()
		return "", fmt.Errorf("not connected")
	}
	if t.locals == nil {
		t.locals = map[string]net.Listener{}
	}
	t.locals[port] = ln
	client := t.client
	logf := t.logf
	t.mu.Unlock()
	go acceptForward(ln, client, remote, logf)
	if logf != nil {
		logf("127.0.0.1:" + port + " -> " + remote)
	}
	return port, nil
}

func (t *Tunnel) RemoveLocal(localPort string) {
	if t == nil {
		return
	}
	port, err := parsePort(localPort)
	if err != nil {
		port = strings.TrimSpace(localPort)
	}
	t.mu.Lock()
	ln := t.locals[port]
	delete(t.locals, port)
	t.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func prepareSettings(s Settings) (sshAddr, socksPort string, err error) {
	s.SSHHost = strings.TrimSpace(s.SSHHost)
	s.User = strings.TrimSpace(s.User)
	if s.SSHHost == "" {
		return "", "", fmt.Errorf("SSH host is empty")
	}
	if s.User == "" {
		return "", "", fmt.Errorf("username is empty")
	}
	sshPort, err := parsePort(s.SSHPort)
	if err != nil {
		return "", "", fmt.Errorf("SSH port: %w", err)
	}
	socksPort, err = parsePort(s.SOCKSPort)
	if err != nil {
		return "", "", fmt.Errorf("SOCKS port: %w", err)
	}
	return net.JoinHostPort(s.SSHHost, sshPort), socksPort, nil
}

func quietSOCKSErr(err error) bool {
	return isRemoteDNSError(err) || isDialTimeout(err) || errors.Is(err, errTunnelClosed) || errors.Is(err, errSOCKSBusy)
}

func isDialTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "deadline exceeded") || strings.Contains(s, "i/o timeout")
}

func nextTimeoutCount(cur int, err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	if !isDialTimeout(err) {
		return cur, false
	}
	cur++
	return cur, cur >= socksTimeoutLimit
}

func isRemoteDNSError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "name or service not known") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "server misbehaving") ||
		strings.Contains(s, "temporary failure in name resolution") ||
		strings.Contains(s, "nxdomain") ||
		strings.Contains(s, "unknown host")
}

func quietSOCKS(address string) bool {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return port == "80" || port == "443"
}

func StartSOCKS(ctx context.Context, cfg Config) (*Tunnel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sshAddr, socksPort, err := prepareSettings(cfg.Settings)
	if err != nil {
		return nil, err
	}
	logf := cfg.Log
	if logf == nil {
		logf = func(string) {}
	}

	sshCfg := &ssh.ClientConfig{
		User: cfg.User,
		Auth: passwordAuth(cfg.Password),
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			logf("host key: " + ssh.FingerprintSHA256(key))
			return nil
		},
		Timeout: 15 * time.Second,
	}

	logf("connecting " + sshAddr + " ...")
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", sshAddr)
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, sshCfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	ln, err := net.Listen("tcp", "127.0.0.1:"+socksPort)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	t := &Tunnel{
		client:  client,
		socks:   ln,
		locals:  map[string]net.Listener{},
		logf:    logf,
		dead:    make(chan struct{}),
		dialSem: make(chan struct{}, socksMaxConcurrent),
	}
	go t.waitSSH()
	go t.keepalive()
	go t.acceptSOCKS()
	logf("SOCKS5 127.0.0.1:" + socksPort)
	return t, nil
}

func (t *Tunnel) acceptSOCKS() {
	logf := t.logf
	if logf == nil {
		logf = func(string) {}
	}
	ln := t.socks
	if ln == nil {
		return
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			err := handleSOCKS(c, func(network, address string) (net.Conn, error) {
				up, err := t.dialSOCKS(network, address)
				if err == nil && !quietSOCKS(address) {
					logf("SOCKS " + address)
				}
				return up, err
			})
			if err != nil && !quietSOCKSErr(err) {
				logf("SOCKS: " + err.Error())
			}
		}(c)
	}
}

func (t *Tunnel) dialSOCKS(network, address string) (net.Conn, error) {
	t.mu.Lock()
	if t.closed || t.client == nil {
		t.mu.Unlock()
		return nil, errTunnelClosed
	}
	client := t.client
	sem := t.dialSem
	t.mu.Unlock()

	if sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-time.After(socksDialTimeout):
			return nil, errSOCKSBusy
		}
	}
	conn, err := dialSSHTarget(client, network, address)
	t.noteDial(err)
	return conn, err
}

func (t *Tunnel) noteDial(err error) {
	t.mu.Lock()
	next, kill := nextTimeoutCount(t.dialTimeouts, err)
	t.dialTimeouts = next
	t.mu.Unlock()
	if kill {
		t.Close()
	}
}

func acceptForward(ln net.Listener, client *ssh.Client, remote string, logf func(string)) {
	if logf == nil {
		logf = func(string) {}
	}
	for {
		local, err := ln.Accept()
		if err != nil {
			return
		}
		go func(local net.Conn) {
			defer local.Close()
			up, err := dialSSHTarget(client, "tcp", remote)
			if err != nil {
				logf("forward " + remote + ": " + err.Error())
				return
			}
			defer up.Close()
			pipe(local, up)
		}(local)
	}
}

func (t *Tunnel) waitSSH() {
	t.mu.Lock()
	c := t.client
	t.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.Wait()
	t.Close()
}

func (t *Tunnel) keepalive() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			t.mu.Lock()
			c := t.client
			t.mu.Unlock()
			if c == nil {
				return
			}
			_, _, err := c.SendRequest("keepalive@openssh.com", false, nil)
			if err != nil {
				t.Close()
				return
			}
		case <-t.dead:
			return
		}
	}
}

func passwordAuth(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		}),
	}
}

type contextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

var lookupHost = func(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func resolveLocalTarget(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if net.ParseIP(host) != nil {
		return "", fmt.Errorf("already an IP")
	}
	ips, err := lookupHost(context.Background(), host)
	if err != nil {
		return "", err
	}
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return net.JoinHostPort(ip, port), nil
		}
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no such host")
	}
	return net.JoinHostPort(ips[0], port), nil
}

func dialSSHTarget(d contextDialer, network, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), socksDialTimeout)
	defer cancel()
	up, err := d.DialContext(ctx, network, address)
	if err == nil {
		return up, nil
	}
	if !isRemoteDNSError(err) {
		return nil, err
	}
	local, lerr := resolveLocalTarget(address)
	if lerr != nil {
		return nil, err
	}
	up, err2 := d.DialContext(ctx, network, local)
	if err2 != nil {
		return nil, err
	}
	return up, nil
}
