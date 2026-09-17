package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	sshMarkerBegin = "# BEGIN PortFwd SOCKS5"
	sshMarkerEnd   = "# END PortFwd SOCKS5"
)

func sshUserConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".ssh", "config")
}

func sshManagedBlockSpec(spec sshHostSpec, user, port, exe, socks string) string {
	var b strings.Builder
	b.WriteString(sshMarkerBegin + "\n")
	line := strings.TrimSpace(spec.HostLine)
	if line == "" {
		line = strings.Join(spec.Hosts, " ")
	}
	b.WriteString("Host " + line + "\n")
	writeSSHHostOptions(&b, user, port, exe, socks)
	b.WriteString(sshMarkerEnd + "\n")
	return b.String()
}

func writeSSHHostOptions(b *strings.Builder, user, port, exe, socks string) {
	if strings.TrimSpace(user) != "" {
		b.WriteString("  User " + strings.TrimSpace(user) + "\n")
	}
	if strings.TrimSpace(port) != "" {
		b.WriteString("  Port " + strings.TrimSpace(port) + "\n")
	}
	b.WriteString("  ProxyCommand " + sshProxyCommand(exe, socks) + "\n")
	b.WriteString("  PubkeyAuthentication no\n")
	b.WriteString("  PreferredAuthentications password,keyboard-interactive\n")
	b.WriteString("  NumberOfPasswordPrompts 1\n")
	b.WriteString("  StrictHostKeyChecking accept-new\n")
}

func applyOpenSSHConfig(existing, host, user, port, exe, socks string) (updated, snippet string, err error) {
	spec, err := parseSSHHostSpec(host)
	if err != nil {
		return "", "", err
	}
	snippet = sshManagedBlockSpec(spec, user, port, exe, socks)
	return mergeOpenSSHSnippet(existing, snippet)
}

func applyOpenSSHSnippet(existing, snippet string) (updated, outSnippet string, err error) {
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return "", "", fmt.Errorf("配置内容为空")
	}
	if !strings.Contains(snippet, sshMarkerBegin) {
		snippet = sshMarkerBegin + "\n" + snippet
	}
	if !strings.Contains(snippet, sshMarkerEnd) {
		snippet += "\n" + sshMarkerEnd
	}
	if !strings.HasSuffix(snippet, "\n") {
		snippet += "\n"
	}
	return mergeOpenSSHSnippet(existing, snippet)
}

func mergeOpenSSHSnippet(existing, snippet string) (updated, outSnippet string, err error) {
	body, err := stripPortFwdBlocks(existing)
	if err != nil {
		return "", "", err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return snippet, snippet, nil
	}
	return snippet + "\n" + body + "\n", snippet, nil
}

func sshManagedBlock(host, user, port, exe, socks string) string {
	spec, err := parseSSHHostSpec(host)
	if err != nil {
		return ""
	}
	return sshManagedBlockSpec(spec, user, port, exe, socks)
}

func writeOpenSSHConfig(path, host, user, port, exe, socks string) (snippet string, err error) {
	sshConfigMu.Lock()
	defer sshConfigMu.Unlock()
	dir := filepath.Dir(path)
	_, dirErr := os.Stat(dir)
	createdDir := os.IsNotExist(dirErr)
	_, fileErr := os.Stat(path)
	createdFile := os.IsNotExist(fileErr)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	old, err := readSSHConfigFile(path)
	if err != nil {
		return "", err
	}
	updated, snippet, err := applyOpenSSHConfig(old, host, user, port, exe, socks)
	if err != nil {
		return "", err
	}
	if err := writeFileReplace(path, []byte(updated), 0o600); err != nil {
		return "", err
	}
	if createdDir {
		_ = secureSSHPath(dir, true)
	}
	if createdFile {
		_ = secureSSHPath(path, false)
	}
	return snippet, nil
}

func writeOpenSSHSnippet(path, snippet string) error {
	sshConfigMu.Lock()
	defer sshConfigMu.Unlock()
	return writeOpenSSHSnippetLocked(path, snippet)
}

func writeOpenSSHSnippetLocked(path, snippet string) error {
	dir := filepath.Dir(path)
	_, dirErr := os.Stat(dir)
	createdDir := os.IsNotExist(dirErr)
	_, fileErr := os.Stat(path)
	createdFile := os.IsNotExist(fileErr)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	old, err := readSSHConfigFile(path)
	if err != nil {
		return err
	}
	updated, _, err := applyOpenSSHSnippet(old, snippet)
	if err != nil {
		return err
	}
	if err := writeFileReplace(path, []byte(updated), 0o600); err != nil {
		return err
	}
	if createdDir {
		_ = secureSSHPath(dir, true)
	}
	if createdFile {
		_ = secureSSHPath(path, false)
	}
	return nil
}

