# 子欣互传 验收报告

生成时间：2026-11-10 23:15:54
环境：Windows · WLAN 10.0.0.31 · Go go1.27.3 · Node v24.18.0 · Chrome headless

## 1. 节点
- `:8080`  alias=AlphaPC  version=0.1.0  announce=True  relay=(未配置)
- `:8081`  alias=BetaPC  version=0.1.0  announce=True  relay=(未配置)
- `:8090`  alias=CloudRelay  version=0.1.0  announce=False  relay=http://127.0.0.1:8090

## 2. 局域网发现（mDNS 被防火墙挡住时的 TCP 兜底扫描）
- 冷扫描：1102 ms，发现 1 台
- 缓存命中：1 ms（单飞合并，仅完整扫描才缓存）
- 发现：BetaPC  10.0.0.31:8081  source=scan

## 3. LAN 直传 3 MiB（三处 SHA-256 比对）
- 传输耗时：1279 ms
- 源文件 SHA-256    ：4d9ff4a096195d9fa9c9baa92676daf7fd65893fd6bc225e10e6ebca3da0f977
- 接收端回显 SHA-256：4d9ff4a096195d9fa9c9baa92676daf7fd65893fd6bc225e10e6ebca3da0f977
- 落盘文件 SHA-256  ：4d9ff4a096195d9fa9c9baa92676daf7fd65893fd6bc225e10e6ebca3da0f977
- 判定：**PASS**

## 4. 跨网段信令（会话 / 6 位取件码 / 双向帧）
```
session id=8c8f23a4e5960532fb8e1d4ab1c9ad24 code=XKVQBA
code lookup ok: id=8c8f23a4e5960532fb8e1d4ab1c9ad24 senderOnline=false
signal relay ok: type=offer from=sender to=receiver payload={"sdp":"v=0 fake"}
payload relay ok: offset=4096 bytes="FLUX-RELAY-BYTES"
reverse relay ok: type=answer from=receiver
ALL SIGNALING TESTS PASSED
```
- 判定：**PASS**

## 5. 双浏览器 UI 全链路（真实 Chrome，无打桩）
```
A mounted: true  B mounted: true
A peers: ["BetaPC","取件码 / P2P","强制中转"]
A selected BetaPC: true
A dropped file: ok
A queue shows: ui-e2e.bin
B dialog: 收到一个传输请求 AlphaPC 想发送一个文件 A ui-e2e.bin 512 KB 拒绝 接受
B accepted via UI: true
B shows a completed transfer: true
B /api/lan/received: [省略]
A queue text: 传输队列 1 条记录 ui-e2e.bin 发送 · 512 KB / 512 KB · BetaPC lan
A page errors: none
B page errors: none
newest received: {"name":"ui-e2e.bin","path":"run\\b\\Downloads\\ui-e2e.bin","size":524288,"sha256":"cd12999681ec67a1718f066454d8349e72dfe66c87f2e4cf506be5a9dda4ef89","from":"AlphaPC","receivedAt":1794323764729,"fingerprint":"a1dbe30c20b96085fbf011df80479808"}
=== TWO-BROWSER UI E2E: PASS ===
```
- 判定：**PASS**

## 6. 本轮修复的真实缺陷
1. **中继帧类型丢失**：服务端一律用 BinaryMessage 下发，浏览器把 SDP offer 当二进制丢弃 → P2P 永远连不上。改为保留原始帧类型（文本/二进制）。
2. **早到帧被丢弃**：对方尚未进房时发送的 offer 被 `continue` 丢掉，握手卡死。改为会话级 backlog 缓存，进房即回放；客户端收到 `peer-joined` 时补发 offer。
3. **扫描结果被轮询覆盖**：每 3 秒的 mDNS 轮询用整表覆盖 peers，扫描结果立刻消失。改为按指纹合并 + 90 秒扫描结果缓存。
4. **扫描被请求超时掐断**：`timeout` 查询参数被当成扫描预算，两个浏览器同时扫描时扫描在 6 秒被取消，且空结果被缓存。改为独立 20 秒扫描预算、并发请求单飞合并、只有完整扫描才进缓存。
5. **接收端无反馈**：文件落盘后界面没有任何展示。新增队列面板「已接收」区（文件名/大小/来源/落盘路径/SHA-256 前 8 位）。
6. **同机双实例地址选择**：同一节点既能经 127.0.0.1 又能经 LAN IP 访问时，之前可能记成回环地址。改为优先记录 LAN 地址。

