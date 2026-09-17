# Windows 上用 OpenSSH `-D` 经跳板访问内网

我这边的情况是：PC 上打不开 `https://192.168.10.10`，但跳板 `192.168.1.1` 上可以。不想在跳板上再装 SOCKS 服务，也不想依赖单独的小工具时，直接用系统自带的 `ssh`，在本机开一个 SOCKS5，让浏览器或 `curl` 把流量塞进 SSH，由跳板去连内网就行。

命令要在 **Windows 本机** 执行。人已经 SSH 登录在跳板上再跑 `ssh -D`，SOCKS 是开在跳板的 `127.0.0.1` 上的，PC 浏览器用不上。

下面按实际操作顺序写。文中的检查命令我在 2026-09-15 于本机跑过一遍；跳板账号仍是密码登录（公钥还没配到 `test` 上），所以开隧道那步需要你在终端里亲手输密码。

---

## 先确认客户端和跳板端口

Win10/11 一般自带 OpenSSH，路径通常是 `C:\Windows\System32\OpenSSH\ssh.exe`。

```powershell
ssh -V
```

本机输出：

```text
OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2
```

跳板 SSH 不在 22，而是 **2222**（和 PortFwd 里保存的一致）。先看 TCP 能不能通：

```powershell
Test-NetConnection 192.168.1.1 -Port 2222
```

本机 `TcpTestSucceeded : True`。

再确认一下：没隧道时，内网 HTTPS 从 PC 直连应当失败。Windows 请用 `curl.exe`，避免和 PowerShell 的 `curl` 别名搞混：

```powershell
curl.exe -sk --connect-timeout 8 -o NUL -w "direct=%{http_code}`n" https://192.168.10.10/
```

本机：`direct=000`（连不上或超时，正常）。

此时若去连本机 1080 的 SOCKS，同样不应成功：

```powershell
curl.exe -sk --connect-timeout 3 --socks5-hostname 127.0.0.1:1080 -o NUL -w "socks=%{http_code}`n" https://192.168.10.10/
```

本机：`socks=000`（当时 1080 没有在监听）。

---

## 开隧道：`ssh -N -D`

`-D` 是动态转发：在本机起一个 SOCKS5，每次 CONNECT 的目标地址由客户端指定，SSH 在跳板那边代为建 TCP。跳板上 **不会** 多出一个叫 socks 的常驻服务，只是 sshd 帮你转发。

开一个 PowerShell 窗口，保持不关：

```powershell
ssh -N -D 127.0.0.1:1080 -p 2222 test@192.168.1.1
```

- `-N`：不进入远程 shell，只做转发。
- `-D 127.0.0.1:1080`：SOCKS 只绑在本机回环，别的机器连不上你的 1080。
- 第一次连会核对主机密钥；我这边 `known_hosts` 里已经有 `[192.168.1.1]:2222`，一般不会再问。

登录成功后，另开一个窗口检查 1080 是否在听：

```powershell
Get-NetTCPConnection -LocalPort 1080 -State Listen
```

应看到 `LocalAddress 127.0.0.1`，进程是 `ssh.exe`。

用 curl 走 SOCKS 访问同一个内网站点：

```powershell
curl.exe -sk --connect-timeout 15 --socks5-hostname 127.0.0.1:1080 -o NUL -w "socks=%{http_code}`n" https://192.168.10.10/
```

隧道正常时，我这边曾得到 `socks=200`（页面能通，状态码随站点而变，关键是不要仍是 `000`）。`-k` 是内网自签证书时图省事，正式用应配好 CA。

关掉第一个窗口里的 `ssh`，隧道就没了，`socks` 会回到 `000`。

---

## 写 `~/.ssh/config`，少敲参数

文件：`C:\Users\<你的用户名>\.ssh\config`，没有就新建。建议目录权限只给自己。

最小片段：

```sshconfig
Host jump
    HostName 192.168.1.1
    Port 2222
    User test
    ServerAliveInterval 30
    ServerAliveCountMax 3
```

以后开隧道：

```powershell
ssh -N -D 127.0.0.1:1080 jump
```

想把 `-D` 写进 config 也可以：

```sshconfig
Host jump
    HostName 192.168.1.1
    Port 2222
    User test
    DynamicForward 127.0.0.1:1080
    ExitOnForwardFailure yes
    ServerAliveInterval 30
