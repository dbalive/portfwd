package main

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"
)

type terminalReq struct {
	App       string `json:"app"`
	Host      string `json:"host"`
	Port      string `json:"port"`
	User      string `json:"user"`
	Password  string `json:"password"`
	SOCKSHost string `json:"-"`
	SOCKSPort string `json:"-"`
}

type detectedApps struct {
	Xshell    string `json:"xshell"`
	SecureCRT string `json:"securecrt"`
	SSH       string `json:"ssh"`
}

var (
	appsOnce   sync.Once
	appsCached detectedApps
)

func firstExisting(paths []string) string {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func pfRoots() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	add(os.Getenv("ProgramFiles"))
	add(os.Getenv("ProgramFiles(x86)"))
	add(os.Getenv("LOCALAPPDATA"))
	if len(out) == 0 {
		add(`C:\Program Files`)
	}
	return out
}

func xshellCandidates() []string {
	var out []string
	vers := []string{"11", "10", "9", "8", "7", "6", "5"}
	for _, root := range pfRoots() {
		ns := filepath.Join(root, "NetSarang")
		for _, ver := range vers {
			out = append(out, filepath.Join(ns, "Xshell "+ver, "Xshell.exe"))
			out = append(out, filepath.Join(ns, "XshellPlus "+ver, "Xshell.exe"))
		}
		out = append(out,
			filepath.Join(ns, "Xshell", "Xshell.exe"),
			filepath.Join(ns, "Xshell Plus", "Xshell.exe"),
			filepath.Join(ns, "Xshell.exe"),
		)
	}
	return out
}

func secureCRTCandidates() []string {
	var out []string
	for _, root := range pfRoots() {
		base := filepath.Join(root, "VanDyke Software")
		for _, ver := range []string{"SecureCRT", "SecureCRT 10", "SecureCRT 9", "SecureCRT 8", "SecureCRT 7"} {
			out = append(out, filepath.Join(base, ver, "SecureCRT.exe"))
		}
	}
	return out
}

func sshCandidates() []string {
	var out []string
	if p, err := exec.LookPath("ssh.exe"); err == nil {
		out = append(out, p)
	}
	if p, err := exec.LookPath("ssh"); err == nil {
		out = append(out, p)
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	out = append(out,
		filepath.Join(root, "System32", "OpenSSH", "ssh.exe"),
		filepath.Join(root, "Sysnative", "OpenSSH", "ssh.exe"),
	)
	for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if pf == "" {
			continue
		}
		out = append(out,
			filepath.Join(pf, "Git", "usr", "bin", "ssh.exe"),
			filepath.Join(pf, "Git", "bin", "ssh.exe"),
		)
	}
	return out
}

func netsarangRoots() []string {
	var out []string
	for _, root := range pfRoots() {
		out = append(out, filepath.Join(root, "NetSarang"), filepath.Join(root, "Programs", "NetSarang"))
	}
	return out
}