## 7. 构建产物
| 产物 | 大小 | 用途 |
|---|---|---|
| `bin/zixin-transfer.exe` | 7.93 MB | Windows 桌面端 / 局域网节点（内含前端） |
| `dist/zixin-transfer-linux-amd64` | 7.69 MB | 云端中转节点 |
| `webui/dist` | 8 个文件 | 被 go:embed 打包的 PWA 产物 |

---

# 第二轮：接收体验与设备管理（2026-11-10 晚）

## 8. 本轮新增能力

| # | 需求（来自使用反馈） | 实现 | 验证 |
|---|---|---|---|
| 7 | 收到的文件没有打开的渠道 | 「已接收」每行加 **↗ 打开文件** / **📁 在文件夹中显示**，调用宿主 `explorer /select,` 等 | 中文名文件 `报表-2026.xlsx` 与 ASCII 文件均返回 `{"ok":true}` |
| 8 | 需要总的保存路径设置 | 顶栏 ⚙ / 设备栏 ⚙ → **设置面板**：显示默认目录、系统目录选择框、直接粘路径、恢复默认；服务端校验可写后持久化到 `settings.json` | 改到 `...\Downloads\zz-test` → 服务端 `/api/settings` 立即返回新值，顶栏胶囊同步为「存到 zz-test」；恢复默认生效 |
| 9 | 单次接收也能选路径 | 接收弹窗内「保存到 … 更改…」，随 `POST /api/lan/accept` 传 `savePath`；服务端 `ensureDir` 后落盘到该目录，不影响默认目录 | UI 走完整流程：弹窗显示默认目录 → 更改 → 接受 → 提示「本次保存到 …」；接口级验证文件落在 `run\alt`，默认目录无副本 |
| 10 | 设备不是一直扫描/发一次就消失 | ① 子网扫描改为**常驻**（首扫 + 每 25 秒，空结果 2.5 秒后自动重试一次）② 设备表**持久化**（`trusted.json` 内含 devices）③ 离线设备灰显「离线 · N 分钟前」并保留可选 | 重启节点后设备仍在列表；浏览器连续 3 次冷启动均能发现 `BetaPC` / `CHINAMI-83LPN76` |
| 11 | 不能给设备备注 | 设备行铅笔 → 内联输入 → `POST /api/devices/rename`，`aliasOverride` 持久化且优先于对方自称 | UI 备注为「书房的Mac」→ 提示「已备注为“书房的Mac”」→ 列表即时更新 → `trusted.json` 中 `"aliasOverride": "客厅台式机"` 等中文正确落盘（UTF-8 往返验证） |

## 9. 本轮修掉的隐藏缺陷

7. **设备指纹每次启动都变**：`newFingerprint()` 是随机生成且只存在内存里，重启后同
   一台机器变成"新设备"，备注和已知设备全部漂移。改为 `device.json` 持久化身份
   （`loadOrCreateFingerprint`）。验证：重启 A 后 `device` 仍为 `cc340cc2…`。
8. **已接收列表重启即清空**：索引只在内存。改为写 `received.json`（追加 + 200 条上限），
   重启后 `/api/lan/received` 仍返回 `persist.bin`。
9. **相对路径直接暴露到界面**：`-data .\run\a` 这种相对路径会让设置面板显示 `.\run\a\Downloads`。
   启动时统一 `filepath.Abs`。
10. **"已知"标签误导**：合并后的设备一律显示来源标签（已知/SCAN），用户看不出在不在线。
    改为 `LAN` / `OFF`（离线）/ `MAN`（手动）。

## 10. 安全边界（本轮新增接口）

`/api/system/reveal`、`/api/system/open`、`/api/system/pick-folder` 会驱动宿主系统，因此：

- 只接受来源为 `127.0.0.1` / `::1` 的请求；手机或其它电脑调用返回 **403**（实测）
- 路径必须落在「已接收文件」或「接收目录」之内，否则 **403**
  （实测 `C:\Windows\notepad.exe` 被拒）
