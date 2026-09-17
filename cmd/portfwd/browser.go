package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func browserCandidates() []string {
	return []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
	}
}

func startAppWindow(url string) (*exec.Cmd, error) {
	profile := appProfileDir()
	closeStaleAppWindows(url)
	prepareAppProfile(profile)
	for _, exe := range browserCandidates() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe, appWindowArgs(url, profile)...)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		if cmd.Process != nil {
			go decorateAppWindow(uint32(cmd.Process.Pid))
		}
		return cmd, nil
	}
	return nil, fmt.Errorf("未找到 Chrome 或 Edge，无法打开界面")
}

func proxiedBrowserArgs(socks string) ([]string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(socks))
	if err != nil {
		return nil, fmt.Errorf("SOCKS 地址无效：%s", socks)
	}
	port, err = parsePort(port)
	if err != nil {
		return nil, fmt.Errorf("SOCKS 端口：%w", err)
	}
	host = strings.TrimSpace(host)
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, fmt.Errorf("SOCKS 只允许本机地址")
	}
	addr := net.JoinHostPort(host, port)
	profile := filepath.Join(os.TempDir(), "portfwd-browser")
	return []string{
		"--user-data-dir=" + profile,
		"--proxy-server=socks5://" + addr,
		"--host-resolver-rules=MAP * ~NOTFOUND , EXCLUDE 127.0.0.1",
		"--proxy-bypass-list=<-loopback>",
		"--disable-quic",
		"--disable-features=UseDnsHttpsSvcb,UseDnsHttpsSvcbAlpn",
		"--test-type", // hide the unsupported --host-resolver-rules infobar on this SOCKS profile
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		"about:blank",
	}, nil
}

func browserMissingMessage(socks string, copied bool) string {
	addr := "socks5://" + strings.TrimSpace(socks)
	if copied {
		return "未找到 Chrome 或 Edge。已复制代理地址 " + addr + " 到剪贴板。请安装 Chrome 或 Edge 后再点「打开浏览器」，或把该地址填到系统/浏览器代理。"
	}
	return "未找到 Chrome 或 Edge。请安装 Chrome 或 Edge 后再点「打开浏览器」。代理地址是 " + addr + " ，可手动填到系统/浏览器代理。"
}

func startProxiedBrowser(socks string) error {
	args, err := proxiedBrowserArgs(socks)
	if err != nil {
		return err
	}
	for _, exe := range browserCandidates() {
		if _, err := os.Stat(exe); err != nil {
			continue
		}
		cmd := exec.Command(exe, args...)
		if err := cmd.Start(); err != nil {
			return err
		}
		return nil
	}
	return errBrowserMissing
}

var errBrowserMissing = errString("未找到 Chrome 或 Edge")
