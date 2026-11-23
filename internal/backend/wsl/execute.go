package wsl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"lapel/internal/domain"
)

// shellQuote returns a POSIX-safe single-quoted representation of s so that an
// untrusted value cannot be interpreted by the shell.
func shellQuote(s string) string {
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
// mangles $(...) in argv, whereas stdin passes through intact.
func runScript(ctx context.Context, dist, script string, asRoot bool) (string, error) {
	return runScriptStream(ctx, dist, script, asRoot, nil)
}

func runScriptStream(ctx context.Context, dist, script string, asRoot bool, stream io.Writer) (string, error) {
	args := []string{"-d", dist}
	if asRoot {
		args = append(args, "sudo", "-n")
	}
	return runWslWithInputStream(ctx, append(args, "bash"), script, stream)
}

// runRoot runs a shell script inside the named distro with root privileges. If
// that distro's default user is already root it runs directly; otherwise it
// escalates via passwordless sudo.
func runRoot(ctx context.Context, dist, script string) (string, error) {
	return runRootStream(ctx, dist, script, nil)
}

// runRootStream behaves like runRoot while streaming sanitized output to stream.
func runRootStream(ctx context.Context, dist, script string, stream io.Writer) (string, error) {
	if isRoot(ctx, dist) {
		return runScriptStream(ctx, dist, script, false, stream)
	}
	out, err := runScriptStream(ctx, dist, script, true, stream)
	if err != nil {
		return out, fmt.Errorf("%w (if this is a pre-existing distro, ensure its default user has passwordless sudo)", err)
	}
	return out, nil
}

// buildScript composes a single safe command line from a working directory, an
// optional env map and the (already-split) command array. The whole result is
// forwarded to `wsl.exe bash` via stdin, which WSL reliably passes through.
func buildScript(workdir string, env map[string]string, cmd []string) string {
	var b strings.Builder
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s; ", shellQuote(k), shellQuote(env[k]))
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

// buildInteractiveWSLArgs constructs the `wsl.exe` argv for an interactive
// session that inherits the user's terminal (stdin/stdout/stderr), applies the
// requested distro and Linux user (`-u`), sets environment variables via
// `env(1)`, changes into the requested working directory (falling back to
// $HOME if unavailable), and execs the target command cleanly.
func buildInteractiveWSLArgs(defaultDist string, req domain.ExecRequest) ([]string, error) {
	dist := strings.TrimSpace(req.Dist)
	if dist == "" {
		dist = strings.TrimSpace(defaultDist)
	}
	if dist == "" {
		dist = DefaultDist
	}

	args := []string{"-d", dist}
	if u := strings.TrimSpace(req.User); u != "" {
		if err := validateUsername(u); err != nil {
			return nil, fmt.Errorf("invalid session user: %w", err)
		}
		args = append(args, "-u", u)
	}
	args = append(args, "--")

	// Inject environment variables via env(1) with sorted keys for determinism.
	if len(req.Env) > 0 {
		keys := make([]string, 0, len(req.Env))
		for k := range req.Env {
			if strings.ContainsRune(k, '=') || k == "" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			args = append(args, "env")
			for _, k := range keys {
				args = append(args, k+"="+req.Env[k])
			}
		}
	}

	cmd := req.Cmd
	if len(cmd) == 0 {
		cmd = []string{"bash", "-l"}
	}

	if wd := strings.TrimSpace(req.WorkingDir); wd != "" {
		// Pass workdir and command positionally to sh -c so no untrusted value
		// is ever concatenated into shell syntax and no $(...) appears in argv.
		args = append(args, "sh", "-c", `cd "$1" 2>/dev/null || cd "$HOME" 2>/dev/null || true; shift; exec "$@"`, "_", wd)
		args = append(args, cmd...)
	} else {
		args = append(args, cmd...)
	}

	return args, nil
}

// RunInteractive runs a command inside WSL attached directly to the current
// terminal (os.Stdin, os.Stdout, os.Stderr), returning the child process's
// exit code. A non-zero child exit code is returned as (exitCode, nil); only
// launch/transport failures return a non-nil error.
func (b *DistBackend) RunInteractive(ctx context.Context, req domain.ExecRequest) (int, error) {
	wslArgs, err := buildInteractiveWSLArgs(b.dist(), req)
	if err != nil {
		return 1, err
	}

	cmd := exec.CommandContext(ctx, "wsl.exe", wslArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return 1, fmt.Errorf("wsl.exe failed: %w", err)
	}
	return 0, nil
}

// Exec runs a structured non-interactive command inside the distro. The command
// and its args are each individually quoted and assembled into one script string
// forwarded via stdin so WSL cannot mangle $(...) in argv.
func (b *DistBackend) Exec(ctx context.Context, req domain.ExecRequest) (*domain.ExecResult, error) {
	dist := strings.TrimSpace(req.Dist)
	if dist == "" {
		dist = b.dist()
	}

	var cmdArgs []string
	cmdArgs = append(cmdArgs, "-d", dist)
	if u := strings.TrimSpace(req.User); u != "" {
		if err := validateUsername(u); err != nil {
			return nil, fmt.Errorf("invalid exec user: %w", err)
		}
		cmdArgs = append(cmdArgs, "-u", u)
	}
	cmdArgs = append(cmdArgs, "bash")

	script := buildScript(req.WorkingDir, req.Env, req.Cmd)
	cmd := exec.CommandContext(ctx, "wsl.exe", cmdArgs...)

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
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			return res, fmt.Errorf("wsl.exe failed to start: %w", err)
		}
	}
	return res, nil
}