var sshConfigMu sync.Mutex

func readSSHConfigFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取 OpenSSH 配置失败：%w", err)
	}
	return string(b), nil
}

func writeFileReplace(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return err
	}
	_ = f.Chmod(perm)
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := replaceFile(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func clearOpenSSHConfig(path string) error {
	sshConfigMu.Lock()
	defer sshConfigMu.Unlock()
	old, err := readSSHConfigFile(path)
	if err != nil {
		return err
	}
	if !strings.Contains(old, "BEGIN PortFwd SOCKS5") {
		return nil
	}
	updated, err := stripPortFwdBlocks(old)
	if err != nil {
		return err
	}
	updated = strings.TrimSpace(updated)
	if updated == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	return writeFileReplace(path, []byte(updated), 0o600)
}

func stripPortFwdBlocks(s string) (string, error) {
	lines := splitKeep(s)
	var out []string
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if isPortFwdMarker(t, sshMarkerBegin) {
			if in {
				return "", fmt.Errorf("OpenSSH 配置中有未闭合的 PortFwd 标记，已中止以免破坏其余配置")
			}
			in = true
			continue
		}
		if in {
			if isPortFwdMarker(t, sshMarkerEnd) {
				in = false
			}
			continue
		}
		out = append(out, line)
	}
	if in {
		return "", fmt.Errorf("OpenSSH 配置中有未闭合的 PortFwd 标记，已中止以免破坏其余配置")
	}
	return strings.Join(out, ""), nil
}

func isPortFwdMarker(trimmed, marker string) bool {
	for {
		if trimmed == marker {
			return true
		}
		if !strings.HasPrefix(trimmed, "#") {
			return false
		}
		next := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if next == trimmed {
			return false
		}
		trimmed = next
	}
}

func commentMarkedBlocks(s string) (string, error) {
	lines := splitKeep(s)
	var out []string
	in := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == sshMarkerBegin {
			in = true
		}
		if in {
			out = append(out, commentIfNeeded(line))
			if t == sshMarkerEnd {
				in = false
			}
			continue
		}
		out = append(out, line)
	}
	if in {
		return "", fmt.Errorf("OpenSSH 配置中有未闭合的 PortFwd 标记，已中止写入以免破坏其余配置")
	}
	return strings.Join(out, ""), nil
}

func commentExactHostBlocks(s, host string) string {
	lines := splitKeep(s)
	var out []string
	commenting := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if isActiveHostOrMatch(t) {
			commenting = hostLineHasExact(t, host)
		}
		if commenting {
			out = append(out, commentIfNeeded(line))
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "")
}

func isActiveHostOrMatch(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	k := strings.ToLower(fields[0])
	return k == "host" || k == "match"
}

func hostLineHasExact(trimmed, host string) bool {
	fields := strings.Fields(trimmed)
	if len(fields) < 2 {
		return false
	}
	if strings.EqualFold(fields[0], "Host") {
		for _, tok := range fields[1:] {
			if tok == host {
				return true
			}
		}
		return false
	}
	if !strings.EqualFold(fields[0], "Match") {
		return false
	}
	for i := 1; i < len(fields)-1; i++ {
		if strings.EqualFold(fields[i], "host") || strings.EqualFold(fields[i], "hostname") {
			if fields[i+1] == host {
				return true
			}
		}
	}
	return false
}

func commentIfNeeded(line string) string {
	nl := ""
	body := line
	if strings.HasSuffix(body, "\r\n") {
		nl = "\r\n"
		body = strings.TrimSuffix(body, "\r\n")
	} else if strings.HasSuffix(body, "\n") {
		nl = "\n"
		body = strings.TrimSuffix(body, "\n")
	}
	if strings.TrimSpace(body) == "" {
		return line
	}
	if strings.HasPrefix(strings.TrimLeft(body, " \t"), "#") {
		return line
	}
	return "# " + body + nl
}

func splitKeep(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func sshConnectCommand(sshExe, user, host, port string) string {
	dest := host
	if u := strings.TrimSpace(user); u != "" {
		dest = u + "@" + host
	}
	return fmt.Sprintf("%s -p %s -- %s", quoteForCmd(sshExe), port, dest)
}
