package main

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestPipeCopiesBothDirections(t *testing.T) {
	a, b := net.Pipe()
	c, d := net.Pipe()
	defer a.Close()
	defer b.Close()
	defer c.Close()
	defer d.Close()

	done := make(chan struct{})
	go func() {
		pipe(b, c)
		close(done)
	}()

	if _, err := a.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	_ = d.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(d, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q", buf)
	}

	if _, err := d.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	_ = a.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "world" {
		t.Fatalf("got %q", buf)
	}

	a.Close()
	d.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipe did not exit")
	}
}

func TestPipeHalfCloseAllowsRemainingData(t *testing.T) {
	client, local := tcpPair(t)
	defer client.Close()
	defer local.Close()
	origin, up := tcpPair(t)
	defer origin.Close()
	defer up.Close()

	done := make(chan struct{})
	go func() {
		pipe(local, up)
		close(done)
	}()

	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "response" {
		t.Fatalf("got %q", buf)
	}
	_ = origin.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipe did not exit")
	}
}

func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		ch <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case acc := <-ch:
		return c, acc
	case err := <-errCh:
		c.Close()
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		c.Close()
		t.Fatal("accept timeout")
	}
	panic("unreachable")
}
