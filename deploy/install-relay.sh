#!/usr/bin/env bash
# 子欣互传 云端中转节点一键部署（Ubuntu 22.04 / Debian 12，腾讯云轻量可用）
#
#   用法：
#     sudo bash install-relay.sh send.example.com /path/to/zixin-transfer-linux-amd64
#
#   第 1 个参数：你的域名（必须已解析到本机公网 IP）
#   第 2 个参数：linux 版二进制路径（在开发机上交叉编译：
#                CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o zixin-transfer-linux-amd64 ./cmd/zixin-transfer）
#
# 脚本做四件事：建用户 + 装服务、放二进制、配 nginx 反代、签发证书。
set -euo pipefail

DOMAIN="${1:-}"
BINARY="${2:-}"

if [[ -z "$DOMAIN" || -z "$BINARY" ]]; then
  echo "用法: sudo bash install-relay.sh <域名> <二进制路径>" >&2
  exit 1
fi
if [[ ! -f "$BINARY" ]]; then
  echo "找不到二进制: $BINARY" >&2
  exit 1
fi

echo "==> 1/6 安装依赖"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nginx certbot python3-certbot-nginx curl ufw

echo "==> 2/6 创建运行用户与目录"
id -u zixin-transfer >/dev/null 2>&1 || useradd --system --home /var/lib/zixin-transfer --shell /usr/sbin/nologin zixin-transfer
install -d -o zixin-transfer -g zixin-transfer -m 0750 /var/lib/zixin-transfer /var/lib/zixin-transfer/Downloads /opt/zixin-transfer

echo "==> 3/6 安装二进制"
install -m 0755 "$BINARY" /opt/zixin-transfer/zixin-transfer
/opt/zixin-transfer/zixin-transfer -version || true

echo "==> 4/6 写入 systemd 服务"
sed "s#https://send.example.com#https://${DOMAIN}#g" "$(dirname "$0")/zixin-relay.service" > /etc/systemd/system/zixin-relay.service
systemctl daemon-reload
systemctl enable --now zixin-transfer
sleep 1
curl -fsS "http://127.0.0.1:8080/api/health" && echo || { echo "服务未起来，看 journalctl -u zixin-transfer -n 50" >&2; exit 1; }

echo "==> 5/6 配置 nginx 反代"
sed "s#send.example.com#${DOMAIN}#g" "$(dirname "$0")/nginx-zixin.conf" > /etc/nginx/sites-available/zixin-transfer.conf
ln -sf /etc/nginx/sites-available/zixin-transfer.conf /etc/nginx/sites-enabled/zixin-transfer.conf
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx

echo "==> 6/6 放行端口 + 签发证书"
ufw allow 22/tcp   >/dev/null 2>&1 || true
ufw allow 80/tcp   >/dev/null 2>&1 || true
ufw allow 443/tcp  >/dev/null 2>&1 || true
certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos --register-unsafely-without-email --redirect || {
  echo "证书签发失败：确认域名已解析到本机公网 IP，且云厂商安全组已放行 80/443。" >&2
  exit 1
}

echo
echo "完成。自检："
echo "  curl -s https://${DOMAIN}/api/health"
echo "  浏览器打开 https://${DOMAIN}  （手机可直接「添加到主屏幕」当 App 用）"
echo
echo "客户端连法：本机 子欣互传 启动时加 -relay https://${DOMAIN}"
