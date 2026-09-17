package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type sshHostSpec struct {
	Hosts    []string
	HostLine string
	HostName string
	Example  string
	Pattern  bool
}

type sshHostError struct{ msg string }

func (e sshHostError) Error() string { return e.msg }

func parseSSHHostSpec(raw string) (sshHostSpec, error) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return sshHostSpec{}, sshHostError{"目标主机不能为空"}
	}
	out := sshHostSpec{}
	for _, tok := range fields {
		one, err := parseSSHHostToken(tok)
		if err != nil {
			return sshHostSpec{}, sshHostError{err.Error()}
		}
		out.Hosts = append(out.Hosts, one.Hosts...)
		if one.Pattern {
			out.Pattern = true
		}
		if out.Example == "" {
			out.Example = one.Example
		}
	}
	if len(out.Hosts) == 1 && !out.Pattern {
		out.HostName = out.Hosts[0]
		out.Example = out.Hosts[0]
	} else {
		out.Pattern = true
		out.HostName = ""
	}
	out.HostLine = strings.Join(out.Hosts, " ")
	if out.Example == "" && len(out.Hosts) > 0 {
		out.Example = strings.Fields(out.Hosts[0])[0]
	}
	return out, nil
}

func parseSSHHostToken(tok string) (sshHostSpec, error) {
	s := strings.TrimSpace(tok)
	if s == "" {
		return sshHostSpec{}, fmt.Errorf("目标主机不能为空")
	}
	if i := strings.LastIndex(s, "~"); i >= 0 {
		return parseIPv4LastOctetRange(s[:i], s[i+1:])
	}
	if strings.Contains(s, "*") {
		if err := validHostGlob(s); err != nil {
			return sshHostSpec{}, err
		}
		return sshHostSpec{Hosts: []string{s}, Example: globExample(s), Pattern: true}, nil
	}
	if pattern, ok := ipv4PrefixPattern(s); ok {
		return sshHostSpec{Hosts: []string{pattern}, Example: prefixExample(s), Pattern: true}, nil
	}
	if err := validSSHHost(s); err != nil {
		return sshHostSpec{}, err
	}
	return sshHostSpec{Hosts: []string{s}, HostName: s, Example: s}, nil
}

func validHostGlob(s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsAny(s, " \t\r\n\"'`&|<>^()%;,#?!\\/") {
		return fmt.Errorf("目标主机无效")
	}
	return nil
}

func ipv4PrefixPattern(s string) (string, bool) {
	trailingDot := strings.HasSuffix(s, ".")
	core := strings.TrimSuffix(s, ".")
	if core == "" {
		return "", false
	}
	parts := strings.Split(core, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return "", false
	}
	for _, p := range parts {
		if !validOctet(p) {
			return "", false
		}
	}
	if trailingDot {
		return core + ".*", true
	}
	return core + "*", true
}

func validOctet(p string) bool {
	if p == "" || strings.HasPrefix(p, "+") || strings.HasPrefix(p, "-") {
		return false
	}
	if len(p) > 1 && strings.HasPrefix(p, "0") {
		return false
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 0 && n <= 255
}

func prefixExample(s string) string {
	core := strings.TrimSuffix(strings.TrimSpace(s), ".")
	parts := strings.Split(core, ".")
	for len(parts) < 4 {
		parts = append(parts, "1")
	}
	return strings.Join(parts[:4], ".")
}

func globExample(s string) string {
	base := strings.TrimSuffix(strings.TrimSuffix(s, "*"), ".")
	return prefixExample(base)
}

func parseIPv4LastOctetRange(startIP, endRaw string) (sshHostSpec, error) {
	if strings.Contains(startIP, ":") {
		return sshHostSpec{}, fmt.Errorf("地址段无效，示例：192.168.1.1~10")
	}
	ip := net.ParseIP(startIP)
	if ip == nil || ip.To4() == nil {
		return sshHostSpec{}, fmt.Errorf("地址段无效，示例：192.168.1.1~10")
	}
	if !validOctet(endRaw) {
		return sshHostSpec{}, fmt.Errorf("地址段无效，末位须为 0–255")
	}
	end, _ := strconv.Atoi(endRaw)
	start := int(ip.To4()[3])
	if start > end {
		return sshHostSpec{}, fmt.Errorf("地址段无效，起始地址不能大于结束地址")
	}
	base := ip.To4()
	hosts := make([]string, 0, end-start+1)
	for n := start; n <= end; n++ {
		hosts = append(hosts, fmt.Sprintf("%d.%d.%d.%d", base[0], base[1], base[2], n))
	}
	line := strings.Join(hosts, " ")
	return sshHostSpec{
		Hosts:    []string{line},
		HostLine: line,
		Example:  hosts[0],
		Pattern:  true,
	}, nil
}

func sshManualHint(user string, spec sshHostSpec, port string) string {
	dest := spec.Example
	if dest == "" && len(spec.Hosts) > 0 {
		dest = strings.Fields(spec.Hosts[0])[0]
	}
	if u := strings.TrimSpace(user); u != "" {
		dest = u + "@" + dest
	}
	cmd := "ssh"
	if p := strings.TrimSpace(port); p != "" && p != "22" {
		cmd += " -p " + p
	}
	msg := "请手动打开 CMD 或 PowerShell，执行：" + cmd + " " + dest
	if spec.Pattern && strings.Contains(spec.HostLine, "*") {
		msg += "（请把主机换成任一匹配地址）"
	}
	return msg
}
