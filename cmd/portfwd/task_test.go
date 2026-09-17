package main

import "testing"

func TestTaskValidateForward(t *testing.T) {
	err := Task{Name: "web", RemoteHost: "192.168.10.10", RemotePort: "443", LocalPort: "8443"}.Validate()
	if err != nil {
		t.Fatal(err)
	}
	got := Task{RemoteHost: "192.168.10.10", RemotePort: "22", LocalPort: "10022"}.RemoteAddr()
	if got != "192.168.10.10:22" {
		t.Fatalf("got %q", got)
	}
	if err := (Task{Name: "x", RemoteHost: "10.1.1.1", RemotePort: "22", LocalPort: "abc"}).Validate(); err == nil {
		t.Fatal("expected invalid local port")
	}
	if err := (Task{Name: "x", RemotePort: "22", LocalPort: "10022"}).Validate(); err == nil {
		t.Fatal("expected empty host")
	}
	if err := (Task{User: "root\nHost evil", RemoteHost: "10.1.1.1", RemotePort: "22", LocalPort: "10022"}).Validate(); err == nil {
		t.Fatal("expected invalid user")
	}
	if err := (Task{User: "root", RemoteHost: "10.1.1.1", RemotePort: "22", LocalPort: "10022"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTaskRemoteAddrIPv6(t *testing.T) {
	got := Task{RemoteHost: "2001:db8::1", RemotePort: "22"}.RemoteAddr()
	if got != "[2001:db8::1]:22" {
		t.Fatalf("got %q", got)
	}
}

func TestWithDefaultPort(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"10.1.1.1", "10.1.1.1:22"},
		{"10.1.1.1:2222", "10.1.1.1:2222"},
		{"2001:db8::1", "[2001:db8::1]:22"},
		{"[2001:db8::1]:22", "[2001:db8::1]:22"},
	}
	for _, tt := range tests {
		got := withDefaultPort(tt.in, "22")
		if got != tt.want {
			t.Fatalf("%q -> %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDuplicateLocalPortsRejected(t *testing.T) {
	err := uniqueLocalPorts([]Task{
		{ID: "a", RemoteHost: "10.1.1.1", RemotePort: "22", LocalPort: "10022"},
		{ID: "b", RemoteHost: "10.2.2.2", RemotePort: "22", LocalPort: "10022"},
	})
	if err == nil {
		t.Fatal("expected duplicate local port error")
	}
	if err := uniqueLocalPorts([]Task{
		{ID: "a", RemoteHost: "10.1.1.1", RemotePort: "22", LocalPort: "10022"},
		{ID: "b", RemoteHost: "10.2.2.2", RemotePort: "22", LocalPort: "10023"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNextLocalPortSkipsUsed(t *testing.T) {
	got := nextLocalPort([]Task{
		{LocalPort: "10022"},
		{LocalPort: "10023"},
	})
	if got != "10024" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultConfigIsProxyOnly(t *testing.T) {
	c := defaultConfig()
	if c.SSHHost != "" || c.SOCKSPort != "1080" {
		t.Fatalf("%+v", c)
	}
	if c.Remember {
		t.Fatal("default remember should be off")
	}
	if len(c.Tasks) != 0 {
		t.Fatalf("expected no default forwards, got %+v", c.Tasks)
	}
}

func TestParseConfigJSONRememberTrueEmptyHostStaysEmpty(t *testing.T) {
	got := parseConfigJSON([]byte(`{"remember":true,"sshHost":"","user":"","password":""}`))
	if !got.Remember {
		t.Fatal("remember should stay on")
	}
	if got.SSHHost != "" || got.User != "" || got.Password != "" {
		t.Fatalf("empty form must not revive a demo jump host: %+v", got)
	}
}
