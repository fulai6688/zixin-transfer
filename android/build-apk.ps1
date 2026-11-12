# 子欣互传 · Android APK 构建（不依赖 Gradle）
#
#   powershell -ExecutionPolicy Bypass -File android/build-apk.ps1
#
# 产物：dist/zixin-transfer-android.apk（含 icon、已签名、zipalign）
param(
    [string]$Version = '',
    [string]$OutDir = 'dist',
    [string]$Keystore = '',
    [string]$StorePass = 'zixin2026',
    [string]$KeyAlias = 'zixin'
)

$ErrorActionPreference = 'Continue'
$root = Split-Path -Parent $PSScriptRoot
$androidDir = Join-Path $root 'android'
Set-Location $root

if (-not $Version) { $Version = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim() }
$versionCode = 10000 + [int](($Version -split '\.')[0]) * 100 + [int](($Version -split '\.')[1]) * 10

# ---------------------------------------------------------------- 工具链
$sdk = if ($env:ANDROID_HOME) { $env:ANDROID_HOME } elseif ($env:ANDROID_SDK_ROOT) { $env:ANDROID_SDK_ROOT } else { 'C:\Users\kkkkkk\Documents\Codex\2026-08-20\k\work\android-sdk' }
if (-not (Test-Path $sdk)) { throw "找不到 Android SDK：$sdk（设置 ANDROID_HOME 或改脚本里的默认值）" }

$bt = Get-ChildItem (Join-Path $sdk 'build-tools') -Directory | Sort-Object Name -Descending | Select-Object -First 1
$aapt2 = Join-Path $bt.FullName 'aapt2.exe'
$d8 = Join-Path $bt.FullName 'd8.bat'
$zipalign = Join-Path $bt.FullName 'zipalign.exe'
$apksigner = Join-Path $bt.FullName 'apksigner.bat'

$platform = Get-ChildItem (Join-Path $sdk 'platforms') -Directory | Sort-Object Name -Descending | Select-Object -First 1
$androidJar = Join-Path $platform.FullName 'android.jar'
foreach ($t in @($aapt2, $d8, $zipalign, $apksigner, $androidJar)) {
    if (-not (Test-Path $t)) { throw "缺少构建工具：$t" }
}
Write-Host "==> build-tools $($bt.Name) / platform $($platform.Name) / 版本 $Version ($versionCode)" -ForegroundColor Cyan

# ---------------------------------------------------------------- 中间目录
$tmp = Join-Path $root 'build\apk'
if (Test-Path $tmp) { Remove-Item $tmp -Recurse -Force }
New-Item -ItemType Directory -Force -Path $tmp, (Join-Path $tmp 'res'), (Join-Path $tmp 'classes'), (Join-Path $tmp 'dex') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $root $OutDir) | Out-Null

# ---------------------------------------------------------------- 图标（从品牌母版生成）
$iconMaster = Join-Path $root 'docs\assets\icon.png'
if (-not (Test-Path $iconMaster)) { $iconMaster = Join-Path $root 'build\icon.png' }
if (Test-Path $iconMaster) {
    $py = (Get-Command python -ErrorAction SilentlyContinue)
    if ($py) {
        Write-Host '==> 生成 Android 图标'
        $pyScript = @'
import os, sys
from PIL import Image
root = sys.argv[1]
master = Image.open(sys.argv[2]).convert("RGBA")
sizes = {"mdpi":48, "hdpi":72, "xhdpi":96, "xxhdpi":144, "xxxhdpi":192}
for dpi, px in sizes.items():
    d = os.path.join(root, "android", "res", "mipmap-" + dpi)
    os.makedirs(d, exist_ok=True)
    master.resize((px, px), Image.LANCZOS).save(os.path.join(d, "ic_launcher.png"))
    # 圆形/自适应风格留白版本（部分启动器喜欢）
print("icons ok")
'@
        $pyFile = Join-Path $tmp 'mkicons.py'
        [IO.File]::WriteAllText($pyFile, $pyScript, (New-Object System.Text.UTF8Encoding($false)))
        & python $pyFile $root $iconMaster
    }
} else {
    Write-Warning '没有品牌图标母版，APK 将使用系统默认图标'
}

