package wsl

import (
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	math_rand "math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"lapel/internal/domain"
)

// shellQuote returns a POSIX-safe single-quoted representation of s so that an
// untrusted value cannot be interpreted by the shell (spec 8: no concatenation
// of arbitrary values into command text).
func shellQuote(s string) string {
	// POSIX single-quote escaping: wrap in '...'; a lone ' becomes '\''.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// isRoot reports whether the named distro's default user (the identity
// wsl.exe uses when no -u is given) is root.
func isRoot(ctx context.Context, dist string) bool {
	out, err := runWslWith(ctx, []string{"-d", dist, "id", "-u"})
	return err == nil && strings.TrimSpace(out) == "0"
}

// runScript pipes a bash script into the named distro via stdin (bash reads
// it as its command input) instead of passing it as a -c argument. WSL 3.0.3.0
// mangles $(...) in argv — even inside quotes, command substitutions are
// pre-evaluated or stripped before bash ever sees them (verified 2026-11),
// which silently corrupts scripts; stdin passes through intact.
func runScript(ctx context.Context, dist, script string, asRoot bool) (string, error) {
	args := []string{"-d", dist}
	if asRoot {
		args = append(args, "sudo", "-n")
	}
	return runWslWithInput(ctx, append(args, "bash"), script)
}

// runRoot runs a shell script inside the named distro with root privileges. If
// that distro's default user is already root it runs directly; otherwise it
// escalates via passwordless sudo (lapel-provisioned users get a
// NOPASSWD drop-in at import time). On pre-existing distros without
// passwordless sudo this fails with a hint.
func runRoot(ctx context.Context, dist, script string) (string, error) {
	if isRoot(ctx, dist) {
		return runScript(ctx, dist, script, false)
	}
	out, err := runScript(ctx, dist, script, true)
	if err != nil {
		return out, fmt.Errorf("%w (if this is a pre-existing distro, ensure its default user has passwordless sudo)", err)
	}
	return out, nil
}

// buildScript composes a single safe command line from a working directory, an
// optional env map and the (already-split) command array. The whole result is
// forwarded to `wsl.exe bash -c '<script>'`, which WSL reliably passes through.
func buildScript(workdir string, env map[string]string, cmd []string) string {
	var b strings.Builder
	for k, v := range env {
		fmt.Fprintf(&b, "export %s=%s; ", shellQuote(k), shellQuote(v))
	}
	if workdir != "" {
		fmt.Fprintf(&b, "cd %s && ", shellQuote(workdir))
	}
	for i, a := range cmd {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

// Exec runs a structured command inside the distro. The command and its args
// are each individually quoted and assembled into one script string, so no
// untrusted value is ever interpolated as shell syntax (spec 8). The script
// travels via stdin, not as a -c argument: WSL mangles $(...) in argv
// (verified 2026-11), which would corrupt user commands containing command
// substitutions.
func (b *DistBackend) Exec(ctx context.Context, req domain.ExecRequest) (*domain.ExecResult, error) {
	script := buildScript(req.WorkingDir, req.Env, req.Cmd)
	// NOTE: assign wslArgs to a local first — go1.27 fails to type-check a spread
	// expression nested inside another multi-argument call in some shapes.
	cmdArgs := wslArgs("bash")
	cmd := exec.CommandContext(ctx, "wsl.exe", cmdArgs...)
	setOwnProcessGroup(cmd) // own process group for clean teardown (no-op on Windows)

	var out, errb bytes.Buffer
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = &out
	cmd.Stderr = &errb
	start := time.Now()
	err := cmd.Run()
	res := &domain.ExecResult{
		Stdout:   out.String(),
		Stderr:   errb.String(),
		Duration: time.Since(start),
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = int(ee.ExitCode())
		} else {
			return res, fmt.Errorf("wsl.exe failed to start: %w", err)
		}
	}
	return res, nil
}

// Process is a launched interactive session (one long-lived wsl.exe handle).
type Process struct {
	cmd    *exec.Cmd
	pgid   int    // host-side wsl.exe process-group id (teardown belt)
	pgFile string // guest path holding the setsid leader pid == pgid
	tag    string // unique session tag for diagnostics
	stdin  io.WriteCloser
	done   chan struct{}
	closeW sync.Once
	waitCh chan waitOutcome
}

// waitOutcome carries a session's Wait result and error.
type waitOutcome struct {
	res *domain.ExecResult
	err error
}

// randTag returns a short random identifier used to build per-session files.
func randTag() string {
	b := make([]byte, 6)
	if _, err := crypto_rand.Read(b); err != nil { // crypto/rand; fallback if it ever fails
		math_rand.Seed(time.Now().UnixNano())
		for i := range b {
			b[i] = byte(math_rand.Intn(256))
		}
	}
	return hex.EncodeToString(b)
}

// Launch starts an interactive command in its own process group and streams I/O.
// The guest session leader is setsid'd so its pid == pgid; that id is written to
// a durable file inside the distro. Stop kills that group from INSIDE the guest,
// which reliably tears down descendants even though closing the wsl.exe handle
// alone does not (probe 2).
func (b *DistBackend) Launch(ctx context.Context, req domain.ExecRequest) (*Process, error) {
	tag := randTag()
	pgFile := "/tmp/lapel-" + tag + ".pg"
	scriptPath := "/tmp/lapel-" + tag + ".sh"
	userScript := buildScript(req.WorkingDir, req.Env, req.Cmd)
	// Session leader: record its pid (== pgid), then exec the user command.
	inner := fmt.Sprintf("echo $$ > %s; exec %s", shellQuote(pgFile), shellQuote(userScript))
	// The bootstrap cannot travel as a -c argument (WSL mangles $(...) in argv,
	// verified 2026-11) and stdin is reserved for interactive input, so write
	// it to a file first via a heredoc. The quoted delimiter keeps the content
	// literal.
	writeArgs := wslArgs("bash")
	wcmd := exec.CommandContext(ctx, "wsl.exe", writeArgs...)
	var wout, werrb bytes.Buffer
	wcmd.Stdout = &wout
	wcmd.Stderr = &werrb
	heredoc := fmt.Sprintf("cat > %s <<'TB_%s'\n%s\nTB_%s\n", shellQuote(scriptPath), tag, inner, tag)
	wcmd.Stdin = strings.NewReader(heredoc)
	if err := wcmd.Run(); err != nil {
		return nil, fmt.Errorf("write bootstrap script: %s: %w", sanitize(wout.String()+"\n"+werrb.String()), err)
	}
	// Main session: setsid makes bash the guest session leader (pid == pgid).
	cmdArgs := wslArgs("setsid", "bash", scriptPath)
	cmd := exec.CommandContext(ctx, "wsl.exe", cmdArgs...)
	setOwnProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start wsl.exe: %w", err)
	}

	pgid, err := pgidOf(cmd)
	if err != nil {
		return nil, fmt.Errorf("get process group: %w", err)
	}

	p := &Process{cmd: cmd, pgid: pgid, pgFile: pgFile, tag: tag, stdin: stdin, done: make(chan struct{}), waitCh: make(chan waitOutcome, 1)}
	go func() { io.Copy(os.Stdout, out) }()
	go func() {
		res, err := waitResult(cmd.Wait())
		p.closeW.Do(func() { close(p.done) })
		p.waitCh <- waitOutcome{res: res, err: err}
	}()
	return p, nil
}

// waitResult converts a cmd.Wait outcome into (result, error): exit-code errors
// are folded into ExecResult.ExitCode with a nil error; transport errors return
// a nil result and the wrapped error.
func waitResult(err error) (*domain.ExecResult, error) {
	if err == nil {
		return nil, nil
	}
	res := &domain.ExecResult{}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = int(ee.ExitCode())
		return res, nil
	}
	return res, err
}

