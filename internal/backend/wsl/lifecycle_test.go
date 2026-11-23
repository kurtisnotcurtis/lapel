package wsl

import (
	"context"
	"os"
	"testing"

	"lapel/internal/domain"
)

// TestExecStructuredArgs validates the non-interactive Exec path against a live
// WSL distribution. Set LAPEL_LIVE_WSL=1 on a Windows host with a provisioned
// lapel distro to run it.
func TestExecStructuredArgs(t *testing.T) {
	if os.Getenv("LAPEL_LIVE_WSL") != "1" {
		t.Skip("set LAPEL_LIVE_WSL=1 to run live WSL execution tests")
	}

	ctx := context.Background()
	b := NewBackend(DefaultDist)

	res, err := b.Exec(ctx, domain.ExecRequest{
		Dist:       DefaultDist,
		WorkingDir: "/tmp",
		Cmd:        []string{"bash", "-c", "echo hello; exit 7"},
	})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if res.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if res.Stdout == "" {
		t.Errorf("expected stdout 'hello', got empty")
	}
}
