package main

import (
	"fmt"
	"os"
)

const usageText = `端口转发 — 连上跳板后，本机提供一个 SOCKS5，浏览器和 SSH 共用。

  无参数                 打开界面
  proxy [socks] host port
                         OpenSSH ProxyCommand（多台内网主机共用一个 SOCKS 端口）
  help                   显示本说明

示例（10 台内网机不必开 10 个本地端口）：

  ssh -o ProxyCommand="本程序.exe proxy 127.0.0.1:1080 %h %p" user@10.1.1.1
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "proxy" {
		if err := runProxy(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("PORTFWD_ASKPASS_RUN") == "1" || (len(os.Args) > 1 && isAskpassArg(os.Args[1])) {
		runAskpass()
		return
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			_, _ = os.Stdout.WriteString(usageText)
			return
		}
	}
	runGUI()
}
