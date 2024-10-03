package wsl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"lapel/internal/domain"
)

// TestExecStructuredArgs validates the non-interactive Exec path: a structured
// command array is passed positionally and returns the correct exit code.
func TestExecStructuredArgs(t *testing.T) {
	ctx := context.Background()
	b := &DistBackend{}

	res, err := b.Exec(ctx, domain.ExecRequest{
		Dist:       Dist,
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

// TestProcessTeardownNoOrphans re-validates probe 2 in Go: after Stop(), the
// launched session's processes must be gone from the guest. The process is
// tagged via argv[0] so the orphan check matches only our own processes.
func TestProcessTeardownNoOrphans(t *testing.T) {
	ctx := context.Background()
	b := &DistBackend{}
	tag := fmt.Sprintf("lapel-test-%d", time.Now().UnixNano())

	req := domain.ExecRequest{
		Dist:       Dist,
		WorkingDir: "/tmp",
		Cmd:        []string{"bash", "-c", fmt.Sprintf("exec -a '%s' sleep 30", tag)},
	}
	p, err := b.Launch(ctx, req)
	if err != nil {
		t.Fatalf("launch failed: %v", err)
	}
	time.Sleep(800 * time.Millisecond) // allow the process to come up

	if err := p.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Logf("wait reported: %v", err)
	}

	// Poll briefly: teardown is asynchronous and may take a moment.
	for i := 0; i < 10; i++ {
		leftover, err := CountChildren(ctx, tag)
		if err == nil && leftover == 0 {
			t.Logf("teardown confirmed: no orphaned processes after Stop()")
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	leftover, _ := CountChildren(ctx, tag)
	if leftover == 0 {
		t.Logf("teardown confirmed (eventual): no orphaned processes after Stop()")
		return
	}
	t.Fatalf("expected 0 orphaned processes after Stop(), found %d", leftover)
}
