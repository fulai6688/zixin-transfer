# Changelog

本项目遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)，
变更记录格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [Unreleased]

### 计划中
- 聊天消息声音提醒
- 聊天内文件预览（PDF / 图片 / 文本）
- 跨网段聊天室（服务器房间）
- 桌面共享（WebRTC 视频轨）
- LAN 断点续传

## [1.0.0] - 2026-11-11

首个正式版本：由内部代号 FluxSend 更名为 **子欣互传 / Zixin Transfer**，并整理为可长期维护的开源项目。

### 新增
- **局域网聊天室**：房间 WebSocket，连上即回放历史；加入/离开提示；历史持久化（`chat.json`）。
- **文件一次上传、多台下载**：发送方只上传一次到自己的节点，房间内所有设备点一下即下载（`chatfiles/`，24h 自动清理）。
- **服务端 relay**：聊天消息与文件公告由节点转发给局域网内其它子欣互传节点，收到转发的节点不再转发（无环路）。
- **未读角标**：不在聊天标签页时统计新消息。
- **默认接收目录设置**：系统目录选择框 / 手动输入，校验可写后持久化（`settings.json`），顶栏同步显示。
- **单次接收换目录**：接收弹窗内可临时指定本次落盘目录。
- **已接收面板**：显示文件名/大小/来源/路径/SHA-256 前 8 位，并支持「打开文件」「在文件夹中显示」（仅本机）。
- **设备备注**：给设备起名，持久化，优先于对方自称。
- **已知设备表**：离线设备灰显保留，重启不丢；每 25 秒自动补扫。
- **构建元数据**：`/api/health` 返回 `version/commit/builtAt`，`-version` 输出同样信息。
- 品牌图标：PNG 全套 + maskable + favicon + Windows `.ico` + 社交预览图。

### 修复
- 中继把文本帧按二进制下发，导致 SDP offer 被浏览器丢弃、P2P 永远连不上。
- 对方未进房时发送的信令帧被丢弃，握手永久卡死（改为会话 backlog + `peer-joined` 补发）。
- mDNS 轮询整表覆盖 peer 列表，扫描结果立即消失（改为按指纹合并 + 90s 缓存）。
- 扫描请求的 `timeout` 被当作扫描预算，并发扫描被掐断且空结果进缓存。
- 设备指纹每次启动随机生成，导致备注与已知设备漂移（改为 `device.json` 持久化）。
- 已接收索引只在内存，重启后清空（改为 `received.json`）。
- 未知 `/api/*` 落到 SPA 兜底返回 200 + index.html，旧版本节点被误判为"送达成功"。
- 客户端 fan-out 存在时序竞态导致文件公告漏发（改为服务端 relay）。
- 文件下载地址取自页面来源，用 localhost 打开时对方会打到自己（改为本机 LAN 地址）。
- 打开文件/目录/选目录接口缺少来源与路径校验（改为仅 loopback + 路径白名单）。
- 子网扫描端口写死 8080/8081，8082–8085 上的实例发现不到。
- 相对路径直接显示在设置面板（启动时统一转绝对路径）。

### 变更
- 产品更名：FluxSend → 子欣互传（二进制 `zixin-transfer.exe`，模块 `github.com/fulai6688/zixin-transfer`）。
- mDNS 服务类型 `_fluxsend._tcp.local` → `_zixin._tcp.local`（**旧版本与新版本不能互相发现，需同时升级**）。
- 数据目录 `FluxSend` → `ZixinTransfer`（Windows: `%LOCALAPPDATA%\ZixinTransfer`）。旧版本数据可手动复制过来。
- 环境变量 `FLUX_*` → `ZX_*`。
- 浏览器本地存储键 `flux.*` → `zx.*`（主题、手动设备列表会重置一次）。

### 迁移说明
1. 关闭旧版本进程。
2. 把旧数据目录（`%LOCALAPPDATA%\FluxSend`）内容复制到 `%LOCALAPPDATA%\ZixinTransfer`，
   即可保留接收目录设置、设备备注、聊天历史。
3. 局域网内所有设备需升级到 1.0.0 才能互相发现（mDNS 服务名变了）。
