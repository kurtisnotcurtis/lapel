//go:build !windows

package wsl

func preserveConsoleMode() func() {
	return func() {}
}