```

然后：

```powershell
ssh -N jump
```

检查 config 是否被正确读入（不真正连接）：

```powershell
ssh -G jump | findstr /i "hostname port user"
```

应看到 `hostname 192.168.1.1`、`port 2222`、`user test`。

---

## 固定端口转发：`-L`（和 SOCKS 不冲突）

如果只想把内网 SSH 映到本机某个口，例如 `127.0.0.1:10022 → 192.168.10.10:22`，可以和 `-D` 写在一条命令里：

```powershell
ssh -N `
  -D 127.0.0.1:1080 `
  -L 127.0.0.1:10022:192.168.10.10:22 `
  -p 2222 test@192.168.1.1
```

之后：

```powershell
ssh -p 10022 <内网用户名>@127.0.0.1
```

`-L` 是固定目标；`-D` 是 SOCKS，浏览器访问哪个 IP 由代理客户端每次决定。两种可以一起用。

---

## 浏览器

系统或浏览器里设 **SOCKS5**，`127.0.0.1`，端口 **1080**（若你改了 `-D` 端口，这里一起改）。

全局 SOCKS 会把公网也绕进跳板，一般会用插件按域名/IP 分流，只让 `192.168.x` 走代理。这部分各浏览器界面不同，不展开。

---

## 内网机器还要 SSH 时

`config` 里可以写：先连本机 SOCKS，再 SSH 到目标。OpenSSH 本身不带 SOCKS 客户端，常见写法是用 **ncat**（Nmap 附带）：

```sshconfig
Host 192.168.*
    ProxyCommand ncat --proxy-type socks5 --proxy 127.0.0.1:1080 %h %p

Host 192.169.*
    ProxyCommand ncat --proxy-type socks5 --proxy 127.0.0.1:1080 %h %p
```

前提是 **`ssh -D` 已经开着**，且本机找得到 `ncat`。我这边当前 PATH 里 `where ncat` 找不到；若你机器上已有类似配置，ncat 可能在 Nmap 安装目录，需要写全路径或把目录加进 PATH。没有 ncat 就装 Nmap，或改用能走 SOCKS 的 `connect.exe` 等，思路一样：ProxyCommand 指向 `127.0.0.1:1080`。

---

## 登录方式：密码和公钥

现在 `test@192.168.1.1` 对我这台 PC 上的默认公钥 **不生效**：

```powershell
ssh -o BatchMode=yes -p 2222 test@192.168.1.1 exit
```

会得到 `Permission denied (publickey,...,password)`。开 `-D` 时照常输密码即可。

若要免密开隧道，在能登录跳板的前提下，把本机 `C:\Users\<你>\.ssh\id_rsa.pub` 写进跳板 `~/.ssh/authorized_keys`，然后在 config 里加上：

```sshconfig
Host jump
    IdentityFile ~/.ssh/id_rsa
    IdentitiesOnly yes
```

---

## 和仓库里「端口转发.exe」的关系

协议层面是一回事：都是 SSH 登录跳板，再让跳板去连内网目标。exe 多了图形界面、转发列表、页面上点主机密钥；纯 `ssh -D` 不依赖那个程序。会维护 `config`、习惯开终端的话，一条 `ssh -N -D` 就够 SOCKS 用了。

---

## 容易踩坑的几件事

**1080 已被占用**  
换端口，例如 `-D 127.0.0.1:1081`，浏览器和 curl 的端口一起改。

**在跳板上执行 `ssh -D`**  
SOCKS 开在跳板本机，PC 浏览器默认够不着；隧道必须从 PC 发起。

**隧道_idle 被掐**  
config 里 `ServerAliveInterval 30` 往往够用；仍断就查中间防火墙或 NAT 对长连接的限制。

**PowerShell 里 curl**  
实测请用 `curl.exe`；`--socks5-hostname` 需要解析域名时用这个参数，访问纯 IP 的 HTTPS 同样适用。

**密码不能管道塞进 ssh**  
OpenSSH 故意不吃 stdin 密码。自动化要么配公钥，要么像平常一样在 `ssh` 窗口里输入。

仓库里有一个辅助脚本 `docs/scripts/test-ssh-socks.ps1`：会弹出单独的 ssh 窗口让你输密码，检测到 1080 监听后再跑 curl。需要一键自检时可以用，不是必选项。

---

## 参考

- OpenBSD `ssh(1)` 手册：`-D`、`DynamicForward`、`-L`
- 同仓库 Go 实现（逻辑对照）：`cmd/portfwd/tunnel.go`
