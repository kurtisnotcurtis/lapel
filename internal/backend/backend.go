// Package backend defines the execution-backend abstraction and hosts the
// WSL implementation under backend/wsl. Domain logic must not be coupled to
// WSL commands directly; it goes through this interface (spec 4.2).
package backend

import (
	"context"

	"lapel/internal/domain"
)

// ExecutionBackend is the boundary between the control plane and a Linux
// runtime. Implementations own all host-specific mechanics (wsl.exe invocation,
// distro discovery, provisioning, process tracking, lifecycle).
type ExecutionBackend interface {
	// Name identifies the backend (e.g. "wsl").
	Name() string

	// Detect reports whether the backend is installed and available.
	Detect(ctx context.Context) (domain.BackendStatus, error)

	// Provision imports/prepares a distribution per spec. Idempotent.
	Provision(ctx context.Context, spec domain.Environment) error

	// Inspect returns the runtime state of the bound environment.
	Inspect(ctx context.Context) (domain.EnvironmentStatus, error)

	// Exec runs a structured command inside the backend and returns results.
	Exec(ctx context.Context, req domain.ExecRequest) (*domain.ExecResult, error)

	// Stop terminates a session and its descendant processes.
	Stop(ctx context.Context, sessionID string) error

	// Destroy removes managed resources for an environment after confirmation.
	Destroy(ctx context.Context, id string) error
}
