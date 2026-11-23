// Package workspace provides Win/Lin path resolution and workspace import logic
// for lapel. It handles the conversion between Windows host paths (e.g.,
// C:\Users\user\projects) and WSL2 guest paths (e.g., /mnt/c/Users/user/projects
// or /home/agent/workspace), as well as the actual import process using rsync -a
// for copy mode or symlink/bind links for bind mode.
package workspace

import (
	"fmt"
	"strings"
)

const (
	// MntPrefix is the standard WSL2 mount point for Windows drives.
	MntPrefix = "/mnt/"

	// HomeDir is the default home directory prefix in WSL2 distros.
	HomeDir = "/home"

	// DefaultImportTarget is where workspaces are mounted/imported under /home/<user>/.
	DefaultImportTarget = "workspace"
)

// PathResolver converts between Windows and Linux paths. It is stateless and
// OS-independent (does not rely on filepath.ToSlash/FromSlash which are no-ops
// off Windows).
type PathResolver struct{}

// NewPathResolver creates a new path resolver.
func NewPathResolver() *PathResolver {
	return &PathResolver{}
}

// HostToGuest converts a Windows path to its WSL2 equivalent.
//
// Examples:
//   - "C:\Users\user\projects" → "/mnt/c/Users/user/projects"
//   - "D:\data\repo.git"       → "/mnt/d/data/repo.git"
//   - "/home/agent/workspace"  → "/home/agent/workspace" (passthrough for already-Linux paths)
func (r *PathResolver) HostToGuest(hostPath string) (string, error) {
	if isLinuxPath(hostPath) {
		return hostPath, nil
	}

	// Normalize backslashes unconditionally so this works identically on macOS/Linux/Windows.
	normalized := strings.ReplaceAll(hostPath, `\`, `/`)

	parts := strings.SplitN(normalized, ":", 2)
	if len(parts) != 2 || len(parts[0]) != 1 {
		return "", fmt.Errorf("not a Windows path: %s", hostPath)
	}

	drive := strings.ToLower(parts[0])
	rest := parts[1]
	if rest == "" || rest == "/" {
		return fmt.Sprintf("%s%s/", MntPrefix, drive), nil
	}

	return fmt.Sprintf("%s%s/%s", MntPrefix, drive, strings.TrimLeft(rest, "/")), nil
}

// GuestToHost converts a WSL2 /mnt/<drive>/... path back to its Windows equivalent.
//
// Examples:
//   - "/mnt/c/Users/user/projects" → `C:\Users\user\projects`
//   - "/home/agent/workspace"      → "" (not a host path, returns empty)
func (r *PathResolver) GuestToHost(guestPath string) (string, error) {
	if !strings.HasPrefix(guestPath, MntPrefix) {
		return "", nil
	}

	parts := strings.SplitN(guestPath[len(MntPrefix):], "/", 2)
	if len(parts) != 2 || len(parts[0]) != 1 {
		return "", fmt.Errorf("not a /mnt/X/... path: %s", guestPath)
	}

	drive := strings.ToUpper(parts[0])
	rest := strings.ReplaceAll(parts[1], `/`, `\`)
	if rest == "" {
		rest = `\`
	} else if !strings.HasPrefix(rest, `\`) {
		rest = `\` + rest
	}

	return drive + ":" + rest, nil
}

// GuestHomePath returns the default import target under /home/<user>/<name>.
func GuestHomePath(user, name string) string {
	return fmt.Sprintf("%s/%s/%s", HomeDir, user, name)
}

// isLinuxPath returns true if the path looks like a Linux absolute path.
func isLinuxPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, MntPrefix)
}

// Normalize ensures a path uses forward slashes and has no trailing separators
// (except for root). Works identically across Windows, Linux, and macOS.
func Normalize(p string) string {
	p = strings.ReplaceAll(p, `\`, `/`)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
}
