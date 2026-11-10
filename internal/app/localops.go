package app

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The node runs on the user's own machine, so it can do what a web page
// cannot: reveal a received file in the OS file manager, open it with the
// default application, and ask the OS for a folder. These actions only make
// sense — and are only allowed — for requests coming from this machine.

// isLoopback reports whether the request originated on this host.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// requireLocal guards a handler that drives the host OS.
func requireLocal(w http.ResponseWriter, r *http.Request) bool {
	if isLoopback(r) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error": "该操作只能在运行 子欣互传 的那台电脑上执行",
	})
	return false
}

// revealPath opens the OS file manager with the file selected.
func revealPath(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("文件不存在: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		if info.IsDir() {
			return exec.Command("explorer", p).Start()
		}
		return exec.Command("explorer", "/select,", p).Start()
	case "darwin":
		if info.IsDir() {
			return exec.Command("open", p).Start()
		}
		return exec.Command("open", "-R", p).Start()
	default:
		dir := p
		if !info.IsDir() {
			dir = filepath.Dir(p)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

// openPath opens a file with the desktop default application.
func openPath(p string) error {
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("文件不存在: %w", err)
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", p).Start()
	case "darwin":
		return exec.Command("open", p).Start()
	default:
		return exec.Command("xdg-open", p).Start()
	}
}

// pickFolder shows the native folder chooser on the host desktop.
//
// It is deliberately Windows-first: that is where the desktop client runs.
// Other platforms return a clear message instead of guessing.
func pickFolder(start string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("当前系统不支持原生目录选择，请手动输入路径")
	}
	script := `
Add-Type -AssemblyName System.Windows.Forms | Out-Null
$dlg = New-Object System.Windows.Forms.FolderBrowserDialog
$dlg.Description = '选择 子欣互传 保存位置'
$dlg.ShowNewFolderButton = $true
if ('` + strings.ReplaceAll(start, "'", "''") + `' -ne '') { $dlg.SelectedPath = '` + strings.ReplaceAll(start, "'", "''") + `' }
$owner = New-Object System.Windows.Forms.Form
$owner.TopMost = $true
$owner.WindowState = 'Minimized'
$owner.ShowInTaskbar = $false
$res = $dlg.ShowDialog($owner)
if ($res -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::Out.Write($dlg.SelectedPath)
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = hiddenWindow()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("打开目录选择器失败: %w", err)
	}
	picked := strings.TrimSpace(string(out))
	if picked == "" {
		return "", nil // the user cancelled; not an error
	}
	return picked, nil
}

// ensureDir makes sure a receive directory exists and is writable.
func ensureDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("目录为空")
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("必须是绝对路径")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法创建目录: %w", err)
	}
	probe := filepath.Join(dir, ".zixin-transfer-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("目录不可写: %w", err)
	}
	_ = os.Remove(probe)
	return nil
}

var _ = time.Second
