@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

if not exist "bin\zixin-transfer.exe" (
  echo [x] 找不到 bin\zixin-transfer.exe
  echo     请先在本目录执行构建：go build -ldflags="-s -w" -o bin\zixin-transfer.exe .\cmd\zixin-transfer
  pause
  exit /b 1
)

rem 云端中转地址：填了才能跨网段（手机流量 <-> 家里电脑）。留空则只做局域网。
if "%ZX_RELAY%"=="" set "ZX_RELAY="

echo ============================================
echo   子欣互传 启动中…
echo   界面会自动在浏览器打开 http://127.0.0.1:8080
echo   局域网内其它设备打开同一地址也能用
echo ============================================
echo.

if "%ZX_RELAY%"=="" (
  "bin\zixin-transfer.exe" -port 8080
) else (
  "bin\zixin-transfer.exe" -port 8080 -relay "%ZX_RELAY%"
)

echo.
echo 子欣互传 已退出。
pause
