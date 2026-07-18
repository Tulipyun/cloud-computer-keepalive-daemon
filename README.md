# Cloud Computer Keepalive Daemon

面向中国移动云电脑子账号场景的纯 Go 长期保活客户端。当前 `v0.2.0` 已实现 SOHO 登录、ZTE VMC/CAG、CAG mux 和 SPICE 会话，不依赖官方 Windows 客户端或原生 SDK，可交叉编译为 Windows x64、Windows ARM64 和 Linux x64。

本仓库为私有维护基线。运行配置、账号、令牌、原始抓包和诊断日志不得提交或发布。

## 版本与分支

- `main` / `v0.2.0`：普通长期运行版，作为后续开发默认起点。
- `codex/v0.2-longtest-diagnostics` / `v0.2.0-longtest-1`：完整日志版源码，用于协议变化和长时间故障取证。
- `v0.1.0`：最初可用的冻结回退版本。

两个 `v0.2` 变体使用相同的登录、ZTE、CAG、SPICE 和重试实现。日志版额外记录轮转日志、结构化事件、数据包摘要和 incident 快照。

## 当前能力

- 子账号密码登录，不依赖主账号手机号。
- 自动查询云电脑并取得 `getFirmAuth` 返回的 ZTE 参数。
- 调用 ZTE VMC `sysConfig/getToken/getDesktopList/startDesktop`。
- 优先建立 CAG TCP/TLS，失败时回退到 UDP/KCP。
- 建立 CAG mux、SPICE 主通道和 7 个子通道。
- 处理 PING/PONG、SET_ACK/ACK_SYNC/ACK 和 ZTE `0x74 -> 0x79`。
- 使用 MARK、SURFACE_CREATE 或 DRAW_COPY 判断显示会话就绪。
- 同时维持 SOHO heartbeat 和 SPICE 协议活动。
- 按认证、维护、网络、协议和本地配置分类执行重试。
- 将子账号密码以机器绑定的 AES-GCM 密文保存到当前目录 `config.json`。

## 快速开始

1. 从私有 Release 下载对应平台的普通版二进制。
2. 将程序放入固定目录并从该目录启动。
3. 首次运行输入子账号和密码；存在多台云电脑时选择目标设备。
4. 程序生成 `config.json` 后立即进入长期保活。
5. 后续在同一机器、同一系统用户和同一工作目录启动时自动读取配置。

程序不需要子命令或运行参数。按 `Ctrl+C` 正常停止。

## 安全说明

`config.json` 中的 `sub_password_box` 使用 scrypt 派生本机密钥并通过 AES-GCM 加密。密钥材料包含操作系统、主机名、当前用户名和随机 salt，因此配置通常不能直接迁移到其他机器或系统用户。

这种机制用于避免密码明文落盘，但不等同于 DPAPI、Credential Manager、Secret Service 或硬件密钥库。能够以同一系统用户执行任意代码的本地攻击者仍可能取得凭据。

以下内容不得上传：

- `config.json`、`cloud_pc.json`
- 账号、密码、token、VM 凭据和私有端点
- `logs/`、原始抓包、incident 和本地探针输出

## 技术文档

- [PROJECT_STATE.md](PROJECT_STATE.md)：当前项目状态和新对话交接入口。
- [docs/PROTOCOL_IMPLEMENTATION.md](docs/PROTOCOL_IMPLEMENTATION.md)：完整技术路径、关键参数和实现方法。
- [docs/SOAK_TEST_20260714.md](docs/SOAK_TEST_20260714.md)：96 小时长时间测试结果。
- [docs/BUILD_AND_RELEASE.md](docs/BUILD_AND_RELEASE.md)：普通版、日志版构建和发布流程。
- [V0.2_HARDENING.md](V0.2_HARDENING.md)：协议硬化内容。
- [LONGTEST_GUIDE.md](LONGTEST_GUIDE.md)：完整日志版使用和日志收集方法。

## 从源码构建

需要 Go 1.24 或兼容版本。三个目标均使用 `CGO_ENABLED=0`。

```powershell
go test ./...
.\scripts\build-release.ps1 -Variant standard -Version v0.2.0
```

日志版需要先检出对应诊断分支：

```powershell
git switch codex/v0.2-longtest-diagnostics
go test ./...
.\scripts\build-release.ps1 -Variant longtest -Version v0.2.0
```

产物和 SHA256 清单位于 `dist/release-v0.2.0-<variant>/`。

## 重要结论

- 子账号 `getFirmAuth` 返回 ZTE VMC/CAG 参数，而不是原始 SCG 路径使用的 `scAuthCode`。
- ZTE 长连接不能只发送 SOHO heartbeat，还必须维持 CAG/SPICE 会话和各通道自动应答。
- `0x74` 必须回复 `0x79`，消息体为单字节 `0x00`。
- `0x75` 与断开流程有关，不能用作 `0x74` 的回复。
- 不应定期重发 DISPLAY_INIT/INPUT_INIT，也未启用未经抓包确认的合成 21 Hz display 流量。
