# 子欣互传 · 产品与技术设计文档

> 一个属于我们自己的、UI 完全自研的跨设备文件/文本/桌面传输工具。
> 融合 LocalSend 的**局域网零服务器直连**与 file-transfer-go 的**跨网段 WebRTC P2P**。

---

## 0. 设计出发点

现有两个开源项目各解决一半问题:

| 项目 | 强项 | 短板 |
|---|---|---|
| **LocalSend** | 局域网零配置发现、极简、跨全平台原生客户端 | 只在同网段可用;无跨网能力;Flutter UI 无个性化空间 |
| **file-transfer-go** | WebRTC 跨网段 P2P、浏览器免安装、支持断点续传/目录同步 | 依赖服务端在线;UI 是现成 Next.js 模板;无局域网发现 |

我们自己的产品要一次拿到两边的优点:

```
┌─────────────────────────────────────────────────────────┐
│                    子欣互传 客户端                        │
│         (同一套自研 UI:桌面 exe + 手机 PWA)              │
├─────────────────────────────────────────────────────────┤
│  统一抽象:「设备」+「会话」+「传输」                      │
├──────────────────────┬──────────────────────────────────┤
│   通道 A: LAN 直连    │   通道 B: 跨网 P2P               │
│   mDNS 发现 + HTTP    │   WebSocket 信令 + WebRTC DC     │
│   同网段,零服务器      │   跨网段,需服务端(腾讯云)        │
├──────────────────────┴──────────────────────────────────┤
│   通道 C: WS 中转兜底(P2P 打洞失败时自动降级)            │
└─────────────────────────────────────────────────────────┘
                      │
              子欣互传 Server(Go 单二进制)
        信令 + 房间 + 静态站点 + embed
```

### 产品原则

1. **零注册**:不用账号,靠设备指纹 + 配对码建立信任
2. **同网不绕路**:局域网走直连,流量不经过任何服务器
3. **跨网也 P2P**:服务器只做信令,数据端到端加密
4. **永远有兜底**:P2P 失败自动降级中转,而不是失败
5. **UI 是我们的**:不沿用任何现成模板,自研设计语言

---

## 1. 命名与设计语言

**产品名:子欣互传** —— 取"数据流"之意。

### 视觉基调

| 项 | 取值 | 说明 |
|---|---|---|
| 主色 | `#6366F1` → `#8B5CF6` 渐变 | Indigo → Violet,冷静科技感 |
| 强调 | `#22D3EE` (Cyan) | 传输中状态、高亮进度条 |
| 成功 | `#34D399` | 传输完成 |
| 警告 | `#FBBF24` | 降级为中转 / 需确认 |
| 背景 | `#0B0E17` (深) / `#F7F8FC` (浅) | 自动跟随系统 |
| 卡片 | 16px 圆角,`backdrop-blur`,微妙 1px 高光描边 | 玻璃拟态 |
| 字体 | 系统 UI 字体栈 | 零加载成本 |
| 动效 | `cubic-bezier(0.22, 1, 0.36, 1)`,160~320ms | 轻快不拖沓 |

### 布局

```
┌──────────────────────────────────────────────────────────┐
│  ◈ 子欣互传          [ ● 状态胶囊 ]         ⚙  ⓘ        │
├──────────────┬───────────────────────────────────────────┤
│              │  ┌─ Tab: 发送 / 接收 / 文本 / 桌面 ────────┐ │
│  设备列表     │  │                                        │ │
│              │  │   ⬤ 拖拽区 / 文件网格 / 文本编辑框     │ │
│  ◉ 客厅台式机 │  │                                        │ │
│    LAN · 直连 │  │                                        │ │
│  ◉ 我的手机   │  └────────────────────────────────────────┘ │
│    Cloud·P2P  │                                           │
│  ◌ 未知设备   │  ┌─ 传输队列 ───────────────────────────┐ │
│              │  │ ▸ video.mp4   64%  12.4 MB/s  ⏸ ✕     │ │
│  [+ 添加设备] │  └────────────────────────────────────────┘ │
└──────────────┴───────────────────────────────────────────┘
```

- **左侧设备栏**:自动发现(LAN mDNS + 云端在线)的设备,带连接通道徽标(LAN/Cloud/Relay)
- **主区**:按模式切换的投放区
- **底部传输队列**:可折叠,显示进度/速度/剩余时间,支持暂停/取消/重试
- **移动端**:设备栏折叠为顶部横向滑动条,传输队列变底部抽屉

---

## 2. 传输通道设计

### 通道 A · 局域网直连(LAN)

**发现**:mDNS 广播服务 `_zixin._tcp.local`

```
DNS-SD 记录:
  实例名: Zixin-<短指纹>
  SRV     → <hostname>.local : PORT
  TXT     → v=1,fp=<fingerprint>,alias=<名称>,proto=flux/1
```

同网段设备互相发现,**不需要服务器**。发现后直连 HTTP 上传/下载。