# ---------------------------------------------------------------- manifest 里写入版本号
$manifestSrc = Join-Path $androidDir 'AndroidManifest.xml'
$manifest = Join-Path $tmp 'AndroidManifest.xml'
$xml = [IO.File]::ReadAllText($manifestSrc)
$xml = [regex]::Replace($xml, 'android:versionCode="\d+"', "android:versionCode=`"$versionCode`"")
$xml = [regex]::Replace($xml, 'android:versionName="[^"]+"', "android:versionName=`"$Version`"")
[IO.File]::WriteAllText($manifest, $xml, (New-Object System.Text.UTF8Encoding($false)))

# ---------------------------------------------------------------- 资源
Write-Host '==> 编译资源'
& $aapt2 compile --dir (Join-Path $androidDir 'res') -o (Join-Path $tmp 'res\res.zip')
if ($LASTEXITCODE -ne 0) { throw 'aapt2 compile 失败' }

$unsigned = Join-Path $tmp 'unsigned.apk'
& $aapt2 link -o $unsigned -I $androidJar --manifest $manifest `
    --min-sdk-version 21 --target-sdk-version 34 `
    --version-code $versionCode --version-name $Version `
    (Join-Path $tmp 'res\res.zip') --auto-add-overlay
if ($LASTEXITCODE -ne 0) { throw 'aapt2 link 失败' }

# ---------------------------------------------------------------- Java → dex
Write-Host '==> 编译 Java'
$sources = Get-ChildItem (Join-Path $androidDir 'src') -Recurse -Filter '*.java' | ForEach-Object { $_.FullName }
& javac -Xlint:-options -encoding UTF-8 -source 8 -target 8 -bootclasspath $androidJar -d (Join-Path $tmp 'classes') $sources 2>&1 |
    Where-Object { $_ -notmatch 'bootstrap class path|source value 8|target value 8|deprecat' }
if ($LASTEXITCODE -ne 0) { throw 'javac 失败' }

Write-Host '==> d8 → classes.dex'
$classes = Get-ChildItem (Join-Path $tmp 'classes') -Recurse -Filter '*.class' | ForEach-Object { $_.FullName }
& $d8 --release --min-api 21 --lib $androidJar --output (Join-Path $tmp 'dex') $classes
if ($LASTEXITCODE -ne 0) { throw 'd8 失败' }

# ---------------------------------------------------------------- 塞进 APK
Write-Host '==> 打包 dex'
$aligned = Join-Path $tmp 'aligned.apk'
Copy-Item $unsigned $aligned -Force
$sevenZip = 'E:\7z\7-Zip\7z.exe'
if (Test-Path $sevenZip) {
    & $sevenZip a -tzip $aligned (Join-Path $tmp 'dex\classes.dex') -mx=0 | Out-Null
} else {
    # 没有 7z 时用 .NET 直接写入 zip
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::Open($aligned, 'Update')
    [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, (Join-Path $tmp 'dex\classes.dex'), 'classes.dex') | Out-Null
    $zip.Dispose()
}

# ---------------------------------------------------------------- 对齐 + 签名
Write-Host '==> zipalign + 签名'
$alignedOut = Join-Path $tmp 'aligned-final.apk'
& $zipalign -f 4 $aligned $alignedOut
if ($LASTEXITCODE -ne 0) { throw 'zipalign 失败' }

if (-not $Keystore) { $Keystore = Join-Path $androidDir 'zixin-release.jks' }
if (-not (Test-Path $Keystore)) {
    Write-Host "==> 生成签名密钥 $Keystore（密码 $StorePass，请自行保管）"
    $keytool = (Get-Command keytool -ErrorAction SilentlyContinue).Source
    if (-not $keytool) { $keytool = 'F:\dsh-tools\jdk-21.0.13+9\bin\keytool.exe' }
    & $keytool -genkeypair -keystore $Keystore -alias $KeyAlias -keyalg RSA -keysize 2048 `
        -validity 10950 -storepass $StorePass -keypass $StorePass `
        -dname 'CN=Zixin Transfer, OU=Personal, O=Zixin, L=Local, ST=Local, C=CN'
    if ($LASTEXITCODE -ne 0) { throw 'keytool 失败' }
}

$apkOut = Join-Path $root "$OutDir\zixin-transfer-android.apk"
Copy-Item $alignedOut $apkOut -Force
& $apksigner sign --ks $Keystore --ks-key-alias $KeyAlias `
    --ks-pass "pass:$StorePass" --key-pass "pass:$StorePass" --out $apkOut $alignedOut
if ($LASTEXITCODE -ne 0) { throw 'apksigner 失败' }

& $apksigner verify --print-certs $apkOut | Select-Object -First 4
$size = [math]::Round((Get-Item $apkOut).Length / 1KB, 1)
Write-Host "==> 完成: $apkOut ($size KB)" -ForegroundColor Green
Write-Host "    安装: adb install -r `"$apkOut`"" -ForegroundColor Gray
