package reconcile

import (
	"context"
	"time"

	"lapel/internal/backend/wsl"
	"lapel/internal/provider"
)

// listDistros enumerates installed WSL distributions. Defined as a function so
// tests can override it if needed.
var listDistros = wsl.ListDistros

// checkReachable probes an HTTP(S) endpoint and reports reachability plus a
// human-readable note. The status literals mirror connectivity's internal
// constants (see provider/connectivity.go).
func checkReachable(baseURL string) (bool, string, error) {
	res := provider.CheckEndpoint(context.Background(), baseURL, 3*time.Second)
	switch res.Status {
	case "ok":
		return true, "reachable (" + res.Detail + ")", nil
	case "closed":
		return false, "closed port: " + res.Detail, nil
	case "unreachable":
		return false, "unreachable: " + res.Detail, nil
	default:
		if res.Error != nil {
			return false, "error: " + res.Error.Error(), nil
		}
		return false, "unknown state", nil
	}
}
