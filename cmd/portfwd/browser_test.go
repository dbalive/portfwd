package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProxiedBrowserArgs(t *testing.T) {
	got, err := proxiedBrowserArgs("127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	if !containsAll(got, "--proxy-server=socks5://127.0.0.1:1080") {
		t.Fatalf("missing proxy: %s", joined)
	}
	if !containsAll(got, "about:blank") {
		t.Fatalf("should open blank page: %s", joined)
	}
	if containsAll(got, "--app=") {
		t.Fatal("proxied browser must show address bar, not --app")
	}
	if !containsAll(got, "--host-resolver-rules=") {
		t.Fatal("need remote DNS via SOCKS")
	}
	if !containsAll(got, "--disable-quic") {
		t.Fatal("HTTPS must not use QUIC around SOCKS")
	}
	if !containsAll(got, "UseDnsHttpsSvcb") {
		t.Fatal("need to disable HTTPS DNS records that skip SOCKS")
	}
	if !containsAll(got, "--test-type") {
		t.Fatal("need --test-type to hide the unsupported-flag infobar")
	}
	ui := false
	for _, a := range got {
		if strings.Contains(a, "portfwd-ui") {
			ui = true
		}
	}
	if ui {
		t.Fatal("must not reuse the control-panel profile")
	}
}

func TestProxiedBrowserArgsRejectsBadSOCKS(t *testing.T) {
	if _, err := proxiedBrowserArgs("127.0.0.1:1080&calc"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := proxiedBrowserArgs(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenBrowserAPIRequiresConnection(t *testing.T) {
	addr, _, _, closer, err := startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post("http://"+addr+"/api/browser", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func containsAll(args []string, needle string) bool {
	for _, a := range args {
		if strings.Contains(a, needle) || a == needle {
			return true
		}
	}
	return false
}
