# 子欣互传 · 发版脚本（打 tag + 推送 + 生成 Release）
#
#   powershell -ExecutionPolicy Bypass -File scripts/release.ps1 -Version 1.0.1 -Notes "修复 xxx"
#
# 前置：已配置 git 凭据（能 push）；工作区干净。
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [string]$Notes = '',
    [switch]$Draft
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$current = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "版本号必须是 x.y.z：$Version" }
if (git status --porcelain) { throw '工作区不干净，先提交或 stash' }

Write-Host "==> 版本 $current → $Version" -ForegroundColor Cyan
[IO.File]::WriteAllText((Join-Path $root 'VERSION'), "$Version`n", (New-Object System.Text.UTF8Encoding($false)))

# 1. 构建两个平台
powershell -ExecutionPolicy Bypass -File (Join-Path $root 'scripts/build.ps1') -Version $Version
$env:CGO_ENABLED = '0'; $env:GOOS = 'linux'; $env:GOARCH = 'amd64'
$commit = (git rev-parse --short HEAD 2>$null)
if (-not $commit) { $commit = 'none' }
$builtAt = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
New-Item -ItemType Directory -Force -Path (Join-Path $root 'dist') | Out-Null
& go build -trimpath -ldflags "-s -w -X main.version=$Version -X main.commit=$commit -X main.buildTime=$builtAt" `
    -o (Join-Path $root 'dist/zixin-transfer-linux-amd64') ./cmd/zixin-transfer
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'

# 2. 提交 + 打 tag
git add -A
git commit -m "chore(release): v$Version"
if (-not $Notes) { $Notes = "v$Version" }
git tag -a "v$Version" -m "v$Version`n`n$Notes"
git push origin HEAD
git push origin "v$Version"

# 3. Release（GitHub CLI；没装就只推 tag，由 CI 出包）
if (Get-Command gh -ErrorAction SilentlyContinue) {
    $args = @('release', 'create', "v$Version",
        (Join-Path $root 'bin/zixin-transfer.exe'),
        (Join-Path $root 'dist/zixin-transfer-linux-amd64'),
        '--title', "v$Version", '--notes', $Notes)
    if ($Draft) { $args += '--draft' }
    & gh @args
    Write-Host "==> Release v$Version 已创建" -ForegroundColor Green
} else {
    Write-Host '==> 已推 tag；未安装 gh，Release 由 CI 生成（.github/workflows/release.yml）' -ForegroundColor Yellow
}
