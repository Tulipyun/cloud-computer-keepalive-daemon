# Cloud Computer Keepalive Daemon

这是一个面向中国移动云电脑子账号场景的长期保活客户端。当前版本以纯 Go 实现已验证的 SOHO、ZTE CAG/VMC 和 SPICE 连接流程，不依赖官方 Windows 客户端或原生 SDK，可编译为 Windows x64、Windows ARM64 和 Linux x64 程序。

本仓库是后续协议维护和无人值守运行的私有起点。协议状态、关键结论和升级入口另见 [LOCAL_DAEMON_STATUS.md](LOCAL_DAEMON_STATUS.md)。

当前开发中的协议硬化版本见 [V0.2_HARDENING.md](V0.2_HARDENING.md)。已发布的 `v0.1.0` 保持冻结，可随时作为回退基线。

用于无人值守长测的临时诊断版本说明见 [LONGTEST_GUIDE.md](LONGTEST_GUIDE.md)。

## 当前能力

- 子账号密码登录，不依赖主账号手机号。
- 自动查询云电脑并获取 ZTE firm auth 参数。
- 通过 ZTE VMC 启动桌面。
- 优先建立 CAG TCP/TLS 连接，失败时回退到 UDP/KCP。
- 完成 CAG mux、SPICE 主通道和子通道认证。
- 同时维持 SOHO heartbeat 与 SPICE 协议级保活。
- 连接意外中断后自动刷新登录并指数退避重试。
- 首次配置后无需命令行参数，启动程序即进入长期保活。

## 快速开始

1. 从私有仓库的 Releases 下载对应平台二进制。
2. 将程序放入一个固定目录，并从该目录启动。
3. 首次运行时输入子账号和密码；如果账号下有多台云电脑，再选择目标云电脑。
4. 程序会在当前工作目录生成 `config.json`，随后直接进入长期保活。
5. 后续从同一目录、同一系统用户启动时，会读取配置并自动连接。

程序不再提供 `login`、`keepalive`、`--duration` 或 `--forever` 等命令行模式。按 `Ctrl+C` 可正常停止。

## 配置与密码

运行配置保存在当前工作目录的 `config.json`，该文件已被 Git 忽略，不应提交。

密码不会以明文写入配置。程序使用 scrypt 派生本机绑定密钥，并通过 AES-GCM 保存为 `sub_password_box`。密钥材料包含操作系统、主机名、当前用户名和随机 salt，因此配置通常只能由同一机器上的同一系统用户自动解密。

这一机制的目标是避免密码明文落盘，不等同于 Windows Credential Manager、DPAPI、Linux Secret Service 或硬件密钥库。能够以同一系统用户执行任意代码的本地攻击者仍可能取得凭据。复制配置到其他机器或切换系统用户后，程序可能要求重新输入密码。

`config.json`、旧版 `cloud_pc.json`、抓包、token、账号资料和调试环境不得上传到仓库或 Release。

## 自动恢复策略

连接异常结束后，daemon 会尝试刷新子账号登录状态，然后重新执行完整连接流程。重试从 5 秒开始指数增长，最大为 5 分钟。如果上一次连接已稳定运行超过 10 分钟，退避会重置为 5 秒。

正常的持续链路包括：

```text
SOHO login/list/getFirmAuth
  -> ZTE VMC getToken/getDesktopList/startDesktop
  -> ZTE CAG TCP/TLS (or UDP/KCP fallback)
  -> CAG mux proxy links
  -> SPICE main/subchannel authentication
  -> SOHO heartbeat + SPICE auto-replies
```

## 已验证的协议要点

- 子账号登录路径是 SOHO home sub-account password login。
- 子账号 `getFirmAuth` 返回 ZTE VMC/CAG 参数，而不是原始 SCG 流程使用的 `scAuthCode`。
- 长连接期间，服务端 SPICE 消息 `0x74` 需要客户端回复 `0x79`，消息体为 1 字节 `0x00`。
- `0x75` 与断开流程相关，不应作为 `0x74` 的保活回复。

## 从源码构建

需要 Go 1.24 或兼容版本。

```powershell
go test ./...

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist\cck-daemon-windows-amd64.exe .

$env:GOARCH = "arm64"
go build -trimpath -ldflags="-s -w" -o dist\cck-daemon-windows-arm64.exe .

$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w" -o dist\cck-daemon-linux-amd64 .
```

三个目标均使用 `CGO_ENABLED=0`，不需要随程序分发 C 运行库或官方 SDK。

## 目录结构

- `main.go`：长期运行入口。
- `cmd/daemon.go`：首次配置、登录刷新和重试调度。
- `cmd/keepalive.go`：SCG/ZTE 保活流程。
- `internal/soho/`：SOHO 登录和 API 请求。
- `internal/zte/`：ZTE VMC、CAG、mux 和认证实现。
- `internal/spice/`：SPICE 握手与协议消息处理。
- `internal/config/`：运行配置和本机绑定密码加密。
- `LOCAL_DAEMON_STATUS.md`：当前实现状态、构建校验值和未来升级提示。

## 调试与升级

协议变化时，优先检查 `cmd/keepalive.go`、`internal/zte/` 和 `internal/spice/raw.go`。本地工作区可保留被 Git 忽略的 `native_probe/` 抓包与探测工具，但其中可能包含敏感连接资料，不能上传。

每次正式更新建议：

1. 运行 `go test ./...`。
2. 至少进行一次 150 秒以上的实际子账号连接测试。
3. 重新构建三个目标平台。
4. 记录 SHA256。
5. 通过新的 Git tag 和私有 Release 发布二进制。
