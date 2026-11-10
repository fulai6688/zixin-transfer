//go:build windows

package app

import "syscall"

// hiddenWindow keeps helper processes (folder picker, explorer) from flashing
// a console window on the user's desktop.
func hiddenWindow() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
