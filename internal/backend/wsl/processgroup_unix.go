//go:build !windows

package wsl

import (
	"os/exec"
	"syscall"
)

// setOwnProcessGroup puts cmd in its own host-side process group so teardown
// can address the whole tree (process-group ids do not exist on Windows; see
// the windows variant of these helpers).
func setOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// pgidOf returns cmd's process-group id (valid after Start).
func pgidOf(cmd *exec.Cmd) (int, error) {
	return syscall.Getpgid(cmd.Process.Pid)
}

// killProcessGroup sends SIGTERM to the whole host-side process group. This is
// only the belt; the definitive teardown happens guest-side (see Stop).
func killProcessGroup(pgid int) error {
	if pgid <= 0 {
		return nil
	}
	return syscall.Kill(-pgid, syscall.SIGTERM)
}