func findNamedUnder(root, name string, maxDepth int) string {
	root = strings.TrimSpace(root)
	if root == "" || name == "" {
		return ""
	}
	var found string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(os.PathSeparator)))
		}
		if d.IsDir() {
			if maxDepth >= 0 && depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), name) {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func extraXshellPaths() []string {
	var out []string
	out = append(out, extraUninstallPaths("Xshell", "Xshell.exe")...)
	if p, err := exec.LookPath("Xshell.exe"); err == nil {
		out = append(out, p)
	}
	for _, root := range netsarangRoots() {
		if p := findNamedUnder(root, "Xshell.exe", 4); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extraSecureCRTPaths() []string {
	var out []string
	out = append(out, extraUninstallPaths("SecureCRT", "SecureCRT.exe")...)
	if p, err := exec.LookPath("SecureCRT.exe"); err == nil {
		out = append(out, p)
	}
	return out
}

func detectApps() detectedApps {
	appsOnce.Do(func() {
		appsCached = detectedApps{
			Xshell:    firstExisting(append(xshellCandidates(), extraXshellPaths()...)),
			SecureCRT: firstExisting(append(secureCRTCandidates(), extraSecureCRTPaths()...)),
			SSH:       firstExisting(sshCandidates()),
		}
	})
	return appsCached
}

func quoteForCmd(s string) string {
	if !strings.ContainsAny(s, " \t\"&<>|^()") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func sshDirectCmdLine(sshExe, user, host, port string) string {
	return sshConnectCommand(sshExe, user, host, port)
}

func xshellURL(user, host, port string) string {
	if u := strings.TrimSpace(user); u != "" {
		return fmt.Sprintf("ssh://%s@%s:%s", u, host, port)
	}
	return fmt.Sprintf("ssh://%s:%s", host, port)
}

func secureCRTArgs(host, port, user string) []string {
	args := []string{"/SSH2"}
	if u := strings.TrimSpace(user); u != "" {
		args = append(args, "/L", u)
	}
	return append(args, "/P", port, host)
}

func launchSSH(sshExe string, in terminalReq) error {
	if sshExe == "" {
		return fmt.Errorf("未安装该终端")
	}
	script, err := writeSSHSessionScript(sshExe, in)
	if err != nil {
		return err
	}
	return startDetachedConsole("端口转发 SSH", script)
}

func writeSSHSessionScript(sshExe string, in terminalReq) (string, error) {
	path := filepath.Join(os.TempDir(), "portfwd-ssh.cmd")
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString("chcp 65001 >nul\r\n")
	if in.Password != "" {
		b.WriteString("echo 目标密码已在界面明文填写，可对照输入。\r\n")
	}
	b.WriteString(sshDirectCmdLine(sshExe, in.User, in.Host, in.Port) + "\r\n")
	b.WriteString("echo.\r\n")
	b.WriteString("echo ----- 会话结束，按任意键关闭窗口 -----\r\n")
	b.WriteString("pause >nul\r\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func isAskpassArg(arg string) bool {
	a := strings.ToLower(strings.TrimSpace(arg))
	if a == "" || a == "proxy" || a == "help" || a == "-h" || a == "--help" {
		return false
	}
	if a == "askpass" {
		return true
	}
	return strings.Contains(a, "password") || strings.Contains(a, "passphrase") || strings.Contains(a, "密码")
}

func askpassPasswordPath() string {
	return filepath.Join(os.TempDir(), "portfwd-askpass.pass")
}

func writeAskpassPassword(pw string) error {
	return os.WriteFile(askpassPasswordPath(), []byte(pw), 0o600)
}

func askpassOutput() string {
	if p := os.Getenv("PORTFWD_ASKPASS"); p != "" {
		return p
	}
	b, err := os.ReadFile(askpassPasswordPath())
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\r\n")
}

func runAskpass() {
	p := askpassOutput()
	_, _ = os.Stdout.WriteString(p)
	if !strings.HasSuffix(p, "\n") {
		_, _ = os.Stdout.WriteString("\n")
	}
}

func mergeAskpassEnv(base []string, askpass, password string) []string {
	out := make([]string, 0, len(base)+4)
	for _, e := range base {
		k, _, ok := strings.Cut(e, "=")
		if !ok {
			out = append(out, e)
			continue
		}
		switch strings.ToUpper(k) {
		case "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "PORTFWD_ASKPASS", "PORTFWD_ASKPASS_RUN", "DISPLAY":
			continue
		}
		out = append(out, e)
	}
	return append(out,
		"SSH_ASKPASS="+askpass,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=:0",
		"PORTFWD_ASKPASS="+password,
		"PORTFWD_ASKPASS_RUN=1",
	)
}

func writeAskpassScript(exe string) (string, error) {
	path := filepath.Join(os.TempDir(), "portfwd-askpass.cmd")
	body := "@echo off\r\n" + quoteForCmd(exe) + " askpass\r\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func validSSHHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" || strings.HasPrefix(host, "-") {
		return fmt.Errorf("目标主机无效")
	}
	if strings.ContainsAny(host, " \t\r\n\"'`&|<>^()%;,#*?!\\/") {
		return fmt.Errorf("目标主机无效")
	}
	ip := host
	if strings.HasPrefix(ip, "[") && strings.HasSuffix(ip, "]") {
		ip = ip[1 : len(ip)-1]
	}
	if net.ParseIP(ip) != nil {
		return nil
	}
	for _, lab := range strings.Split(host, ".") {
		if lab == "" || strings.HasPrefix(lab, "-") || strings.HasSuffix(lab, "-") {
			return fmt.Errorf("目标主机无效")
		}
		for _, r := range lab {
			ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-'
			if !ok {
				return fmt.Errorf("目标主机无效")
			}
		}
	}
	return nil
}

func validSSHUser(user string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil
	}
	if strings.HasPrefix(user, "-") || strings.ContainsAny(user, " \t\r\n\"'`&|<>^()%;,#*?!\\/") {
		return fmt.Errorf("用户名无效")
	}
	return nil
}

func parseTerminalReq(in terminalReq, socks string) (terminalReq, error) {
	app := strings.ToLower(strings.TrimSpace(in.App))
	if app != "xshell" && app != "securecrt" && app != "ssh" {
		return terminalReq{}, fmt.Errorf("不支持的终端")
	}
	host := strings.TrimSpace(in.Host)
	if err := validSSHHost(host); err != nil {
		return terminalReq{}, err
	}
	port, err := parsePort(in.Port)
	if err != nil {
		return terminalReq{}, fmt.Errorf("目标端口：%w", err)
	}
	user := strings.TrimSpace(in.User)
	if err := validSSHUser(user); err != nil {
		return terminalReq{}, err
	}
	sh, sp, err := net.SplitHostPort(strings.TrimSpace(socks))
	if err != nil {
		return terminalReq{}, fmt.Errorf("SOCKS 地址无效")
	}
	sp, err = parsePort(sp)
	if err != nil {
		return terminalReq{}, err
	}
	if sh != "127.0.0.1" && sh != "localhost" && sh != "::1" {
		return terminalReq{}, fmt.Errorf("SOCKS 只允许本机地址")
	}
	return terminalReq{App: app, Host: host, Port: port, User: user, Password: in.Password, SOCKSHost: sh, SOCKSPort: sp}, nil
}

func xshellSession(in terminalReq) []byte {
	user := strings.TrimSpace(in.User)
	usePass := "0"
	if user != "" {
		usePass = "1"
	}
	body := fmt.Sprintf(`[CONNECTION]
Host=%s
Port=%s
Protocol=SSH
Description=PortFwd

[CONNECTION:AUTHENTICATION]
UserName=%s
Password=
UsePassword=%s

[CONNECTION:PROXY]
UseProxy=0
`, in.Host, in.Port, user, usePass)
	return utf16LEBOM(body)
}

func utf16LEBOM(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(u)*2)
	out[0], out[1] = 0xff, 0xfe
	for i, c := range u {
		out[2+i*2] = byte(c)
		out[3+i*2] = byte(c >> 8)
	}
	return out
}

func crtD(n int) string {
	return fmt.Sprintf("%08x", n)
}

func atoiPort(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func secureCRTSession(in terminalReq) string {
	sshPort := atoiPort(in.Port)
	return fmt.Sprintf(`S:"Hostname"=%s
S:"Protocol Name"=SSH2
D:"[SSH2] Port"=%s
S:"Username"=%s
S:"Firewall Name"=None
`, in.Host, crtD(sshPort), in.User)
}

func writeXshellSession(dir string, in terminalReq) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "portfwd.xsh")
	if err := os.WriteFile(p, xshellSession(in), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

func writeSecureCRTConfig(dir string, in terminalReq) error {
	sess := filepath.Join(dir, "Sessions")
	if err := os.MkdirAll(sess, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(sess, "PortFwd.ini"), []byte(secureCRTSession(in)), 0o600)
}

func launchTerminal(exe string, in terminalReq) error {
	if exe == "" {
		return fmt.Errorf("未安装该终端")
	}
	dir := filepath.Join(os.TempDir(), "portfwd-term")
	switch in.App {
	case "xshell":
		p, err := writeXshellSession(dir, in)
		if err != nil {
			return err
		}
		return exec.Command(exe, "-newwin", p).Start()
	case "securecrt":
		return exec.Command(exe, secureCRTArgs(in.Host, in.Port, in.User)...).Start()
	case "ssh":
		return launchSSH(exe, in)
	default:
		return fmt.Errorf("不支持的终端")
	}
}

func cmdStartArgv(title, target string) []string {
	if !strings.Contains(title, " ") {
		title = title + " "
	}
	return []string{"/C", "start", title, target}
}

func openInteractiveShell(kind string) error {
	if strings.ToLower(strings.TrimSpace(kind)) != "cmd" {
		return fmt.Errorf("不支持的终端")
	}
	return startDetachedConsole("端口转发 CMD", "cmd.exe")
}
