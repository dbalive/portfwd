package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

func socks5Request(target string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("目标地址无效：%s", target)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("目标端口无效：%s", portStr)
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if host == "" || len(host) > 255 {
			return nil, fmt.Errorf("目标主机无效：%s", host)
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(port))
	return append(req, p[:]...), nil
}

func readSOCKS5Reply(r io.Reader) error {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return err
	}
	if hdr[0] != 0x05 {
		return fmt.Errorf("不是 SOCKS5")
	}
	if hdr[1] != 0x00 {
		return fmt.Errorf("SOCKS 连接失败（代码 %d）", hdr[1])
	}
	var skip int
	switch hdr[3] {
	case 0x01:
		skip = 4 + 2
	case 0x04:
		skip = 16 + 2
	case 0x03:
		n := make([]byte, 1)
		if _, err := io.ReadFull(r, n); err != nil {
			return err
		}
		skip = int(n[0]) + 2
	default:
		return fmt.Errorf("不支持的地址类型 %d", hdr[3])
	}
	buf := make([]byte, skip)
	_, err := io.ReadFull(r, buf)
	return err
}

func dialSOCKS5(proxy, target string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxy, 15*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	hello := make([]byte, 2)
	if _, err := io.ReadFull(conn, hello); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if hello[0] != 0x05 || hello[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("SOCKS 握手失败")
	}
	req, err := socks5Request(target)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := readSOCKS5Reply(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
