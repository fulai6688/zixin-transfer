//go:build !windows

package app

import "syscall"

// hiddenWindow is a no-op outside Windows.
func hiddenWindow() *syscall.SysProcAttr { return nil }
