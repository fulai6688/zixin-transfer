# 子欣互传 · 本地构建脚本
#
#   powershell -ExecutionPolicy Bypass -File scripts/build.ps1
#   powershell -ExecutionPolicy Bypass -File scripts/build.ps1 -Version 1.0.1 -SkipWeb
#
# 做的事：读 VERSION → 构建前端（除非 -SkipWeb）→ 生成图标 → 注入版本/提交/时间 → 出 Windows 二进制
param(
    [string]$Version = '',
    [switch]$SkipWeb,
    [switch]$SkipIcons,
    [string]$OutDir = 'bin'
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $Version) {
    $Version = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
}
$commit = 'none'
try { $commit = (git rev-parse --short HEAD 2>$null) } catch {}
if (-not $commit) { $commit = 'none' }
$builtAt = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')

Write-Host "==> 版本 $Version  commit $commit  built $builtAt" -ForegroundColor Cyan

# 1. 图标（改品牌色/形状后需要跑）
if (-not $SkipIcons) {
    $py = Get-Command python -ErrorAction SilentlyContinue
    if ($py) {
        Write-Host '==> 生成图标'
        & python (Join-Path $root 'scripts/make_icons.py')
    } else {
        Write-Warning '未找到 python，跳过图标生成'
    }
}

# 2. 前端
if (-not $SkipWeb) {
    Write-Host '==> 构建前端'
    Push-Location (Join-Path $root 'web')
    try {
        if (-not (Test-Path 'node_modules')) { npm install }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw "前端构建失败 (exit $LASTEXITCODE)" }
    } finally { Pop-Location }
} else {
    Write-Host '==> 跳过前端（使用已有 webui/dist）'
}

# 3. 后端
Write-Host '==> 编译后端'
$out = Join-Path $root $OutDir
New-Item -ItemType Directory -Force -Path $out | Out-Null
$exe = Join-Path $out 'zixin-transfer.exe'
$ldflags = "-s -w -X main.version=$Version -X main.commit=$commit -X main.buildTime=$builtAt"
& go build -trimpath -ldflags $ldflags -o $exe ./cmd/zixin-transfer
if ($LASTEXITCODE -ne 0) { throw "编译失败 (exit $LASTEXITCODE)" }

$size = [math]::Round((Get-Item $exe).Length / 1MB, 2)
Write-Host "==> 完成: $exe ($size MB)" -ForegroundColor Green
& $exe -version
