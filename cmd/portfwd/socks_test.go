package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestParseSOCKS5TargetIPv4(t *testing.T) {
	req := []byte{0x05, 0x01, 0x00, 0x01, 192, 168, 10, 10, 0x01, 0xbb}
	got, err := parseSOCKS5Target(bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.10.10:443" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSOCKS5TargetDomain(t *testing.T) {
	host := "example.com"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, 0x00, 0x50)
	got, err := parseSOCKS5Target(bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	if got != "example.com:80" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSOCKS5TargetRejectsUDP(t *testing.T) {
	req := []byte{0x05, 0x03, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x50}
	_, err := parseSOCKS5Target(bytes.NewReader(req))
	if err == nil {
		t.Fatal("expected error for UDP associate")
	}
}

func TestSOCKS5HandshakeAndConnect(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	upA, upB := net.Pipe()
	defer upA.Close()
	defer upB.Close()

	done := make(chan error, 1)
	go func() {
		done <- handleSOCKS(server, func(network, address string) (net.Conn, error) {
			if network != "tcp" || address != "192.168.10.10:443" {
				return nil, io.ErrUnexpectedEOF
			}
			return upA, nil
		})
	}()

	// greeting
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("greeting reply %v", reply)
	}

	// CONNECT 192.168.10.10:443
	req := []byte{0x05, 0x01, 0x00, 0x01, 192, 168, 10, 10, 0x01, 0xbb}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 10)
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatal(err)
	}
	if resp[0] != 0x05 || resp[1] != 0x00 {
		t.Fatalf("connect reply %v", resp)
	}

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = upB.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(upB, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("upstream got %q", buf)
	}

	client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestSOCKS5ReplyLength(t *testing.T) {
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, 0)
	if len(socks5SuccessReply()) != 10 {
		t.Fatalf("reply must be 10 bytes, got %d", len(socks5SuccessReply()))
	}
}

func TestParseSOCKS5TargetIPv6(t *testing.T) {
	req := []byte{0x05, 0x01, 0x00, 0x04}
	req = append(req, net.ParseIP("::1").To16()...)
	req = append(req, 0x01, 0xbb)
	got, err := parseSOCKS5Target(bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	if got != "[::1]:443" {
		t.Fatalf("got %q", got)
	}
}

func TestSOCKS5RejectsUnsupportedAuth(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- handleSOCKS(server, func(string, string) (net.Conn, error) {
			return nil, io.ErrUnexpectedEOF
		})
	}()
	if _, err := client.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0xff {
		t.Fatalf("got %v", reply)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestSOCKS5DialFailureWritesReply(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan error, 1)
	go func() {
		done <- handleSOCKS(server, func(string, string) (net.Conn, error) {
			return nil, io.ErrClosedPipe
		})
	}()
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(client, greet); err != nil {
		t.Fatal(err)
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, 192, 168, 10, 10, 0x01, 0xbb}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	resp := make([]byte, 10)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, resp); err != nil {
		t.Fatal(err)
	}
	if resp[0] != 0x05 || resp[1] != 0x05 || len(resp) != 10 {
		t.Fatalf("got %v", resp)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
}
