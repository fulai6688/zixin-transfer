<div align="center">

<img src="web/public/icon-192.png" width="96" alt="子欣互传" />

# 子欣互传 · Zixin Transfer

[![ci](https://github.com/fulai6688/zixin-transfer/actions/workflows/ci.yml/badge.svg)](https://github.com/fulai6688/zixin-transfer/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/fulai6688/zixin-transfer?label=release)](https://github.com/fulai6688/zixin-transfer/releases)
[![license](https://img.shields.io/github/license/fulai6688/zixin-transfer)](LICENSE)
[![go](https://img.shields.io/github/go-mod/go-version/fulai6688/zixin-transfer)](go.mod)
**局域网聊天 + 文件互传，一个 Go 二进制搞定。**

同一 WiFi 零服务器直连；跨网段用 6 位取件码打 WebRTC 直连；打洞失败自动降级中转。
桌面端、手机浏览器（PWA）、云端中转节点，全部由同一个程序扮演。

[功能](#功能) · [快速开始](#快速开始) · [架构](#架构) · [接口](#接口) · [维护](#维护约定) · [更新记录](CHANGELOG.md)

</div>

---

## 为什么是它

| 常见做法 | 子欣互传 |
|---|---|
| 走公网网盘/服务器中转，慢且怕隐私 | 同网段**直连**，字节不经过任何服务器 |
| 只支持同一局域网 | 跨网段用**取件码 + WebRTC**，服务器只转发信令 |
| 聊天归聊天、传文件归传文件 | **聊天室里直接丢文件**：一次上传，房间里谁要谁下 |
| 手机还得装 App | 浏览器打开即用，可「添加到主屏幕」当 App |

## 功能

**传输**
- 局域网直连：mDNS 发现 + TCP 扫描兜底 + HTTP 流式直传，边写边算 SHA-256
- 跨网段：6 位取件码 → WebRTC DataChannel；9 秒未打通自动降级服务端中转
- 多文件、大文件、重名自动改名不覆盖；接收目录可改（全局默认 + 单次临时）

**聊天室**
- 房间 WebSocket：连上即回放历史，加入/离开提示，历史持久化
- **文件一次上传、多台下载**：字节留在发送方节点，房间成员点一下即取
- 服务端 relay：文本与文件公告自动同步到局域网内其它节点，无环路
- 未读角标、拖拽上传、连接状态、同步开关

**设备**
- 已知设备表持久化：离线设备灰显保留，重启不丢
- 设备备注名（优先于对方自称）
- 一键放行 Windows 防火墙（UAC 提权）

**可维护性**（给自己的长期项目准备）
- `docs/MEMORY.md` 项目记忆：架构约束、已修缺陷档案、验证清单、路线图
- 版本号/提交/构建时间注入二进制，`/api/health` 与 `-version` 都可查
- CI 检查（gofmt/vet/构建/冒烟）+ tag 触发全平台自动发版
- 前端产物提交进仓库，`go build` 开箱即用，无需 Node 环境

## 快速开始

### Windows

```powershell
# 1. 构建（需要 Go 1.22+）
powershell -File scripts/build.ps1

# 2. 运行（会自动开浏览器）
.\bin\zixin-transfer.exe

# 3. 首次在公网/公司网络用，管理员运行一次放行防火墙
#    双击「放行防火墙(管理员).cmd」，或：
.\bin\zixin-transfer.exe -install-firewall
```

也可以直接双击 `启动 子欣互传.cmd`。

### 手机 / 其它设备

浏览器打开 `http://<这台电脑的IP>:8080`，或打开云端地址，**添加到主屏幕**即得 App 体验。

### 云端中转（仅跨网段需要）

一台 1C1G 的轻量服务器足够。见 [deploy/README-deploy.md](deploy/README-deploy.md)：

```bash
sudo bash install-relay.sh send.example.com /root/zixin-transfer-linux-amd64
```

之后客户端启动时带上中转地址：

```powershell
.\bin\zixin-transfer.exe -relay https://send.example.com    # 或设 ZX_RELAY 环境变量
```

## 命令行

```
-port 8080              监听端口
-alias <名字>           设备显示名（默认主机名）
-downloads <目录>       接收文件目录（默认 ~/Downloads/ZixinTransfer）
-data <目录>            节点状态目录（默认 %LOCALAPPDATA%\ZixinTransfer）
-relay <URL>            跨网段信令服务器
-no-announce            不对外宣告（纯中转节点用）
-open=false             启动时不打开浏览器
-install-firewall       添加 Windows 防火墙规则后退出
-uninstall-firewall     移除防火墙规则后退出
-version                打印版本/提交/构建时间
```

环境变量：`ZX_ALIAS` `ZX_DOWNLOADS` `ZX_DATA` `ZX_RELAY` `ZX_NO_ANNOUNCE` `ZX_OPEN`。

## 架构

```
┌──────────────────────── 子欣互传（同一套自研 UI）────────────────────────┐
│  桌面 exe（Windows）      手机浏览器 PWA        其它电脑                    │
├──────────────────────────────────────────────────────────────────────────┤
│  通道 A · LAN 直连         通道 B · 跨网 P2P            通道 C · 中转兜底     │
│  mDNS/扫描 + HTTP 直传      WS 信令 + WebRTC DC        同一 WS，服务端透传   │
├──────────────────────────────────────────────────────────────────────────┤
│  聊天室：房间 WS + 服务端 relay + 文件一次上传多台下载                       │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                    子欣互传 Server（同一个二进制，跑在服务器上）
                    信令 / 房间 / 静态站点 / 前端 embed
```

细节见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)，
维护必读 [docs/MEMORY.md](docs/MEMORY.md)（架构约束 + 已修缺陷 + 验证清单）。

## 接口

| 方法 | 路径 | 作用 |
|---|---|---|
| GET | `/api/health` | 身份、版本、commit、是否宣告、防火墙、中转地址、接收目录 |
| GET | `/api/lan/devices` · `/api/lan/scan` | mDNS 表 / 子网扫描（单飞 + 缓存） |
| POST | `/api/lan/inquiry` · `/api/lan/accept` · `/api/lan/upload?token=` | 局域网直传三步 |
| GET | `/api/lan/pending` · `/api/lan/received` | 待决策 / 已接收（持久化） |
| POST | `/api/session` · GET `/api/session/code/{code}` | 取件码会话 |
| GET | `/api/ws/session?session=&role=` | 信令 WebSocket |
| GET | `/api/chat/ws` · `/api/chat/history` | 聊天室房间 / 历史 |
| POST | `/api/chat/post` · `/api/chat/upload` · GET `/api/chat/file/{id}` | 发消息 / 分享文件 / 取文件 |
| GET/POST | `/api/settings` | 默认接收目录 |
| POST | `/api/system/pick-folder` · `/api/system/reveal` · `/api/system/open` | 本机专属（仅 loopback + 路径白名单） |
| POST | `/api/devices/rename` · GET `/api/devices` | 设备备注 / 已知设备 |
| GET/POST | `/api/firewall/status` · `/api/firewall/install` | 防火墙 |

未知 `/api/*` 一律 **404 JSON**（不会落到 SPA）。

## 项目结构

```
cmd/zixin-transfer/     入口（版本注入、flag/env）
internal/app/           server / session / lan / scan / mdns / chat / hub / settings / localops / firewall
web/                    React + Vite + TS 源码（无 UI 框架）
webui/                  go:embed 的前端产物（已提交，保证 go build 可用）
deploy/                 systemd / nginx / 一键部署
scripts/                make_icons.py · build.ps1 · build.sh · release.ps1
docs/                   MEMORY（记忆）/ ARCHITECTURE / ACCEPTANCE / API
.github/workflows/      ci（检查+冒烟）· release（tag 自动出包）
```

## 维护约定

- 版本：`VERSION` 文件 + git tag `vX.Y.Z` + `CHANGELOG.md`，发版用 `scripts/release.ps1`
- 提交：Conventional Commits（`feat:` / `fix:` / `docs:` / `refactor:` / `chore:`）
- **改代码前先读 `docs/MEMORY.md` 第 3 节「不可破坏的约束」，改完必须回去更新该文件**
- 破坏兼容（API 字段、数据文件格式）→ 主版本 +1，并在 CHANGELOG 写迁移说明

## 已验证

见 [docs/ACCEPTANCE.md](docs/ACCEPTANCE.md)，含实测数据：3 MiB LAN 直传 SHA-256 三处一致、
双浏览器 UI 全链路、跨网段信令协议、聊天室文件一次上传多台下载、未读角标等。

## 许可证

[MIT](LICENSE)
