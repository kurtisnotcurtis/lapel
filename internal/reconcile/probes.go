package reconcile

import (
	"context"
	"net/http"
	"strings"
	"time"

	"lapel/internal/backend/wsl"
	"lapel/internal/provider"
)

// listDistros enumerates installed WSL distributions. Defined as a function so
// tests can override it if needed.
var listDistros = wsl.ListDistros

// checkReachable probes an HTTP(S) endpoint and reports reachability plus a
// human-readable note. It first checks from the WSL guest (rewriting
// host.docker.internal/localhost to the resolved WSL gateway IP when available),
// and also checks from the host HTTP client as a fallback.
func checkReachable(baseURL string) (bool, string, error) {
	ctx := context.Background()
	candidates := []string{baseURL}

	if hostIP, err := provider.HostIP(ctx); err == nil && hostIP != "" {
		for _, placeholder := range []string{"host.docker.internal", "127.0.0.1", "localhost"} {
			if strings.Contains(baseURL, placeholder) {
				rewritten := strings.Replace(baseURL, placeholder, hostIP, 1)
				candidates = append([]string{rewritten}, candidates...)
				break
			}
		}
	}

	var lastRes provider.Result
	for _, target := range candidates {
		res := provider.CheckEndpoint(ctx, target, 3*time.Second)
		lastRes = res
		if res.Status == "ok" {
			return true, "reachable (" + res.Detail + ")", nil
		}
	}

	// Host-side HTTP fallback (e.g. when LM Studio is bound to 127.0.0.1 on Windows).
	hostURL := strings.Replace(baseURL, "host.docker.internal", "127.0.0.1", 1)
	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Get(hostURL); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode < 500 {
			return true, "reachable on host (" + hostURL + ")", nil
		}
	}

	switch lastRes.Status {
	case "closed":
		return false, "closed port: " + lastRes.Detail, nil
	case "unreachable":
		return false, "unreachable: " + lastRes.Detail, nil
	default:
		if lastRes.Error != nil {
			return false, "error: " + lastRes.Error.Error(), nil
		}
		return false, "unknown state", nil
	}
}
