package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// FirewallRuleName is the display name of the Windows firewall rules we add.
const FirewallRuleName = "子欣互传 (局域网 P2P 文件传输)"

// InstallFirewall adds inbound firewall rules so other devices on the
// network can reach this node (TCP/UDP app port plus mDNS on 5353).
//
// It requires elevation. On Windows the rules are added by re-launching a
// generated batch file through UAC, so the user only has to click "Yes"
// once. On other platforms it is a no-op.
func InstallFirewall() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("firewall rules are only managed on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	script := fmt.Sprintf(`@echo off
setlocal
netsh advfirewall firewall delete rule name="%[1]s" >nul 2>&1
netsh advfirewall firewall delete rule name="%[1]s (mDNS)" >nul 2>&1
netsh advfirewall firewall add rule name="%[1]s" dir=in action=allow program="%[2]s" enable=yes profile=any protocol=TCP,UDP
netsh advfirewall firewall add rule name="%[1]s (mDNS)" dir=in action=allow program="%[2]s" enable=yes profile=any protocol=UDP localport=5353
exit /b 0
`, FirewallRuleName, exe)

	batch := filepath.Join(os.TempDir(), "zixin-firewall.cmd")
	if err := os.WriteFile(batch, []byte(script), 0o644); err != nil {
		return err
	}
	defer os.Remove(batch)

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", fmt.Sprintf("Start-Process -FilePath '%s' -Verb RunAs -Wait -WindowStyle Hidden", batch))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("elevated helper failed: %w", err)
	}
	return nil
}

// UninstallFirewall removes the rules added by InstallFirewall.
func UninstallFirewall() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("firewall rules are only managed on Windows")
	}
	script := fmt.Sprintf(`@echo off
netsh advfirewall firewall delete rule name="%s" >nul 2>&1
netsh advfirewall firewall delete rule name="%s (mDNS)" >nul 2>&1
exit /b 0
`, FirewallRuleName, FirewallRuleName)
	batch := filepath.Join(os.TempDir(), "zixin-firewall-remove.cmd")
	if err := os.WriteFile(batch, []byte(script), 0o644); err != nil {
		return err
	}
	defer os.Remove(batch)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", fmt.Sprintf("Start-Process -FilePath '%s' -Verb RunAs -Wait -WindowStyle Hidden", batch))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// FirewallInstalled reports whether the rules look present already.
func FirewallInstalled() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	out, err := exec.Command("netsh", "advfirewall", "firewall", "show", "rule",
		"name="+FirewallRuleName).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), FirewallRuleName)
}
