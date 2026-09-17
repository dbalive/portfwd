package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

func parseProxyArgs(args []string, defaultSOCKS string) (socks, host, port string, err error) {
	if strings.TrimSpace(defaultSOCKS) == "" {
		defaultSOCKS = "127.0.0.1:1080"
	}
	switch {
	case len(args) == 2:
		socks, host, port = defaultSOCKS, args[0], args[1]
	case len(args) == 3:
		socks, host, port = args[0], args[1], args[2]
	case len(args) == 4 && args[0] == "--socks":
		socks, host, port = args[1], args[2], args[3]
	default:
		return "", "", "", fmt.Errorf("用法: proxy [socksAddr] host port")
	}
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if host == "" {
		return "", "", "", fmt.Errorf("目标主机不能为空")
	}
	port, err = parsePort(port)
	if err != nil {
		return "", "", "", fmt.Errorf("目标端口：%w", err)
	}
	if _, _, err := net.SplitHostPort(socks); err != nil {
		return "", "", "", fmt.Errorf("SOCKS 地址无效：%s", socks)
	}
	return socks, host, port, nil
}

func defaultSOCKSAddr() string {
	c := loadConfig()
	port, err := parsePort(c.SOCKSPort)
	if err != nil {
		port = "1080"
	}
	return "127.0.0.1:" + port
}

func slashExe(exe string) string {
	return filepath.ToSlash(strings.TrimSpace(exe))
}

func sshProxyCommand(exe, socks string) string {
	exe = slashExe(exe)
	if exe == "" {
		exe = "portfwd.exe"
	}
	if socks == "" {
		socks = "127.0.0.1:1080"
	}
	return strconv.Quote(exe) + " proxy " + socks + " %h %p"
}

func sshExample(exe, socks string) string {
	return "ssh -o ProxyCommand=" + strconv.Quote(sshProxyCommand(exe, socks)) + " user@内网主机"
}

func sshConfigSnippet(exe, socks string) string {
	exe = slashExe(exe)
	if exe == "" {
		exe = "portfwd.exe"
	}
	if socks == "" {
		socks = "127.0.0.1:1080"
	}
	return "Host 10.* 192.168.*\n  ProxyCommand " + strconv.Quote(exe) + " proxy " + socks + " %h %p\n"
}

func currentExe() string {
	p, err := os.Executable()
	if err != nil || p == "" {
		p = os.Args[0]
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p
}

func runProxy(args []string) error {
	socks, host, port, err := parseProxyArgs(args, defaultSOCKSAddr())
	if err != nil {
		return err
	}
	conn, err := dialSOCKS5(socks, net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer conn.Close()
	stdioTunnel(conn, os.Stdin, os.Stdout)
	return nil
}

func stdioTunnel(conn net.Conn, in io.ReadCloser, out io.Writer) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(conn, in)
		closeWrite(conn)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(out, conn)
		_ = in.Close()
	}()
	wg.Wait()
}
