//go:build windows

package wsl

import "os/exec"

// setOwnProcessGroup is a no-op on Windows: wsl.exe has no Unix process-group
// semantics to join. Teardown relies on the guest-side kill (see Stop).
func setOwnProcessGroup(cmd *exec.Cmd) {}

// pgidOf returns 0 on Windows; there is no host-side process group to track.
func pgidOf(cmd *exec.Cmd) (int, error) { return 0, nil }

// killProcessGroup is a no-op on Windows; the guest-side kill does the work.
func killProcessGroup(pgid int) error { return nil }
