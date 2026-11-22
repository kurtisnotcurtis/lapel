//go:build windows

package wsl

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32DLL        = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32DLL.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32DLL.NewProc("SetConsoleMode")
)

// preserveConsoleMode captures the current Windows console output modes for
// stdout and stderr before launching wsl.exe and returns a function that
// restores them on exit. wsl.exe can mutate the parent console mode (e.g.
// enabling DISABLE_NEWLINE_AUTO_RETURN), causing subsequent \n writes to
// staircase across the terminal.
func preserveConsoleMode() func() {
	type savedHandle struct {
		h    syscall.Handle
		mode uint32
	}
	var saved []savedHandle
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := syscall.Handle(f.Fd())
		var mode uint32
		r1, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
		if r1 != 0 {
			saved = append(saved, savedHandle{h: h, mode: mode})
		}
	}
	return func() {
		for _, s := range saved {
			_, _, _ = procSetConsoleMode.Call(uintptr(s.h), uintptr(s.mode))
		}
	}
}
