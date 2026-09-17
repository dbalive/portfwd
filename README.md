# 端口转发 (PortFwd)

Windows 跳板机图形工具。连上 SSH 跳板后，本机提供一个 SOCKS5，浏览器、OpenSSH、Xshell、SecureCRT 可以共用这一条隧道。

当前版本见 [`VERSION`](VERSION)。

## 能做什么

- **动态端口转发**：SSH 登录跳板，本机监听 `127.0.0.1:1080`（可改）SOCKS5
- **多个跳板**：可保存多台，同时只连接一台
- **固定端口转发**：把内网主机映到本机端口（类似 `ssh -L`）
- **配置 OpenSSH**：按目标网段写入 `~/.ssh/config` 的 `ProxyCommand`，多台内网机共用一个 SOCKS 口
- **打开浏览器**：用本机 SOCKS 打开 Chrome / Edge
- **托盘**：最小化进托盘，点关闭退出

界面用本机 Edge 打开。同一时间只允许运行一个实例。

## 环境

- Windows 10 / 11
- 本机已装 Microsoft Edge（界面）
- 编译需要 [Go](https://go.dev/dl/) 1.25+

跳板侧只需普通 `sshd`，不必再装 SOCKS 服务。

## 编译

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\build.ps1
```

会按 `VERSION` 生成 `PortFwd-vX.Y.Z.exe`。可执行文件不纳入 git。

```powershell
go test ./cmd/portfwd
```

## 使用

1. 运行 `PortFwd-vX.Y.Z.exe`
2. 填写跳板地址、端口、用户名、密码，回车连接
3. 顶部芯片变为已连接后，本机 SOCKS 即可用，例如：

```text
socks5://127.0.0.1:1080
```

4. 需要访问一批内网地址时，在「配置 OpenSSH」里填写目标网段（如 `192.168` 或 `192.168.1.1~10`），点 **配置 SSH** 再保存
5. 「固定端口转发」用于把某一个内网端口映到本机

勾选「保存主机配置」后，跳板和转发列表写到 `%AppData%\PortFwd\config.json`。不要把该文件提交到 git。

命令行也可给 OpenSSH 当 `ProxyCommand`（多台内网主机共用一个 SOCKS 端口）：

```text
ssh -o ProxyCommand="PortFwd.exe proxy 127.0.0.1:1080 %h %p" user@192.168.10.10
```

纯命令行 `ssh -D` 的步骤见 [docs/ssh-dynamic-forwarding.md](docs/ssh-dynamic-forwarding.md)。
