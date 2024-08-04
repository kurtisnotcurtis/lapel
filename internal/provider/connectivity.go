// Package provider resolves and tests inference endpoints from inside WSL.
// Endpoint resolution belongs here (spec 3.3), not in every harness adapter.
package provider

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Result is the outcome of an endpoint reachability check.
type Result struct {
	Status    string // "ok" | "closed" | "unreachable" | "error"
	Detail    string
	Error     error
}

const (
	statusOK          = "ok"
	statusClosed      = "closed"      // host up, port closed (e.g. loopback-bound LM Studio)
	statusUnreachable = "unreachable" // routing/timeout
	statusError       = "error"
)

// HostIP returns the Windows host IP as seen from WSL, resolved from the
// default route in /proc/net/route (probe 1). Returns error if not found.
func HostIP(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "wsl.exe", "-d", distro(), "cat", "/proc/net/route").Output()
	if err != nil {
		return "", fmt.Errorf("read /proc/net/route: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		// Skip header (first data row). Columns are whitespace-separated.
		if i == 0 || len(fields) < 3 {
			continue
		}
		if fields[1] == "00000000" { // default route destination
			gwHex := fields[2]
			if len(gwHex) == 8 {
				return decodeIPv4LE(gwHex), nil
			}
		}
	}
	return "", fmt.Errorf("default route gateway not found in /proc/net/route")
}

// decodeIPv4LE decodes a little-endian hex gateway (WSL stores it reversed).
func decodeIPv4LE(hex string) string {
	if len(hex) != 8 {
		return "0.0.0.0"
	}
	var b [4]byte
	for i := 0; i < 4; i++ {
		pair := hex[i*2 : i*2+2]
		v, err := strconv.ParseUint(pair, 16, 32)
		if err != nil {
			return "0.0.0.0"
		}
		b[i] = byte(v) // little-endian: first pair is the least significant byte
	}
	// Reverse for big-endian display.
	return fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0])
}

// CheckEndpoint runs a reachability probe against baseURL from inside WSL and
// classifies the outcome so diagnostics can distinguish networking, auth and
// model errors (spec 3.3). It probes the LAN IP bound to the distro.
func CheckEndpoint(ctx context.Context, baseURL string, timeout time.Duration) Result {
	return classifyProbe(probeFromWSL(ctx, baseURL, timeout), false)
}

// CheckLocalhost probes an inference endpoint bound to localhost inside WSL.
// Under WSL2 mirrored networking (networkingMode=mirrored) with localhostforwarding,
// localhost within WSL is mapped to the Windows host's localhost, so this reaches
// an LM Studio / Ollama instance running on the host via a different path than
// probing the host's LAN IP directly. It re-validates reachability under that
// mapping and classifies results like CheckEndpoint.
func CheckLocalhost(ctx context.Context, port int, timeout time.Duration) Result {
	return classifyProbe(probeFromWSL(ctx, fmt.Sprintf("http://localhost:%d", port), timeout), true)
}

// probeResult is the raw outcome of running curl inside the distro.
type probeResult struct {
	code string // trimmed HTTP status / curl message on success
	msg  string // full captured stderr/stdout (lowercased) for error classification
	err  error // non-nil when exec/cml.Run() failed
}

// probeFromWSL runs a minimal curl against baseURL inside the distro and returns
// its raw outcome. Shared by CheckEndpoint (LAN IP) and CheckLocalhost (mirrored
// localhost). A path without a scheme is treated as a /v1/models request.
func probeFromWSL(ctx context.Context, baseURL string, timeout time.Duration) probeResult {
	path := baseURL
	if !strings.Contains(path, "/") {
		path = strings.TrimRight(path, "/") + "/v1/models"
	}
	script := fmt.Sprintf("curl -sS -m %d -o /dev/null -w '%%{http_code}' '%s'", int(timeout.Seconds()), path)
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro(), "bash", "-c", script)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	msg := strings.ToLower(out.String())
	if err != nil {
		return probeResult{msg: msg, err: err}
	}
	return probeResult{code: strings.TrimSpace(msg), msg: msg}
}

// classifyEndpoint builds a Result from a probeResult using LAN-IP guidance.
func classifyEndpoint(r probeResult) Result {
	switch {
	case r.code != "" && (r.code == "200" || r.code == "404" || r.code == "401"):
		return Result{Status: statusOK, Detail: fmt.Sprintf("endpoint responded (http %s)", r.code)}
	case strings.Contains(r.msg, "could not connect") || strings.Contains(r.msg, "refused"):
		return Result{Status: statusClosed, Detail: "host reachable but port closed; if LM Studio is running it may be bound to 127.0.0.1 only (enable 'Allow connections from IP addresses')"}
	case strings.Contains(r.msg, "timed out"):
		return Result{Status: statusUnreachable, Detail: "connection timed out; check WSL<->host routing and firewall"}
	default:
		detail := r.msg
		if detail == "" && r.err != nil {
			detail = r.err.Error()
		}
		return Result{Status: statusError, Error: fmt.Errorf("probe failed: %v", r.err), Detail: detail}
	}
}

// classifyLocalhost mirrors classifyEndpoint but with mirrored-localhost guidance.
func classifyLocalhost(r probeResult) Result {
	switch {
	case r.code != "" && (r.code == "200" || r.code == "404" || r.code == "401"):
		return Result{Status: statusOK, Detail: fmt.Sprintf("reachable via mirrored localhost mapping (http %s)", r.code)}
	case strings.Contains(r.msg, "could not connect") || strings.Contains(r.msg, "refused"):
		return Result{Status: statusClosed, Detail: "host localhost reachable but port closed; LM Studio/Ollama not listening here (ensure it binds host localhost and mirrored networking is on)"}
	case strings.Contains(r.msg, "timed out"):
		return Result{Status: statusUnreachable, Detail: "connection timed out via localhost; verify networkingMode=mirrored + localhostforwarding=true in .wslconfig"}
	default:
		detail := r.msg
		if detail == "" && r.err != nil {
			detail = r.err.Error()
		}
		return Result{Status: statusError, Error: fmt.Errorf("probe failed: %v", r.err), Detail: detail}
	}
}

// classifyProbe is the shared entry point used by CheckEndpoint and
// CheckLocalhost; it selects LAN-IP vs mirrored-localhost guidance.
func classifyProbe(r probeResult, localhost bool) Result {
	if localhost {
		return classifyLocalhost(r)
	}
	return classifyEndpoint(r)
}

func distro() string { return "Ubuntu-Lean" }
