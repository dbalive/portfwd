package main

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestSOCKS5ClientConnectsAndPipes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	upA, upB := net.Pipe()
	defer upA.Close()
	defer upB.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = handleSOCKS(c, func(network, address string) (net.Conn, error) {
			if network != "tcp" || address != "10.1.1.1:22" {
				return nil, io.ErrUnexpectedEOF
			}
			return upA, nil
		})
	}()

	conn, err := dialSOCKS5(ln.Addr().String(), "10.1.1.1:22")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("SSH-2.0")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 7)
	_ = upB.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(upB, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "SSH-2.0" {
		t.Fatalf("got %q", buf)
	}
}

func TestSOCKS5ClientTwoTargetsShareOneListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type pair struct {
		a, b net.Conn
	}
	up := map[string]pair{}
	for _, addr := range []string{"10.1.1.1:22", "10.1.1.2:22"} {
		a, b := net.Pipe()
		up[addr] = pair{a, b}
		defer a.Close()
		defer b.Close()
	}

	go func() {
		for i := 0; i < 2; i++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSOCKS(c, func(_ string, address string) (net.Conn, error) {
				p, ok := up[address]
				if !ok {
					return nil, io.ErrClosedPipe
				}
				return p.a, nil
			})
		}
	}()

	c1, err := dialSOCKS5(ln.Addr().String(), "10.1.1.1:22")
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	c2, err := dialSOCKS5(ln.Addr().String(), "10.1.1.2:22")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	if _, err := c1.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Write([]byte("two")); err != nil {
		t.Fatal(err)
	}
	b1 := make([]byte, 3)
	b2 := make([]byte, 3)
	_ = up["10.1.1.1:22"].b.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = up["10.1.1.2:22"].b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(up["10.1.1.1:22"].b, b1); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(up["10.1.1.2:22"].b, b2); err != nil {
		t.Fatal(err)
	}
	if string(b1) != "one" || string(b2) != "two" {
		t.Fatalf("got %q %q", b1, b2)
	}
}

func TestSOCKS5RequestDomain(t *testing.T) {
	got, err := socks5Request("intranet.local:22")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x05, 0x01, 0x00, 0x03, byte(len("intranet.local"))}
	want = append(want, []byte("intranet.local")...)
	want = append(want, 0x00, 22)
	if string(got) != string(want) {
		t.Fatalf("got %v", got)
	}
}

func TestStdioTunnelCopiesBothDirections(t *testing.T) {
	client, local := tcpPair(t)
	defer client.Close()
	defer local.Close()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inR.Close()
	defer inW.Close()
	defer outR.Close()
	defer outW.Close()

	done := make(chan struct{})
	go func() {
		stdioTunnel(local, inR, outW)
		close(done)
	}()

	if _, err := inW.Write([]byte("up")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "up" {
		t.Fatalf("got %q", buf)
	}
	if _, err := client.Write([]byte("dn")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(outR, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "dn" {
		t.Fatalf("got %q", buf)
	}
	_ = inW.Close()
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stdio tunnel did not exit")
	}
}

func TestStdioTunnelExitsWhenRemoteClosesWhileStdinOpen(t *testing.T) {
	client, local := tcpPair(t)
	defer client.Close()
	defer local.Close()
	inR, inW := io.Pipe()
	defer inR.Close()
	defer inW.Close()
	outR, outW := io.Pipe()
	defer outR.Close()
	defer outW.Close()

	done := make(chan struct{})
	go func() {
		stdioTunnel(local, inR, outW)
		close(done)
	}()

	if _, err := client.Write([]byte("bye")); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	buf := make([]byte, 3)
	if _, err := io.ReadFull(outR, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "bye" {
		t.Fatalf("got %q", buf)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hung after remote close with stdin still open")
	}
}

func TestStdioTunnelKeepsRemoteDataAfterStdinEOF(t *testing.T) {
	client, local := tcpPair(t)
	defer client.Close()
	defer local.Close()
	inR, inW := io.Pipe()
	defer inR.Close()
	outR, outW := io.Pipe()
	defer outR.Close()
	defer outW.Close()

	done := make(chan struct{})
	go func() {
		stdioTunnel(local, inR, outW)
		close(done)
	}()

	_ = inW.Close()
	if _, err := client.Write([]byte("tail")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(outR, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "tail" {
		t.Fatalf("got %q", buf)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not exit after remote close")
	}
}

func TestSOCKS5RequestIPv6(t *testing.T) {
	got, err := socks5Request("[::1]:22")
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 0x05 || got[1] != 0x01 || got[3] != 0x04 {
		t.Fatalf("header %v", got[:4])
	}
	ip := net.IP(got[4:20])
	if !ip.Equal(net.ParseIP("::1")) {
		t.Fatalf("ip %v", ip)
	}
	if got[20] != 0 || got[21] != 22 {
		t.Fatalf("port %v", got[20:])
	}
}
