package wsl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"lapel/internal/domain"
)

// DistBackend is the WSL execution backend bound to a single distribution.
type DistBackend struct {
	// Dist is the target WSL distribution name (defaults to DefaultDist when empty).
	Dist string
}

// NewBackend returns a DistBackend bound to the given distribution name
// (defaulting to DefaultDist when empty).
func NewBackend(distro string) *DistBackend {
	d := strings.TrimSpace(distro)
	if d == "" {
		d = DefaultDist
	}
	return &DistBackend{Dist: d}
}

func (b *DistBackend) dist() string {
	if b != nil && strings.TrimSpace(b.Dist) != "" {
		return strings.TrimSpace(b.Dist)
	}
	return DefaultDist
}

// Name identifies the backend implementation.
func (b *DistBackend) Name() string { return "wsl" }

// Provision imports/prepares the distro per spec and installs declared deps.
// Idempotent: repeating without manifest changes does not reinstall everything.
func (b *DistBackend) Provision(ctx context.Context, spec domain.Environment) error {
	if strings.TrimSpace(spec.Name) == "" {
		spec.Name = b.dist()
	}
	return provisionDistro(ctx, DefaultProvisionOptions(spec))
}

// Destroy removes managed resources for an environment after confirmation.
func (b *DistBackend) Destroy(ctx context.Context, id string) error {
	if _, err := runWslGlobal(ctx, "--unregister", id); err != nil {
		return fmt.Errorf("destroying environment %s: %w", id, err)
	}
	if dir, err := DistroStorageDir(id); err == nil && dir != "" {
		_ = os.RemoveAll(dir)
	}
	return nil
}
