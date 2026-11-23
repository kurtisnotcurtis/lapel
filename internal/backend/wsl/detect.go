// Package wsl implements the WSL2 execution backend: distro discovery,
// user provisioning, bootstrap, command execution, and lifecycle.
package wsl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"lapel/internal/domain"
)

// DefaultDist is the default WSL distribution name managed by lapel.
const DefaultDist = "lapel"

func wslArgsFor(dist string, args ...string) []string {
	if strings.TrimSpace(dist) == "" {
		dist = DefaultDist
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-d", dist)
	out = append(out, args...)
	return out
}

// runWslGlobal executes `wsl.exe <args...>` WITHOUT the `-d <dist>` prefix.
// Global management flags (--import, --unregister, ...) cannot be combined
// with -d: WSL rejects them ("Invalid command line argument") or mis-parses
// the arguments.
func runWslGlobal(ctx context.Context, args ...string) (string, error) {
	return runWslWith(ctx, args)
}

// runWslWith runs wsl.exe with an explicit argument list and returns trimmed,
// sanitized stdout.
func runWslWith(ctx context.Context, cmdArgs []string) (string, error) {
	return runWslWithInput(ctx, cmdArgs, "")
}

// runWslWithInput is like runWslWith but pipes stdinText into wsl.exe's stdin.
// Use it for bash scripts instead of passing them as -c arguments: WSL 3.0.3.0
// mangles unquoted $(...) in argv — command substitutions arrive empty and
// silently break scripts — while stdin passes through intact.
func runWslWithInput(ctx context.Context, cmdArgs []string, stdinText string) (string, error) {
	return runWslWithInputStream(ctx, cmdArgs, stdinText, nil)
}

// runWslWithInputStream behaves like runWslWithInput and optionally streams
// sanitized, line-prefixed stdout/stderr output to stream as the command runs.
func runWslWithInputStream(ctx context.Context, cmdArgs []string, stdinText string, stream io.Writer) (string, error) {
	restoreConsole := preserveConsoleMode()
	defer restoreConsole()

	cmd := exec.CommandContext(ctx, "wsl.exe", cmdArgs...)
	var out, errb bytes.Buffer
	var pw *linePrefixWriter
	if stream != nil {
		pw = newLinePrefixWriter(stream, "    ")
		cmd.Stdout = io.MultiWriter(&out, pw)
		cmd.Stderr = io.MultiWriter(&errb, pw)
	} else {
		cmd.Stdout = &out
		cmd.Stderr = &errb
	}
	if stdinText != "" {
		cmd.Stdin = strings.NewReader(stdinText)
	}
	err := cmd.Run()
	if pw != nil {
		pw.Flush()
	}
	if err != nil {
		detail := strings.TrimSpace(sanitize(out.String() + "\n" + errb.String()))
		return sanitize(strings.TrimSpace(out.String())), fmt.Errorf("%s: %w", detail, err)
	}
	return sanitize(out.String()), nil
}

// linePrefixWriter buffers streamed command output, strips UTF-16LE NUL bytes
// and carriage returns, and writes complete lines prefixed with prefix.
type linePrefixWriter struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
	eol    string
	buf    []byte
}

func newLinePrefixWriter(w io.Writer, prefix string) *linePrefixWriter {
	eol := "\n"
	if f, ok := w.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
			// Use CRLF on interactive terminals so lines return to column 0 even
			// while wsl.exe temporarily mutates console newline handling.
			eol = "\r\n"
		}
	}
	return &linePrefixWriter{w: w, prefix: prefix, eol: eol}
}

func (p *linePrefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ch := range b {
		if ch == 0 || ch == '\r' {
			continue
		}
		if ch == '\n' {
			p.emitLineLocked()
			continue
		}
		p.buf = append(p.buf, ch)
	}
	return len(b), nil
}

