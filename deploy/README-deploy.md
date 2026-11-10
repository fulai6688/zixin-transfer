# 子欣互传 云端中转部署（腾讯云轻量应用服务器）

跨网段（手机 4G ↔ 家里电脑）必须有一台公网服务器做**信令中转**。它只转发握手包，
P2P 打洞成功时数据不经过它；打洞失败才降级走它中转。

一台 1 核 2G 的轻量服务器就够几十人日常用。

---

## 1. 准备

| 项 | 说明 |
|---|---|
| 服务器 | 腾讯云轻量 Ubuntu 22.04（1C2G 起） |
| 域名 | 一个子域，如 `send.example.com`，A 记录指向服务器公网 IP |
| 端口 | 云厂商**安全组**放行 22 / 80 / 443（这一步漏了证书一定签不下来） |
| 本地 | Go 工具链（交叉编译 linux 二进制） |

## 2. 交叉编译 linux 版

在开发机 `zixin-transfer/` 目录：

```powershell
$env:CGO_ENABLED='0'; $env:GOOS='linux'; $env:GOARCH='amd64'
go build -ldflags="-s -w" -o ..\dist\zixin-transfer-linux-amd64 .\cmd\zixin-transfer
```

产物约 8 MB，单文件、无依赖。

## 3. 上传并一键部署

```bash
scp deploy/install-relay.sh deploy/zixin-relay.service deploy/nginx-zixin.conf \
    ../dist/zixin-transfer-linux-amd64 root@<服务器IP>:/root/

ssh root@<服务器IP>
cd /root && chmod +x install-relay.sh
sudo bash install-relay.sh send.example.com /root/zixin-transfer-linux-amd64
```

脚本依次完成：装 nginx/certbot → 建 `zixin-transfer` 用户 → 装二进制到 `/opt/zixin-transfer`
→ 写 systemd 并启动 → 配 nginx（含 WebSocket 升级、不限请求体、长连接不超时）
→ 放行端口 → certbot 签证书并强制 https。

## 4. 自检

```bash
curl -s https://send.example.com/api/health
# {"ok":true,"version":"0.1.0","alias":"CloudRelay","platform":"linux","device":"…","port":8080,"relay":"https://send.example.com"}

systemctl status zixin-transfer --no-pager
journalctl -u zixin-transfer -f          # 实时日志，能看到 POST /api/session 之类的请求
```

浏览器打开 `https://send.example.com` 应当直接出现 子欣互传 界面（PWA，可"添加到主屏幕"）。

## 5. 客户端怎么连

Windows 桌面端启动时把中转地址传进去：

```powershell
.\bin\zixin-transfer.exe -relay https://send.example.com
```

或设环境变量（推荐写进启动脚本）：

```powershell
$env:ZX_RELAY='https://send.example.com'
.\bin\zixin-transfer.exe
```

之后界面里点"生成取件码"，会话就创建在云端；另一台设备（手机浏览器打开同一域名，
或另一台电脑的客户端）输入这 6 位码即可握手。

## 6. 运维

| 操作 | 命令 |
|---|---|
| 升级二进制 | `install -m0755 zixin-transfer-linux-amd64 /opt/zixin-transfer/zixin-transfer && systemctl restart zixin-transfer` |
| 改别名/端口 | 编辑 `/etc/systemd/system/zixin-relay.service` 后 `systemctl daemon-reload && systemctl restart zixin-transfer` |
| 看会话数 | 会话 1 小时自动过期，无持久化状态；重启不丢配置 |
| 证书续期 | certbot 装好即有 systemd timer，`certbot renew --dry-run` 验证 |
| 数据落盘 | 只在中转模式下才会写 `/var/lib/zixin-transfer/Downloads`，可定期清理 |

## 7. 安全说明

- 信令服务器**看不到文件内容**：P2P 成功时数据走 DTLS/SCTP 直连；失败降级才经服务器转发，且只转发已有会话内的帧。
- 会话靠 6 位码（32^6 ≈ 10 亿种）+ 1 小时过期 + 首次加入即占位（同一角色不能重复占用）。
- 服务器不存账号、不存文件索引，进程重启即清空会话。
- 若要更强隔离：给 nginx 加 `limit_req` 限制 `/api/session` 频率即可防爆破（每分钟 10 次足够）。
