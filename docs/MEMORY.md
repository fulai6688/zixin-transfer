# 项目记忆 · PROJECT MEMORY

> 这个文件是本项目的**长期记忆**。任何人（包括未来的我）接手时，先读这里，
> 再读 `docs/ARCHITECTURE.md`，就能在 5 分钟内恢复全部上下文。
> **每次发版或做出重要决定后，必须回来更新本文件。**

最后更新：2026-11-11 · 版本 v1.0.0

---

## 1. 这是什么

**子欣互传 / Zixin Transfer**：一个 Go 单二进制的跨设备互传工具。

- 同一 WiFi：零服务器直连（mDNS 发现 + TCP 扫描兜底 + HTTP 直传）
- 跨网段：6 位取件码 + WebRTC P2P（服务器只转发信令），失败降级中转
- 局域网聊天室：群聊 + **文件一次上传、多台设备各自下载**

一个进程同时是：桌面端（本地服务 + 自研 Web UI）、手机 PWA、云端中转。

## 2. 三条通道（不要混）

| 通道 | 代码 | 需要服务器 | 说明 |
|---|---|---|---|
| A · LAN 直连 | `internal/app/lan.go`（inquiry→accept→upload） | 否 | 接收端持有 token，发送端流式 POST |
| B · 跨网 P2P | `web/src/lib/p2p.ts` + `internal/app/session.go`（WS 信令） | 是（`-relay`） | WebRTC DataChannel，失败 9s 后降级 |
| C · 中转 | 同一 WS，二进制帧透传 | 是 | 服务端不解包，只转发 |
| D · 聊天室 | `internal/app/chat.go` | 否（LAN） | WS 房间 + 服务端 relay 到其它节点 |

## 3. 不可破坏的约束（改代码前必读）

1. **中继必须保留帧类型**：文本帧（SDP/ICE）与二进制帧（数据）不能混。曾经因为统一按
   `BinaryMessage` 下发，导致浏览器把 offer 当二进制丢掉、P2P 永远连不上（见 §6 #1）。
2. **早到帧要缓存**：对方未进房时收到的信令帧进 `Session.backlog`，进房回放；
   客户端在 `peer-joined` 时补发 offer（§6 #2）。
3. **未知 `/api/*` 必须 404**：不能用 SPA 兜底返回 index.html，否则旧版本节点会被
   误判为"接口存在/送达成功"（§6 #7）。
4. **文件下载地址用 LAN 地址**（`Server.publicFileURL`），不能用页面来源 —— 用
   `localhost` 打开界面时，别的设备点下载会打到它自己（§6 #9）。
5. **设备指纹持久化**（`device.json`）：每次启动随机生成会让备注与"已知设备"全部漂移（§6 #5）。
6. **接收目录、设备表、聊天历史、已接收索引都要持久化**（`settings.json` /
   `trusted.json` / `chat.json` / `received.json`），重启不丢是产品承诺。
7. **本机专属操作只认 loopback**：`reveal`/`open`/`pick-folder` 三个接口必须
   `isLoopback` 校验 + 路径白名单，否则任意网页都能开你机器上的文件（§6 #10）。

## 4. 目录地图

```
cmd/zixin-transfer/        入口：flag/env、版本注入、启动
internal/app/
  server.go                路由、SPA 兜底、/api/ 404、WS 信令入口、生命周期
  session.go               会话 + 取件码 + 双角色 + backlog + 帧类型
  lan.go                   局域网直传（token 上传、SHA-256、唯一文件名）
  scan.go                  子网扫描（单飞、5s 缓存、仅完整扫描才缓存）
  mdns.go                  自实现 mDNS 宣告/浏览（_zixin._tcp.local）
  chat.go                  聊天室：房间、历史、文件暂存、服务端 relay
  hub.go                   设备注册表 + 备注名 + 持久化
  settings.go              默认接收目录
  settings_api.go          设置/选目录/打开文件/改名 的 HTTP 层
  localops.go              宿主操作（explorer / open / FolderBrowserDialog）
  firewall.go              Windows 防火墙规则（UAC 提权）
  fileutil.go              原子写文件
web/src/                   React + Vite + TS（无 UI 框架，手写设计系统）
webui/                     go:embed 打包前端产物（dist 提交进仓库，保证 go build 开箱可用）
deploy/                    systemd / nginx / 一键部署脚本
scripts/                   make_icons.py / build.ps1 / build.sh / release.ps1
docs/                      ARCHITECTURE / MEMORY（本文件）/ ACCEPTANCE / API
```

## 5. 常用命令

```powershell
# 前端（改了 web/ 之后必须执行，再重新 go build 才会生效）
cd web; npm install; npm run build     # 产物输出到 ../webui/dist

# 后端（本地开发，带版本注入）
powershell -File scripts/build.ps1 -Version 1.0.0

# 仅编译
go build -o bin/zixin-transfer.exe ./cmd/zixin-transfer

# 跨平台产物（云端中转用）
$env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
go build -ldflags="-s -w" -o dist/zixin-transfer-linux-amd64 ./cmd/zixin-transfer

# 双节点自测（端口 8080/8081，数据落 run/）
.\bin\zixin-transfer.exe -port 8080 -data .\run\a -downloads .\run\a\Downloads -alias AlphaPC -open=false
.\bin\zixin-transfer.exe -port 8081 -data .\run\b -downloads .\run\b\Downloads -alias BetaPC -open=false
```

## 6. 已修缺陷档案（防止复现）

