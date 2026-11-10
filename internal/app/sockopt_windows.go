//go:build windows

package app

import "syscall"

// setReuseAddr enables address reuse so several 子欣互传 nodes can share
// UDP port 5353 on the same host.
func setReuseAddr(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}