// Flush writes any remaining buffered bytes as a final line.
func (p *linePrefixWriter) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buf) > 0 {
		p.emitLineLocked()
	}
}

func (p *linePrefixWriter) emitLineLocked() {
	line := string(p.buf)
	p.buf = p.buf[:0]
	if strings.TrimSpace(line) == "" {
		return
	}
	eol := p.eol
	if eol == "" {
		eol = "\n"
	}
	fmt.Fprintf(p.w, "%s%s%s", p.prefix, line, eol)
}

// sanitize removes NUL bytes that wsl.exe emits with UTF-16LE encoding when its
// stdout is a pipe rather than a console (e.g. `wsl.exe --list --verbose`).
func sanitize(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

// ListDistros enumerates installed distributions.
func ListDistros(ctx context.Context) ([]string, error) {
	out, err := runWslGlobal(ctx, "--list", "--verbose")
	if err != nil {
		return nil, err
	}
	return parseDistroNames(out), nil
}

// parseDistroNames extracts distro names from `wsl --list --verbose` output.
// Columns are NAME STATE VERSION; the name may contain spaces, so it is
// everything except the last two fields. The active distro is prefixed with '*'.
func parseDistroNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] == "NAME" {
			continue // skip blank lines and header
		}
		name := strings.Join(fields[:len(fields)-2], " ")
		name = strings.TrimSpace(strings.TrimPrefix(name, "*"))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// OSRelease parses /etc/os-release for the given distro.
func OSRelease(ctx context.Context, dist string) (map[string]string, error) {
	out, err := runWslWith(ctx, wslArgsFor(dist, "cat", "/etc/os-release"))
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	return m, nil
}

// Detect reports whether WSL2 and the backend's target distro are available.
// Available is true whenever wsl.exe is installed and operational on the host;
// if the target distro has not been imported yet, Note explains that
// provisioning is required so `lapel setup` and `lapel apply` can create it.
func (d *DistBackend) Detect(ctx context.Context) (domain.BackendStatus, error) {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return domain.BackendStatus{Available: false}, fmt.Errorf("wsl.exe not found on PATH: %w", err)
	}
	target := d.dist()
	version := detectWSLVersion(ctx)
	names, err := ListDistros(ctx)
	if err != nil && version == "" {
		return domain.BackendStatus{}, fmt.Errorf("listing distros: %w", err)
	}
	status := domain.BackendStatus{
		Name:      "wsl",
		Available: true,
		Version:   version,
	}
	distroFound := false
	for _, n := range names {
		if strings.EqualFold(n, target) {
			distroFound = true
			break
		}
	}
	if !distroFound {
		status.Note = fmt.Sprintf("distro %q not yet provisioned (run 'lapel setup' or 'lapel provision')", target)
	}
	return status, nil
}

// detectWSLVersion parses `wsl.exe --version` output to extract the WSL version string.
func detectWSLVersion(ctx context.Context) string {
	out, err := runWslWith(ctx, []string{"--version"})
	if err != nil {
		return ""
	}
	return parseWSLVersionField(out, "wsl version:")
}

// parseWSLVersionField extracts the value after a "<prefix>:" line from
// `wsl.exe --version` output.
func parseWSLVersionField(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), strings.ToLower(prefix)) {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// Inspect returns the target distro's runtime state.
func (d *DistBackend) Inspect(ctx context.Context) (domain.EnvironmentStatus, error) {
	target := d.dist()
	names, err := ListDistros(ctx)
	if err != nil {
		return domain.EnvironmentStatus{}, err
	}
	st := domain.EnvironmentStatus{}
	for _, n := range names {
		if strings.EqualFold(n, target) {
			st.Exists = true
			st.Distribution = n
			break
		}
	}
	if st.Exists {
		meta, _ := OSRelease(ctx, st.Distribution)
		st.Metadata = meta
		if v, ok := meta["VERSION_ID"]; ok {
			st.Version = v
		}
	}
	return st, nil
}
