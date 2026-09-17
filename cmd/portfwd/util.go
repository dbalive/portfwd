package main

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func parsePort(s string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("端口无效：%s", s)
	}
	return strconv.Itoa(n), nil
}

func httpsURL(remote string) string {
	host, port, err := net.SplitHostPort(remote)
	if err != nil {
		return "https://" + remote
	}
	if port == "443" {
		return "https://" + host
	}
	return "https://" + net.JoinHostPort(host, port)
}

func parseRemote(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", fmt.Errorf("远端地址不能为空")
	}
	if !strings.Contains(remote, ":") {
		remote += ":443"
	}
	host, port, err := net.SplitHostPort(remote)
	if err != nil {
		return "", fmt.Errorf("远端地址无效：%s", remote)
	}
	port, err = parsePort(port)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("远端地址无效：%s", remote)
	}
	return net.JoinHostPort(host, port), nil
}

func normalizeSite(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("打开网址不能为空")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("打开网址无效：%s", s)
	}
	return u.String(), nil
}

func withDefaultPort(remote, defaultPort string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return remote
	}
	if _, _, err := net.SplitHostPort(remote); err == nil {
		return remote
	}
	return net.JoinHostPort(remote, defaultPort)
}
