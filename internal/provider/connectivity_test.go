package provider

import (
	"context"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeIPv4LE(t *testing.T) {
	if got := decodeIPv4LE("0104A8C0"); got != "192.168.4.1" {
		t.Errorf("decodeIPv4LE(0104A8C0) = %q, want 192.168.4.1", got)
	}
	if got := decodeIPv4LE("01101DAC"); got != "172.29.16.1" {
		t.Errorf("decodeIPv4LE(01101DAC) = %q, want 172.29.16.1", got)
	}
	if got := decodeIPv4LE("bad"); got != "0.0.0.0" {
		t.Errorf("decodeIPv4LE(bad) = %q, want 0.0.0.0", got)
	}
}

func TestBuildProbeCurlArgsAvoidsShellInterpolation(t *testing.T) {
	malicious := "http://127.0.0.1:1234/v1'; rm -rf / #"
	got := buildProbeCurlArgs("my-distro", malicious, 5*time.Second)
	want := []string{
		"-d", "my-distro",
		"--", "curl",
		"-sS",
		"-m", "5",
		"-o", "/dev/null",
		"-w", "%{http_code}",
		"--", malicious,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildProbeCurlArgs =\n  %#v\nwant\n  %#v", got, want)
	}
}

func TestClassifyEndpoint(t *testing.T) {
	if r := classifyEndpoint(probeResult{code: "200"}); r.Status != statusOK {
		t.Errorf("expected ok, got %+v", r)
	}
	if r := classifyEndpoint(probeResult{msg: "curl: (7) failed to connect: connection refused"}); r.Status != statusClosed {
		t.Errorf("expected closed, got %+v", r)
	}
	if r := classifyEndpoint(probeResult{msg: "curl: (28) operation timed out"}); r.Status != statusUnreachable {
		t.Errorf("expected unreachable, got %+v", r)
	}
}

// TestHostIPResolvesFromWSL validates endpoint-resolution against a live WSL distro.
// Set LAPEL_LIVE_WSL=1 on a Windows host with a provisioned lapel distro to run it.
func TestHostIPResolvesFromWSL(t *testing.T) {
	if os.Getenv("LAPEL_LIVE_WSL") != "1" {
		t.Skip("set LAPEL_LIVE_WSL=1 to run live WSL provider connectivity tests")
	}

	ctx := context.Background()
	ip, err := HostIP(ctx)
	if err != nil {
		t.Fatalf("host IP resolution failed (probe 1): %v", err)
	}
	if net.ParseIP(ip) == nil {
		t.Fatalf("expected a valid IPv4 host address, got %q", ip)
	}

	out, err := exec.Command("wsl.exe", "-d", resolveDist(), "ip", "route", "show", "default").Output()
	if err != nil {
		t.Fatalf("reading default route: %v", err)
	}
	fields := strings.Fields(string(out))
	var gw string
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			gw = fields[i+1]
			break
		}
	}
	if gw == "" {
		t.Fatalf("no gateway in default route output: %q", string(out))
	}
	if ip != gw {
		t.Fatalf("HostIP() = %q but default route gateway is %q (hex parser bug?)", ip, gw)
	}
	t.Logf("resolved WSL->host IP: %s", ip)
}

// TestLMStudioReachableFromWSL probes the live inference endpoint from inside WSL.
func TestLMStudioReachableFromWSL(t *testing.T) {
	if os.Getenv("LAPEL_LIVE_WSL") != "1" {
		t.Skip("set LAPEL_LIVE_WSL=1 to run live WSL provider connectivity tests")
	}

	ctx := context.Background()
	host, err := HostIP(ctx)
	if err != nil {
		t.Skipf("host IP resolution failed; skipping live probe: %v", err)
	}
	res := CheckEndpoint(ctx, "http://"+host+":1234", 8*time.Second)
	switch res.Status {
	case statusOK, statusClosed, statusUnreachable:
		t.Logf("LM Studio probe from WSL classified %s: %s", res.Status, res.Detail)
	default:
		t.Fatalf("unexpected classification %q (detail=%q): our probe mechanism failed", res.Status, res.Detail)
	}
}
