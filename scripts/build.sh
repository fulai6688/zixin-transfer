#!/usr/bin/env bash
# 子欣互传 · Linux/macOS 构建脚本（云端中转节点也用这个）
#
#   ./scripts/build.sh                 # 本机平台
#   ./scripts/build.sh linux amd64     # 交叉编译
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="$(tr -d '\r\n' < VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

GOOS_ARG="${1:-$(go env GOOS)}"
GOARCH_ARG="${2:-$(go env GOARCH)}"

echo "==> 版本 $VERSION  commit $COMMIT  built $BUILT_AT  目标 ${GOOS_ARG}/${GOARCH_ARG}"

OUT_DIR="dist"
mkdir -p "$OUT_DIR"
NAME="zixin-transfer-${GOOS_ARG}-${GOARCH_ARG}"
[ "$GOOS_ARG" = "windows" ] && NAME="${NAME}.exe"

CGO_ENABLED=0 GOOS="$GOOS_ARG" GOARCH="$GOARCH_ARG" \
  go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILT_AT}" \
  -o "$OUT_DIR/$NAME" ./cmd/zixin-transfer

ls -lh "$OUT_DIR/$NAME"
