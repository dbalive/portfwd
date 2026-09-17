package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

func socks5SuccessReply() []byte {
	return []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
}

func socks5FailReply(code byte) []byte {
	return []byte{0x05, code, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
}

func parseSOCKS5Target(r io.Reader) (string, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return "", err
	}
	if hdr[0] != 0x05 {
		return "", fmt.Errorf("不是 SOCKS5")
	}
	if hdr[1] != 0x01 {
		return "", fmt.Errorf("只支持 CONNECT")
	}

	var host string
	switch hdr[3] {
	case 0x01:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case 0x03:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return "", err
		}
		name := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(r, name); err != nil {
			return "", err
		}
		host = string(name)
	case 0x04:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(r, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	default:
		return "", fmt.Errorf("不支持的地址类型 %d", hdr[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(r, portBuf); err != nil {
		return "", err
	}
	port := int(binary.BigEndian.Uint16(portBuf))
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func handleSOCKS(conn net.Conn, dial func(network, address string) (net.Conn, error)) error {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[0] != 0x05 {
		return fmt.Errorf("不是 SOCKS5")
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	noAuth := false
	for _, m := range methods {
		if m == 0x00 {
			noAuth = true
			break
		}
	}
	if !noAuth {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return fmt.Errorf("客户端不支持无认证 SOCKS")
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return err
	}

	target, err := parseSOCKS5Target(conn)
	if err != nil {
		_, _ = conn.Write(socks5FailReply(0x07))
		return err
	}
	_ = conn.SetDeadline(time.Time{})

	up, err := dial("tcp", target)
	if err != nil {
		_, _ = conn.Write(socks5FailReply(0x05))
		return err
	}
	defer up.Close()

	if _, err := conn.Write(socks5SuccessReply()); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Time{})
	pipe(conn, up)
	return nil
}