// Wait blocks until the process exits and returns its result. It is idempotent:
// the first call caches the outcome so repeated calls (and Stop's internal wait)
// do not race on cmd.Wait.
func (p *Process) Wait() (*domain.ExecResult, error) {
	<-p.done
	o := <-p.waitCh
	return o.res, o.err
}

// Stop terminates the session from INSIDE the guest (kills its process group),
// then closes the host-side wsl.exe handle. Killing the group by the id captured
// at launch reliably removes descendant processes that closing the connection
// alone leaves behind (probe 2). SIGKILL is used as a fallback if TERM stalls.
func (p *Process) Stop() error {
	ctx := context.Background()

	// Definitive guest-side teardown by process-group id written at launch.
	pg := readPg(ctx, p.pgFile)
	if pg != "" {
		killScript := fmt.Sprintf("kill -TERM -%s 2>/dev/null; sleep 1; kill -KILL -%s 2>/dev/null", shellQuote(pg), shellQuote(pg))
		// best-effort; ignore errors (process may already be gone)
		_, err := runWslWithInput(ctx, wslArgs("bash"), killScript)
		if err != nil {
			// Log but don't fail - process might have exited on its own
			fmt.Printf("[WARN] guest-side kill for pg %s: %v\n", pg, err)
		}
	} else {
		// Fallback: try to find and kill any lingering processes
		fmt.Printf("[INFO] No process group file found, attempting fallback cleanup\n")
	}

	// Host-side belt: close the wsl.exe handle.
	if p.cmd.Process != nil {
		_ = killProcessGroup(p.pgid)
		p.closeW.Do(func() { <-p.done })
	}
	return nil
}

// readPg returns the session leader pid (== pgid) captured at launch.
func readPg(ctx context.Context, path string) string {
	out, err := runWsl(ctx, "cat", path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Stdin returns the process stdin writer for interactive input.
func (p *Process) Stdin() io.Writer { return p.stdin }

// PID returns the wsl.exe host pid.
func (p *Process) PID() int { return p.cmd.Process.Pid }

// CountChildren counts guest processes whose command line's first token equals
// pattern exactly. Exact-first-field matching avoids false positives from our own
// wsl.exe handles (whose command lines embed the full script) and is what session
// teardown verification relies on to confirm no orphans remain (probe 2).
func CountChildren(ctx context.Context, pattern string) (int, error) {
	script := fmt.Sprintf("ps -eo args= | awk '$1==%s {c++} END{print c+0}'", shellQuote(pattern))
	out, err := runWslWithInput(ctx, wslArgs("bash"), script)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range strings.TrimSpace(out) {
		if r < '0' || r > '9' {
			break
		}
		count = count*10 + int(r-'0')
	}
	return count, nil
}