**传输**:
```
POST /api/lan/upload          # multipart 或 raw stream
GET  /api/lan/download/:token # 断点续传: Range 头
```
- 分块 256KB
- 每块 CRC32 校验
- 整体 SHA-256 校验
- 断点续传:客户端记录已确认偏移,重连时用 `Range: bytes=<offset>-`

### 通道 B · 跨网段 WebRTC(P2P)

**信令**:WebSocket,和 file-transfer-go 同构但简化

```
WS /api/ws/signal?session=<id>&role=sender|receiver
```

| 方向 | type | payload |
|---|---|---|
| S→C | `peer-joined` | `{role}` |
| S→C | `error` | `{message}` |
| C↔C | `offer` / `answer` / `ice-candidate` | WebRTC 原生对象 |

服务端**不解析** SDP/ICE,只做转发 —— 保证服务器看不到内容元数据。

**数据通道**:WebRTC DataChannel,`ordered:false`,分块 256KB,自带 DTLS 加密。

**ICE 配置**:默认 5 个公共 STUN + 用户自定义 TURN(localStorage 持久化)。

### 通道 C · 中转兜底(Relay)

当通道 B 的 ICE 协商在 N 秒内未建立,自动降级到通道 C:

```
WS /api/ws/relay?session=<id>&role=...
  → relay-binary / relay-binary-ack  (带 offset,服务端只转发不落盘)
```

服务端**内存转发,不写磁盘**,TLS 保护。

### 通道选择策略

```
用户点"发送"
   │
   ├─ 目标设备在 LAN 可达?  ──是──► 通道 A(HTTP 直连)
   │                          否
   ▼
   └─ 有 session 且服务端在线? ──是──► 通道 B(WebRTC)
                                    │  8s 内 ICE 未连通
                                    ▼
                                 通道 C(WS 中转)
```

---

## 3. 统一领域模型

```ts
type Device = {
  fingerprint: string      // 32 hex,设备身份
  alias: string            // 显示名
  platform: 'windows'|'macos'|'linux'|'android'|'ios'|'web'
  channels: Channel[]      // 同时可达的通道
  online: boolean
  lastSeen: number
  paired: boolean          // 是否已确认信任
  address?: string         // LAN IP
  port?: number
}

type Channel = 'lan' | 'p2p' | 'relay'

type Session = {
  id: string               // uuid
  code: string             // 6 位取件码,字符集去 0/O
  createdAt: number
  expiresAt: number        // 默认 1h
  sender?: Device
  receiver?: Device
  status: 'waiting'|'signaling'|'connected'|'transferring'|'done'|'failed'
  activeChannel?: Channel
}

type Transfer = {
  id: string
  sessionId: string
  direction: 'send'|'receive'
  filename: string
  size: number
  mime: string
  sha256?: string
  offset: number           // 已确认字节
  speed: number            // B/s
  state: 'queued'|'active'|'paused'|'done'|'error'|'canceled'
  channel?: Channel
}
```

### REST API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/health` | 服务健康 + 版本 |
| POST | `/api/session` | 创建会话,返回 `{id, code, expiresAt}` |
| GET | `/api/session/:id` | 查询会话状态 |
| DELETE | `/api/session/:id` | 主动销毁 |
| POST | `/api/pair` | 提交配对请求(指纹 + 别名) |
| GET | `/api/pair/:id` | 轮询配对结果 |
| GET | `/api/devices` | 列出云端已知设备 |
| POST | `/api/lan/upload` | 通道 A 上传 |
| GET | `/api/lan/download/:token` | 通道 A 下载(支持 Range) |
| WS | `/api/ws/signal` | 通道 B 信令 |
| WS | `/api/ws/relay` | 通道 C 中转 |
| GET | `/api/manifest.webmanifest` | PWA manifest |

---

## 4. 服务端架构(Go)

```
cmd/zixin-transfer/main.go
internal/
  server/      HTTP 路由、静态资源、embed
  session/     会话与房间管理(内存 + RWMutex)
  signal/      WebSocket 信令中继
  relay/       WebSocket 中转
  lan/         局域网直传端点 + 临时文件槽
  mDNS/        服务宣告(_zixin-transfer._tcp)与设备发现
  pairing/     设备配对与信任存储
  auth/        轻量 token 中间件
```

**关键约束**:
- 单二进制:前端静态资源用 `go:embed` 打进 exe,启动即可用,无外部依赖
- 零数据库:房间/配对全部内存 + 可选 JSON 落盘
- 端口:`PORT` 环境变量,默认 8080

### Go 依赖

| 库 | 用途 |
|---|---|
| `github.com/gorilla/websocket` | WebSocket(信令 + 中转) |
| `github.com/charmbracelet/mdns` | mDNS 服务宣告与发现 |
| `github.com/go-chi/chi/v5` | 路由(可选,标准库也够) |

---

## 5. 前端架构(React + Vite + TypeScript)

