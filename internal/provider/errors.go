package provider

import "fmt"

// ErrNotInstalled indicates the provider CLI is not found or not in PATH.
type ErrNotInstalled struct {
	Name string
}

func (e *ErrNotInstalled) Error() string {
	return fmt.Sprintf("%s: CLI not installed or not in PATH", e.Name)
}

// ErrServerDown indicates the provider server is not running.
type ErrServerDown struct {
	Name string
	Detail string
}

func (e *ErrServerDown) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: server not running - %s", e.Name, e.Detail)
	}
	return fmt.Sprintf("%s: server not running", e.Name)
}

// ErrDetectFailed indicates a detection error (e.g., CLI exists but status check failed).
type ErrDetectFailed struct {
	Name string
	Err  error
}

func (e *ErrDetectFailed) Error() string {
	return fmt.Sprintf("%s: detection error - %v", e.Name, e.Err)
}

func (e *ErrDetectFailed) Unwrap() error {
	return e.Err
}