| # | 缺陷 | 后果 | 修法 | 位置 |
|---|---|---|---|---|
| 1 | 中继一律 `BinaryMessage` | 浏览器丢弃 offer，P2P 连不上 | `wsFrame{kind,data}` 保留帧类型 | session.go |
| 2 | 对方未进房时的帧被丢弃 | 握手永久卡死 | `Session.backlog` + `peer-joined` 补发 | session.go / p2p.ts |
| 3 | mDNS 轮询整表覆盖 peers | 扫描结果秒消失 | 按指纹合并 + 90s 缓存 | App.tsx |
| 4 | `timeout` 参数被当扫描预算 | 并发扫描被掐断且缓存空结果 | 独立 20s 预算 + 单飞 + 仅完整才缓存 | scan.go |
| 5 | 设备指纹每次启动随机 | 备注/已知设备漂移 | `device.json` 持久化身份 | server.go |
| 6 | 已接收索引只在内存 | 重启后列表清空 | `received.json` | lan.go |
| 7 | 未知 `/api/*` 落到 SPA | 旧节点被算作"送达" | `/api/` 统一 404 + relay 校验 content-type | server.go / chat.go |
| 8 | 客户端 fan-out 有时序竞态 | 文件公告漏发 | 改服务端 relay（用节点设备表） | chat.go |
| 9 | 文件地址用页面来源 | localhost 打开时对方下到自己 | `publicFileURL` 用 LAN IP | chat.go |
| 10 | 打开文件/目录无来源校验 | 任意网页可开本机文件 | `requireLocal` + 路径白名单 | localops.go / settings_api.go |
| 11 | 扫描端口写死 8080/8081 | 8092/8084 的实例发现不到 | 扫 8080–8085 + 已知端口 | scan.go |
| 12 | 相对路径进界面 | 设置面板显示 `.\run\a\Downloads` | 启动时 `filepath.Abs` | server.go |

## 7. 验证清单（每次发版跑一遍）

```powershell
# 1) 静态检查
gofmt -l internal cmd     # 必须无输出
go vet ./...

# 2) 编译
go build ./...

# 3) 双节点：LAN 直传（SHA-256 三处比对）
#    见 docs/ACCEPTANCE.md §3 的脚本

# 4) 信令协议（会话/取件码/双向帧）
#    见 docs/ACCEPTANCE.md §4

# 5) 双浏览器 UI 全链路（A 发 → B 界面接受 → 落盘）
#    见 docs/ACCEPTANCE.md §5

# 6) 聊天室（文本、文件一次上传多台下载、未读）
#    见 docs/ACCEPTANCE.md §13-15
```

## 8. 版本与提交规则

- 版本写 `VERSION`，发版同步：`CHANGELOG.md` → git tag `vX.Y.Z` → GitHub Release
- 提交信息用 Conventional Commits：`feat:` / `fix:` / `docs:` / `refactor:` / `chore:`
- 破坏兼容（改 API 字段、改数据文件格式）→ 主版本 +1，并在 CHANGELOG 写迁移说明
- 每次发版必须更新本文件顶部的"最后更新"与 CHANGELOG

## 9. 下一步（Roadmap）

- [ ] 聊天消息声音提醒（参考：爱传内置 `sound/*.wav` 提示音）
- [ ] 聊天内文件预览（PDF/图片/文本）
- [ ] 群发到"多选设备"（当前群发走聊天室，多选直推未做）
- [ ] 跨网段聊天室（当前 relay 只走 LAN；需要服务器房间）
- [ ] 桌面共享（WebRTC 视频轨，信令已就绪）
- [ ] LAN 断点续传（分片 + offset）

## 10. 外部参考（仅做对照，不拷贝代码）

- `cloudups/iTransfer`（2016，GitHub）：纯文件三通道，无聊天室
- 爱传 · 局域网聊天室 v2（Python + PyInstaller 分发）：聊天室 + 声音提醒 + SumatraPDF 预览
- `Myfastools/anytransfer`（WebRTC 网页互传）：跨网 P2P 思路

---

## 11. 仓库与发布

- 仓库：<https://github.com/fulai6688/zixin-transfer>
- 发布页：<https://github.com/fulai6688/zixin-transfer/releases>
- 当前版本：**v1.0.0**（首个正式版，含 5 平台产物 + SHA256SUMS）

### 发布流程（照做即可）

```powershell
# 1. 改代码 → 更新 CHANGELOG.md（写清 Added/Fixed/Changed）→ 更新本文件顶部日期
# 2. 一条命令发版（自动：构建前端→编译→提交→打 tag→推送→创建 Release）
powershell -ExecutionPolicy Bypass -File scripts/release.ps1 -Version 1.0.1 -Notes "修复 xxx"
# 3. 没有 gh CLI 时，tag 推送后由 .github/workflows/release.yml 出包
```

### CI / Release 事实

- `ci.yml`：push/PR 触发，跑 gofmt + vet + `go test ./...` + 编译 + 冒烟（health/未知 API 404）
- `release.yml`：`push: tags: v*` 或手动 dispatch；编译 windows/linux/darwin × amd64/arm64，
  生成 `SHA256SUMS.txt`，`softprops/action-gh-release` 建 Release（dispatch 时是 draft，需发布）
- **坑**：新建仓库后第一次如果 `git push -u origin main --tags` 一次性推分支+标签，
  tag 的 workflow 可能不触发（实测）。以后单独 `git push origin vX.Y.Z` 即可；
  或手动 dispatch：`POST /repos/{owner}/{repo}/actions/workflows/release.yml/dispatches`

### 版本号规则

- `VERSION` 文件是唯一真源；`scripts/build.ps1` 读它并 `-X main.version=` 注入
- 二进制能自证身份：`zixin-transfer.exe -version` → `子欣互传 1.0.0 (windows/amd64) commit=… built=…`
- `/api/health` 同时返回 `version` / `commit` / `builtAt`，前端设置面板可展示