```
src/
  app/            路由与全局 Provider
  components/     自研 UI 组件(Button/Card/Dialog/Tabs/Progress...)
  features/
    devices/      设备列表、发现、配对
    send/         文件拖拽、文本、桌面捕获
    receive/      接收队列
    transfers/    传输队列与进度
  lib/
    api.ts        REST 客户端
    signal.ts     WebSocket 信令客户端
    relay.ts      中转客户端
    lan.ts        局域网直连
    p2p.ts        WebRTC 封装
    crypto.ts     SHA-256 / CRC32 / 指纹
    pwa.ts        Service Worker 注册
  styles/         设计令牌 + 主题
```

### 打包分发

| 目标 | 产物 | 方式 |
|---|---|---|
| 桌面 | `zixin-transfer.exe`(约 12MB) | Go 内嵌前端,双击自动开浏览器 |
| 桌面(原生窗) | `子欣互传 Setup.exe` | Electron/Tauri 壳(第二阶段) |
| 手机 | PWA | 腾讯云域名 + "添加到主屏幕" |
| 服务端 | 同桌面 exe | 腾讯云轻量,systemd 常驻 |

---

## 6. 安全模型

| 面 | 措施 |
|---|---|
| 传输 | WebRTC DTLS / HTTPS / 通道 A 走 `http://<lan-ip>` 但内容 SHA-256 校验 |
| 设备身份 | 首次连接生成 32 字节随机指纹,存 localStorage + 服务端配对表 |
| 配对 | 6 位码 + 双向确认(对方需点"接受"),配对关系持久化 |
| 中转 | 服务端内存转发不落盘,中转包带 offset 不缓存整块 |
| 公网暴露 | 建议前置 Nginx + TLS + Basic Auth(服务本身无鉴权) |
| 房间码 | 6 位,34^6 ≈ 15.4 亿组合,60s 内不可枚举(限流) |

---

## 7. 里程碑

| 阶段 | 交付 | 状态 |
|---|---|---|
| M1 | Go 后端骨架:会话/信令/embed/静态站点 | 进行中 |
| M2 | React 自研 UI:设备栏 + 投放区 + 传输队列 | 进行中 |
| M3 | 通道 A(LAN 直连 + mDNS 发现) | 进行中 |
| M4 | 通道 B(WebRTC P2P) + 通道 C(中转兜底) | 进行中 |
| M5 | 打包 exe + PWA + 本机双端自测 | 计划 |
| M6 | 腾讯云部署 + 手机联调 | 计划 |

---

## 8. 许可与出处

本项目为全新实现,不复制任一上游的源码,但**协议与机制设计**参考了两个项目的公开设计:

- **LocalSend**(Apache-2.0):mDNS 发现模型、设备指纹配对、HTTP 上传下载
- **file-transfer-go**(MIT):WebSocket 信令中继、WebRTC DataChannel 分块协议、房间码模型

均为宽松许可,参考设计不构成衍生作品问题;若后续直接引用其代码,需按各自许可保留声明。

---

## 10. 聊天室（v1.0 新增）

### 数据模型

- **房间是节点级的**：每个子欣互传进程托管一间房（`ChatRoom`），浏览器与其它节点都连到它。
- 消息：`ChatMessage{ID, From(fingerprint), Alias, Body, Kind(text|file|system), Extra, Timestamp}`
- `Kind=file` 时 `Extra` 是 JSON：`{id, name, size, sha256, mime, url}`。
- 历史：内存保留 200 条，落盘 `chat.json`（同样 200 条），重启回放。

### 三条路径

```
浏览器 ──WS /api/chat/ws──▶ 本地节点 ChatRoom ──relay──▶ 其它节点（LAN）
   ▲                              │
   └──── 广播 chat/presence ◀──────┘        POST /api/chat/post（对方收到后不再转发）
```

1. **本机浏览器 → 本机房间**：WebSocket。连上先收 `history`，之后 `chat` / `presence` 实时推送。
2. **节点 → 节点**：`Server.relayChat`，目标来自**节点自己的设备表**（`registry`），
   不依赖浏览器是否扫完网；收到转发的一侧以 `relay=false` 处理，天然无环路。
   计数只认 `200 + application/json`（旧版本对未知 `/api/*` 会回 SPA 页面，不能被算作送达）。
3. **文件**：发送方 `POST /api/chat/upload` 把字节写进 `chatfiles/<id>-<name>`，
   只写一次；广播的公告里 `url` 由 `Server.publicFileURL` 改写成
   `http://<本机LAN地址>:<端口>/api/chat/file/<id>`，房间成员各自 GET 下载（支持 Range）。
   `chatfiles/` 启动时清理 24 小时前的文件。

### 为什么不做"推给每个人"

十台设备推十份 = 十次上传、十次占带宽。房间公告 + 各自下载 = **一次上传**，
且接收方可以选择"现在要"还是"不要"，不需要每台都弹接受框。
需要点对点直推（带接受确认）时走通道 A：`/api/lan/inquiry`。

### 未读与提示

前端 `ChatRoom` 常驻挂载（切到别的标签页只是 `display:none`），因此 WS 不断线；
非当前标签页收到他人消息时累加未读数，切回聊天页清零。