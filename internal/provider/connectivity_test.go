package provider

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestHostIPResolvesFromWSL validates probe 1's endpoint-resolution mechanism:
// the host IP is discovered from WSL's routing table, not hardcoded. The WSL
// NAT subnet is machine-specific (e.g. 192.168.4.x or 172.29.x depending on
// .wslconfig), so instead of asserting a fixed value we cross-check HostIP's
// little-endian hex decode against an independent decimal parse of
// `ip route show default`. A byte-order bug (e.g. "01.00.1D.AC" -> 1.0.29.172)
// would still be caught, because it produces a valid-but-wrong address.
func TestHostIPResolvesFromWSL(t *testing.T) {
	ctx := context.Background()
	ip, err := HostIP(ctx)
	if err != nil {
		t.Fatalf("host IP resolution failed (probe 1): %v", err)
	}
	if net.ParseIP(ip) == nil {
		t.Fatalf("expected a valid IPv4 host address, got %q", ip)
	}

	out, err := exec.Command("wsl.exe", "-d", distro(), "ip", "route", "show", "default").Output()
	if err != nil {
		t.Fatalf("reading default route: %v", err)
	}
	fields := strings.Fields(string(out)) // "default via <gw> dev eth0 ..."
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

// TestLMStudioReachableFromWSL probes the live inference endpoint from inside
// WSL and asserts our probe classifies it into a recognized state. It does not
// require "ok": LM Studio is often bound to 127.0.0.1 only, which surfaces as
// "closed" (spec statusClosed). A classification other than ok/closed/
// unreachable means our own probing failed and must be surfaced.
func TestLMStudioReachableFromWSL(t *testing.T) {
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
