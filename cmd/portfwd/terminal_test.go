package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestFirstExisting(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.exe")
	b := filepath.Join(dir, "b.exe")
	if err := os.WriteFile(b, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := firstExisting([]string{a, b})
	if got != b {
		t.Fatalf("got %q", got)
	}
	if firstExisting([]string{a}) != "" {
		t.Fatal("expected empty")
	}
}

func TestXshellSessionEmptyUser(t *testing.T) {
	raw := xshellSession(terminalReq{Host: "127.0.0.1", Port: "10022"})
	text := decodeUTF16LE(t, raw[2:])
	if strings.Contains(text, "UserName=test") || strings.Contains(text, "UserName=root") {
		t.Fatalf("must not prefill username:\n%s", text)
	}
	if !strings.Contains(text, "UserName=") {
		t.Fatalf("expected empty username key:\n%s", text)
	}
}

func TestSecureCRTArgsOmitEmptyUser(t *testing.T) {
	got := strings.Join(secureCRTArgs("127.0.0.1", "10022", ""), " ")
	if strings.Contains(got, "/L") {
		t.Fatalf("empty user must not pass /L: %s", got)
	}
}

func TestXshellSessionContainsTarget(t *testing.T) {
	raw := xshellSession(terminalReq{
		Host: "10.1.1.9", Port: "22", User: "root",
	})
	if len(raw) < 4 || raw[0] != 0xff || raw[1] != 0xfe {
		t.Fatal("need UTF-16 LE BOM")
	}
	text := decodeUTF16LE(t, raw[2:])
	for _, want := range []string{"10.1.1.9", "22", "root"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}

func TestSecureCRTSessionContainsTarget(t *testing.T) {
	text := secureCRTSession(terminalReq{
		Host: "10.1.1.8", Port: "22", User: "ops",
	})
	for _, want := range []string{"10.1.1.8", "ops", "00000016"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
}

func TestParseTerminalReq(t *testing.T) {
	got, err := parseTerminalReq(terminalReq{App: "xshell", Host: "10.2.2.2", Port: "22", User: "a"}, "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if got.App != "xshell" || got.Host != "10.2.2.2" || got.SOCKSPort != "1080" {
		t.Fatalf("%+v", got)
	}
	sshReq, err := parseTerminalReq(terminalReq{App: "ssh", Host: "10.2.2.2", Port: "2281", User: "root"}, "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if sshReq.App != "ssh" || sshReq.Port != "2281" {
		t.Fatalf("%+v", sshReq)
	}
	if _, err := parseTerminalReq(terminalReq{App: "putty", Host: "10.2.2.2", Port: "22"}, "127.0.0.1:1080"); err == nil {
		t.Fatal("expected bad app")
	}
	if _, err := parseTerminalReq(terminalReq{App: "xshell", Host: "10.2.2.2\nssh", Port: "22"}, "127.0.0.1:1080"); err == nil {
		t.Fatal("expected bad host")
	}
	if _, err := parseTerminalReq(terminalReq{App: "ssh", Host: "10.2.2.2", Port: "22", User: "root&calc"}, "127.0.0.1:1080"); err == nil {
		t.Fatal("expected bad user")
	}
}

func TestParseTerminalReqRejectsSSHPatterns(t *testing.T) {
	socks := "127.0.0.1:1080"
	bads := []terminalReq{
		{App: "ssh", Host: "*", Port: "22", User: "root"},
		{App: "ssh", Host: "10.*", Port: "22", User: "root"},
		{App: "ssh", Host: "-L80:evil:22", Port: "22"},
		{App: "ssh", Host: "10.1.1.9#foo", Port: "22", User: "root"},
		{App: "ssh", Host: "10.1.1.9", Port: "22", User: "-o"},
		{App: "ssh", Host: "host?x", Port: "22", User: "root"},
	}
	for _, in := range bads {
		if _, err := parseTerminalReq(in, socks); err == nil {
			t.Fatalf("expected reject %+v", in)
		}
	}
	if _, err := parseTerminalReq(terminalReq{App: "ssh", Host: "inner.lab.local", Port: "22", User: "ops"}, socks); err != nil {
		t.Fatal(err)
	}
}

func TestFindNamedUnder(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "NetSarang", "Xshell 10", "Xshell.exe")
	if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := findNamedUnder(root, "Xshell.exe", 4)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if findNamedUnder(root, "Missing.exe", 4) != "" {
		t.Fatal("expected empty")
	}
}

func TestXshellCandidatesIncludeNewerLayouts(t *testing.T) {
	joined := strings.Join(xshellCandidates(), "\n")
	for _, want := range []string{`Xshell 10`, `Xshell Plus`, `Xshell.exe`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
}

func TestSSHCandidatesIncludeOpenSSH(t *testing.T) {
	joined := strings.ToLower(strings.Join(sshCandidates(), "\n"))
	if !strings.Contains(joined, `openssh`+string(os.PathSeparator)+`ssh.exe`) && !strings.Contains(joined, "ssh.exe") {
		t.Fatalf("need ssh.exe candidates:\n%s", joined)
	}
}

func TestSSHDirectCmdLineHasNoProxyCommand(t *testing.T) {
	got := sshDirectCmdLine(`C:\Windows\System32\OpenSSH\ssh.exe`, "root", "10.1.1.9", "22")
	if strings.Contains(got, "ProxyCommand") {
		t.Fatalf("direct ssh must not use ProxyCommand: %s", got)
	}
	if strings.Contains(got, "127.0.0.1") {
		t.Fatalf("must ssh to the real host: %s", got)
	}
	if !strings.Contains(got, "-p 22") || !strings.Contains(got, "root@10.1.1.9") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, " -- ") {
		t.Fatalf("ssh dest must follow --: %s", got)
	}
}

func TestWriteSSHSessionScriptKeepsWindow(t *testing.T) {
	path, err := writeSSHSessionScript(`C:\Windows\System32\OpenSSH\ssh.exe`, terminalReq{
		User: "root", Host: "10.1.1.9", Port: "22",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "ssh.exe") || !strings.Contains(text, "root@10.1.1.9") {
		t.Fatalf("got %s", text)
	}
	if !strings.Contains(text, "pause") {
		t.Fatal("script must pause so the window stays")
	}
}

func TestAskpassOutputUsesEnv(t *testing.T) {
	t.Setenv("PORTFWD_ASKPASS", "p@ss&word")
	if got := askpassOutput(); got != "p@ss&word" {
		t.Fatalf("got %q", got)
	}
}

func TestAskpassOutputUsesFile(t *testing.T) {
	t.Setenv("PORTFWD_ASKPASS", "")
	if err := os.WriteFile(askpassPasswordPath(), []byte("file-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(askpassPasswordPath())
	if got := askpassOutput(); got != "file-secret" {
		t.Fatalf("got %q", got)
	}
}

func TestIsAskpassArg(t *testing.T) {
	if !isAskpassArg("askpass") || !isAskpassArg("root@10.1.1.9's password:") {
		t.Fatal("should detect askpass")
	}
	if !isAskpassArg("Password:") || !isAskpassArg("Enter passphrase for key") {
		t.Fatal("should detect password prompt")
	}
	if isAskpassArg("proxy") || isAskpassArg("help") || isAskpassArg("") {
		t.Fatal("should not treat other args as askpass")
	}
}

func TestMergeAskpassEnv(t *testing.T) {
	got := mergeAskpassEnv([]string{"PATH=C:\\Windows", "SSH_ASKPASS=old", "PORTFWD_ASKPASS=x"}, `C:\ask.cmd`, "secret")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, `SSH_ASKPASS=C:\ask.cmd`) || !strings.Contains(joined, "SSH_ASKPASS_REQUIRE=force") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(joined, "PORTFWD_ASKPASS=secret") || !strings.Contains(joined, "PORTFWD_ASKPASS_RUN=1") {
		t.Fatalf("missing password env: %q", got)
	}
	if strings.Count(joined, "SSH_ASKPASS=") != 1 {
		t.Fatalf("duplicate env: %q", got)
	}
	n := 0
	for _, e := range got {
		if strings.HasPrefix(e, "PORTFWD_ASKPASS=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("duplicate password env: %q", got)
	}
}

func TestWriteAskpassScriptCallsExe(t *testing.T) {
	path, err := writeAskpassScript(`E:\tools\端口转发.exe`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "askpass") || !strings.Contains(text, `端口转发.exe`) {
		t.Fatalf("got %s", text)
	}
}

func TestParseTerminalReqKeepsPassword(t *testing.T) {
	got, err := parseTerminalReq(terminalReq{App: "ssh", Host: "10.2.2.2", Port: "22", User: "root", Password: "a&b"}, "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "a&b" {
		t.Fatalf("%+v", got)
	}
}

func TestXshellURL(t *testing.T) {
	got := xshellURL("root", "127.0.0.1", "52341")
	if got != "ssh://root@127.0.0.1:52341" {
		t.Fatalf("got %q", got)
	}
}

func TestSecureCRTArgsDirectSSH(t *testing.T) {
	got := strings.Join(secureCRTArgs("127.0.0.1", "52341", "root"), " ")
	if strings.Contains(got, "/F") {
		t.Fatalf("must use default config, not isolated /F: %s", got)
	}
	if !strings.Contains(got, "/SSH2") || !strings.Contains(got, "/P 52341") || !strings.Contains(got, "127.0.0.1") {
		t.Fatalf("got %q", got)
	}
}

func TestXshellSessionDirectHasNoProxy(t *testing.T) {
	raw := xshellSession(terminalReq{
		Host: "127.0.0.1", Port: "52341", User: "root",
	})
	text := decodeUTF16LE(t, raw[2:])
	if strings.Contains(text, "UseProxy=1") {
		t.Fatalf("local forward session must not enable proxy:\n%s", text)
	}
	if !strings.Contains(text, "Host=127.0.0.1") || !strings.Contains(text, "Port=52341") {
		t.Fatalf("missing local target:\n%s", text)
	}
}

func TestSecureCRTSessionDirectHasNoFirewall(t *testing.T) {
	text := secureCRTSession(terminalReq{Host: "127.0.0.1", Port: "52341", User: "root"})
	if strings.Contains(text, "PortFwdSOCKS") {
		t.Fatal("must not reference missing firewall name")
	}
	if !strings.Contains(text, `S:"Hostname"=127.0.0.1`) {
		t.Fatalf("got %s", text)
	}
}

func TestSSHConfigAPIRequiresConnection(t *testing.T) {
	addr, _, _, closer, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post("http://"+addr+"/api/ssh-config", "application/json", strings.NewReader(`{"host":"10.1.1.1","port":"22","user":"root"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestTerminalAPIRequiresConnection(t *testing.T) {
	addr, _, _, closer, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post("http://"+addr+"/api/terminal", "application/json", strings.NewReader(`{"app":"xshell","host":"10.1.1.1","port":"22","user":"root"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestDetectAppsReportsInstalledTools(t *testing.T) {
	got := detectApps()
	t.Logf("xshell=%q ssh=%q securecrt=%q", got.Xshell, got.SSH, got.SecureCRT)
	if got.SSH == "" && firstExisting(sshCandidates()) != "" {
		t.Fatal("ssh should be detected")
	}
}

func TestQuoteForCmdDoublesEmbeddedQuotes(t *testing.T) {
	got := quoteForCmd(`C:\Program Files\OpenSSH\ssh.exe`)
	if got != `"C:\Program Files\OpenSSH\ssh.exe"` {
		t.Fatalf("got %q", got)
	}
	got = quoteForCmd(`C:\dir\"odd"\ssh.exe`)
	if got != `"C:\dir\""odd""\ssh.exe"` {
		t.Fatalf("got %q", got)
	}
}

func TestCmdStartArgvQuotesTitleForWindowsStart(t *testing.T) {
	got := cmdStartArgv("端口转发 CMD", "cmd.exe")
	want := []string{"/C", "start", "端口转发 CMD", "cmd.exe"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got[2], " ") {
		t.Fatal("title must contain a space so cmd start treats it as a window title")
	}
	if err := openInteractiveShell("powershell"); err == nil {
		t.Fatal("powershell should be removed")
	}
}

func decodeUTF16LE(t *testing.T, b []byte) string {
	t.Helper()
	if len(b)%2 != 0 {
		t.Fatal("odd utf16")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return string(utf16.Decode(u))
}
