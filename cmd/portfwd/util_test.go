package main

import "testing"

func TestParsePort(t *testing.T) {
	got, err := parsePort("8443")
	if err != nil || got != "8443" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := parsePort("8443&calc"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parsePort("0"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parsePort("65536"); err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPSURL(t *testing.T) {
	if got := httpsURL("192.168.10.10:443"); got != "https://192.168.10.10" {
		t.Fatalf("got %q", got)
	}
	if got := httpsURL("192.168.10.10:8443"); got != "https://192.168.10.10:8443" {
		t.Fatalf("got %q", got)
	}
}

func TestParseRemote(t *testing.T) {
	got, err := parseRemote("192.168.10.10")
	if err != nil || got != "192.168.10.10:443" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := parseRemote("192.168.10.10:99999"); err == nil {
		t.Fatal("expected error")
	}
}
