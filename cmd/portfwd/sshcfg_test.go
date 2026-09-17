package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyOpenSSHConfigEmptyFile(t *testing.T) {
	got, snippet, err := applyOpenSSHConfig("", "10.1.1.9", "root", "22", `E:\tools\端口转发.exe`, "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snippet, "Host 10.1.1.9") {
		t.Fatalf("snippet %q", snippet)
	}
	if !strings.Contains(snippet, "ProxyCommand") || !strings.Contains(snippet, "proxy 127.0.0.1:1080 %h %p") {
		t.Fatalf("snippet %q", snippet)
	}
	if !strings.Contains(snippet, "PubkeyAuthentication no") {
		t.Fatalf("snippet should skip pubkey delay: %q", snippet)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "# BEGIN PortFwd SOCKS5") {
		t.Fatalf("new block should be first:\n%s", got)
	}
	if !strings.Contains(got, "User root") {
		t.Fatalf("got %s", got)
	}
	if strings.Contains(snippet, "HostName") {
		t.Fatalf("single IP must not set HostName:\n%s", snippet)
	}
}

func TestApplyOpenSSHConfigReplacesMarkedBlock(t *testing.T) {
	old := `# BEGIN PortFwd SOCKS5
Host 10.1.1.9
  ProxyCommand old.exe proxy 127.0.0.1:1080 %h %p
# END PortFwd SOCKS5

Host other
  User x
`
	got, _, err := applyOpenSSHConfig(old, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "old.exe") {
		t.Fatalf("old managed block must be removed:\n%s", got)
	}
	if strings.Count(got, "# BEGIN PortFwd SOCKS5") != 1 {
		t.Fatalf("only one managed block:\n%s", got)
	}
	if !strings.Contains(got, "Host other") || strings.Contains(got, "# Host other") {
		t.Fatalf("unrelated host must stay:\n%s", got)
	}
}

func TestApplyOpenSSHConfigKeepsExistingHostAfterBlock(t *testing.T) {
	old := `Host 10.1.1.9
  User admin
  Port 2222

Host other
  User x
`
	got, _, err := applyOpenSSHConfig(old, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Host 10.1.1.9") {
		t.Fatalf("existing host should remain for restore on exit:\n%s", got)
	}
	if strings.Contains(got, "# Host 10.1.1.9") {
		t.Fatalf("do not comment user hosts:\n%s", got)
	}
	if !strings.Contains(got, "\nHost other\n") && !strings.Contains(got, "\nHost other\r\n") {
		if !strings.Contains(got, "Host other") || strings.Contains(got, "# Host other") {
			t.Fatalf("unrelated host must stay active:\n%s", got)
		}
	}
}

func TestApplyOpenSSHConfigIdempotentWhenAlreadyFirst(t *testing.T) {
	first, snippet, err := applyOpenSSHConfig("", "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	second, snippet2, err := applyOpenSSHConfig(first, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if snippet != snippet2 {
		t.Fatal("snippet should match")
	}
	if strings.Count(second, snippet) != 1 {
		t.Fatalf("should not duplicate active block:\n%s", second)
	}
}

func TestApplyOpenSSHConfigCommentsLaterDuplicateWhenSnippetFirst(t *testing.T) {
	first, snippet, err := applyOpenSSHConfig("", "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	mixed := first + "\nHost 10.1.1.9\n  User extra\n"
	got, _, err := applyOpenSSHConfig(mixed, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, snippet) != 1 {
		t.Fatalf("must keep one live block:\n%s", got)
	}
}

func TestApplyOpenSSHConfigLeavesMatchHost(t *testing.T) {
	old := `Match host 10.1.1.9
  User admin

Host other
  User x
`
	got, _, err := applyOpenSSHConfig(old, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "# Match host 10.1.1.9") {
		t.Fatalf("do not comment Match:\n%s", got)
	}
	if !strings.Contains(got, "Match host 10.1.1.9") {
		t.Fatalf("Match host should remain:\n%s", got)
	}
	if strings.Contains(got, "# Host other") {
		t.Fatalf("unrelated host must stay:\n%s", got)
	}
}

func TestApplyOpenSSHConfigUnclosedMarkerAborts(t *testing.T) {
	old := `# BEGIN PortFwd SOCKS5
Host 10.1.1.9
  User a

Host keep
  User x
`
	_, _, err := applyOpenSSHConfig(old, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err == nil {
		t.Fatal("unclosed marker must abort so the rest of the file is not commented")
	}
}

func TestReadSSHConfigFileMissingIsEmpty(t *testing.T) {
	got, err := readSSHConfigFile(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteOpenSSHConfigKeepsFileOnReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := writeOpenSSHConfig(path, "10.2.2.2", "ops", "22", "app.exe", "127.0.0.1:1080")
	if err == nil {
		t.Fatal("unreadable config must not be treated as empty")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsDir() {
		t.Fatal("must not replace unreadable path with a new config file")
	}
}

func TestWriteOpenSSHConfigUnclosedMarkerLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	old := "# BEGIN PortFwd SOCKS5\nHost 10.1.1.9\nHost keep\n  User x\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := writeOpenSSHConfig(path, "10.1.1.9", "root", "22", "app.exe", "127.0.0.1:1080")
	if err == nil {
		t.Fatal("expected abort")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != old {
		t.Fatalf("original must be unchanged:\n%s", b)
	}
}

func TestSecureSSHPathNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := secureSSHPath(path, false); err != nil {
		t.Fatal(err)
	}
}

func TestWriteOpenSSHConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "config")
	snippet, err := writeOpenSSHConfig(path, "10.2.2.2", "ops", "2281", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, snippet) {
		t.Fatalf("file missing snippet:\n%s", got)
	}
	if !strings.Contains(got, "Port 2281") || !strings.Contains(got, "User ops") {
		t.Fatalf("got %s", got)
	}
}

func TestStripPortFwdBlocksRemovesManagedOnly(t *testing.T) {
	in := `# BEGIN PortFwd SOCKS5
Host 192.168.10.231
  ProxyCommand app.exe proxy 127.0.0.1:1080 %h %p
# END PortFwd SOCKS5

Host other
  User x
`
	got, err := stripPortFwdBlocks(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "PortFwd") || strings.Contains(got, "192.168.10.231") || strings.Contains(got, "ProxyCommand") {
		t.Fatalf("managed block should be gone:\n%s", got)
	}
	if !strings.Contains(got, "Host other") {
		t.Fatalf("got %s", got)
	}
}

func TestClearOpenSSHConfigRemovesBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	_, err := writeOpenSSHConfig(path, "10.2.2.2", "ops", "22", "app.exe", "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(mustRead(t, path)+"\nHost keep\n  User a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearOpenSSHConfig(path); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, path)
	if strings.Contains(got, "BEGIN PortFwd") || strings.Contains(got, "10.2.2.2") {
		t.Fatalf("cleared file still has managed config:\n%s", got)
	}
	if !strings.Contains(got, "Host keep") {
		t.Fatalf("got %s", got)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
