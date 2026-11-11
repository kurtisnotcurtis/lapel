package config

import (
	"os"
	"strings"
)

// fileExists reports whether path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// allowedTopLevelKey reports whether key is a known manifest top-level field,
// used to surface typos instead of silently ignoring them.
func allowedTopLevelKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "apiversion", "name", "environment", "provider", "agent", "workspace", "jail":
		return true
	default:
		return false
	}
}