- 远程 P2P 落盘不接受客户端指定目录（`dir` 参数仅本机生效）

## 11. 回归测试（改动后重跑）

- 跨网段信令协议：`ALL SIGNALING TESTS PASSED`
- 双浏览器 UI 全链路：`PASS`，`ui-e2e.bin` 524288 B，
  SHA-256 `cd12999681ec67a1718f066454d8349e72dfe66c87f2e4cf506be5a9dda4ef89` 与源一致
- 接收记录里现在带上发送方指纹 `cc340cc2…`（稳定身份生效的旁证）
---

# 第三轮：聊天室 + 一次发多台（2026-11-11）

## 12. 需求澄清（重要）

用户说的「爱传」**不是** GitHub 上的 `cloudups/iTransfer`（2016 年那个纯文件传输 App），
而是网盘分享里的 **「爱传~局域网实时聊天传输工具」**，其中包含
`爱传_局域网聊天室_v2.zip`（68.18 MB）与 `爱传.zip`（64.71 MB）。

- 分享元数据经 Quark 接口确认：标题「爱传~局域网实时聊天传输工具」
- 该分享下载需登录（实测 `require login [share missing]` / `require login [guest]`），未能取到 zip
- `cloudups/iTransfer` 的复核结论（源码 + 成品 APK 字节扫描）：全仓 `聊天/群聊` 命中 **0**，
  `strings.xml` 只有发送/接收文件，`MainActivity` 只有两个 FAB → 它确实没有聊天室

因此聊天室是按「局域网实时聊天 + 传输」这个定位对齐实现的，而不是照抄。

## 13. 实现

| 能力 | 落点 |
|---|---|
| 房间 WebSocket（连上即回放历史、实时广播、加入/离开提示） | `internal/app/chat.go` `handleChatSocket` |
| 历史持久化（`chat.json`，200 条上限，重启不丢） | `chatStore` |
| 文件一次上传、多台下载 | `handleChatUpload` + `handleChatFile`（`chatfiles/`，24h 清理） |
| 跨设备同步（服端 relay，用节点设备表，不依赖浏览器列表） | `Server.relayChat` |
| 文件地址改写为发送方 LAN 地址 | `Server.publicFileURL` |
| 未读角标 | `ChatRoom.tsx` + `tab__badge` |
| UI | 「聊天室」标签页：气泡、文件卡片、拖拽上传、连接状态、同步开关 |

## 14. 本轮修掉的缺陷

11. **未知 `/api/*` 落到 SPA 兜底** → 返回 `200 + index.html`，导致老版本节点被误判为"送达"
    （`+3 台设备` 实为 1 台）。改为 `/api/` 前缀统一 **404 JSON**，并在 relay 侧校验
    `Content-Type: application/json` 才计数。
12. **客户端 fan-out 有时序竞态**（页面设备表还没刷新 → 文件公告漏发）。改为**服务端转发**。
13. **文件下载地址用了页面来源** → 以 `localhost` 打开界面时，别的设备点下载会打到它自己。
    改为 `http://<本机LAN地址>:<端口>/api/chat/file/{id}`（`/api/health` 新增 `address`）。
14. **扫描端口写死 `{本机, 8080, 8081}`** → 第二个实例在 8092/8084 时发现不到。
    改为扫描 `8080–8085` + 已知端口。

## 15. 验证（两节点：A=:8090，B=:8084）

| 项 | 结果 |
|---|---|
| 房间连接与历史 | `房间在线 · 1 人`，历史随连接回放 |
| 文本双向（经 WS） | A 发 → A 房间出现「(我)」，B 房间出现「AlphaPC: …」 |
| 文本跨节点（relay） | `delivered=1`（仅真实支持 chat 的节点），B 立即收到 |
| 文件一次上传 | 512 KB 上传 `delivered=1`，B 的公告地址 `http://10.0.0.31:8090/api/chat/file/…` |
| 从公告地址下载 | 200，524288 B，SHA-256 与源一致 |
| UI 上传（真实文件输入） | 卡片显示 `chat-ui-upload.bin · 256 KB · SHA-256 …`，链接可取 200/262144 |
| 未读角标 | 在其它标签页收到消息 → 角标 `1` |
| 页面错误 | A/B 均 `none` |