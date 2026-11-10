@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

rem 需要管理员权限：右键"以管理员身份运行"。
rem 作用：放行 子欣互传 的入站 TCP/UDP（否则别的设备连不进来），以及 mDNS 的 UDP 5353。

net session >nul 2>&1
if errorlevel 1 (
  echo [x] 需要管理员权限。请右键本文件 -^> "以管理员身份运行"。
  pause
  exit /b 1
)

echo 正在添加防火墙规则…
netsh advfirewall firewall delete rule name="子欣互传 (局域网 P2P 文件传输)" >nul 2>&1
netsh advfirewall firewall delete rule name="子欣互传 (局域网 P2P 文件传输) (mDNS)" >nul 2>&1

netsh advfirewall firewall add rule name="子欣互传 (局域网 P2P 文件传输)" dir=in action=allow program="%~dp0bin\zixin-transfer.exe" enable=yes profile=any protocol=TCP,UDP
netsh advfirewall firewall add rule name="子欣互传 (局域网 P2P 文件传输) (mDNS)" dir=in action=allow program="%~dp0bin\zixin-transfer.exe" enable=yes profile=any protocol=UDP localport=5353

echo.
echo 完成。现在：
echo   - 同一 WiFi 下的手机/电脑能发现本机（mDNS）
echo   - 即使路由器/防火墙挡了组播，界面里的"扫描局域网"也会用 TCP 扫出来
echo.
echo 移除规则：netsh advfirewall firewall delete rule name="子欣互传 (局域网 P2P 文件传输)"
pause
