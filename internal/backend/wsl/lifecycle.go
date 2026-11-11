package wsl

import (
	"context"
	"fmt"
	"strings"

	"lapel/internal/backend"
	"lapel/internal/domain"
)

// DistBackend is the WSL execution backend bound to a single distribution.
type DistBackend struct{}

// Name identifies the backend implementation.
func (b *DistBackend) Name() string { return "wsl" }

// Compile-time assertion that DistBackend satisfies the backend boundary.
var _ backend.ExecutionBackend = (*DistBackend)(nil)

// Provision imports/prepares the distro per spec and installs declared deps.
// Idempotent: repeating without manifest changes does not reinstall everything.
func (b *DistBackend) Provision(ctx context.Context, spec domain.Environment) error {
	return provisionDistro(ctx, b, DefaultProvisionOptions(spec))
}

// Stop terminates all active lapel sessions by killing their process groups.
// It finds all /tmp/lapel-*.pg files, reads the PIDs from them, and kills those process groups.
func (b *DistBackend) Stop(ctx context.Context, sessionID string) error {
	// Find all active session pgFiles
	findScript := "ls /tmp/lapel-*.pg 2>/dev/null"
	out, err := runWsl(ctx, "bash", "-c", findScript)
	if err != nil {
		return fmt.Errorf("finding sessions: %w", err)
	}

	pgFiles := strings.Fields(out) // Split by whitespace to get individual file paths
	if len(pgFiles) == 0 {
		// No active sessions found - this is not an error, just means everything is stopped
		return nil
	}

	var killErrors []string
	for _, pgFile := range pgFiles {
		pg := readPg(ctx, pgFile)
		if pg == "" {
			continue // Skip if we can't read the PID
		}
		// Kill the process group from inside the guest
		killScript := fmt.Sprintf("kill -TERM -%s 2>/dev/null; sleep 1; kill -KILL -%s 2>/dev/null", shellQuote(pg), shellQuote(pg))
		if _, err := runWslWithInput(ctx, wslArgs("bash"), killScript); err != nil {
			killErrors = append(killErrors, fmt.Sprintf("pg %s: %v", pg, err))
		}
	}

	// Clean up the pgFiles themselves
	cleanupScript := "rm -f /tmp/lapel-*.pg"
	if _, err := runWsl(ctx, "bash", "-c", cleanupScript); err != nil {
		killErrors = append(killErrors, fmt.Sprintf("cleanup: %v", err))
	}

	if len(killErrors) > 0 {
		return fmt.Errorf("stopped session %s with errors: %v", sessionID, killErrors)
	}
	return nil
}

// Destroy removes managed resources for an environment after confirmation.
func (b *DistBackend) Destroy(ctx context.Context, id string) error {
	// --unregister is a GLOBAL flag: it cannot be combined with -d (WSL rejects
	// the combination), so route through runWslGlobal.
	if _, err := runWslGlobal(ctx, "--unregister", id); err != nil {
		return fmt.Errorf("destroying environment %s: %w", id, err)
	}
	return nil
}
