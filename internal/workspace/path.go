// Package workspace provides Win/Lin path resolution and workspace import logic
// for lapel. It handles the conversion between Windows host paths (e.g.,
// C:\Users\kurt\projects) and WSL2 guest paths (e.g., /mnt/c/Users/kurt/projects
// or /home/agent/workspace), as well as the actual import process using rsync -a
// for copy mode or bind mounts for bind mode.
//
// The path resolution layer is identity-independent of the WSL distro name,
// meaning it works regardless of whether the distro is called "Ubuntu-Lean",
// "Ubuntu-24.04", or anything else. This is critical because distro names can
// change during provisioning.
package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// MntPrefix is the standard WSL2 mount point for Windows drives.
	MntPrefix = "/mnt/"

	// HomeDir is the default home directory prefix in WSL2 distros.
	HomeDir = "/home"

	// DefaultImportTarget is where copy-mode workspaces are imported to by default.
	DefaultImportTarget = "workspace"
)

// PathResolver converts between Windows and Linux paths. It's stateless and
// safe for concurrent use.
type PathResolver struct {
	// hostDriveLetter is the lowercase drive letter (e.g., "c") extracted from
	// the first path conversion, used as a fallback convention.
	hostDriveLetter string
}

// NewPathResolver creates a new resolver. The drive letter is inferred lazily
// on first use; callers can also pin it explicitly via SetHostDriveLetter.
func NewPathResolver() *PathResolver {
	return &PathResolver{}
}

// SetHostDriveLetter pins the host drive letter to a specific value (e.g., "c").
// This is useful when the caller knows the convention ahead of time.
func (r *PathResolver) SetHostDriveLetter(letter string) {
	r.hostDriveLetter = strings.ToLower(strings.TrimSpace(letter))
	if len(r.hostDriveLetter) == 1 {
		r.hostDriveLetter = r.hostDriveLetter[:1]
	}
}

// HostToGuest converts a Windows path to its WSL2 equivalent.
//
// Examples:
//   - "C:\Users\kurt\projects" → "/mnt/c/Users/kurt/projects"
//   - "D:\\data\\repo.git"     → "/mnt/d/data/repo.git"
//   - "/home/agent/workspace"  → "/home/agent/workspace" (passthrough for already-Linux paths)
func (r *PathResolver) HostToGuest(hostPath string) (string, error) {
	if isLinuxPath(hostPath) {
		return hostPath, nil
	}

	// Normalize to forward slashes first.
	hostPath = filepath.ToSlash(hostPath)

	// Split drive letter from the rest.
	parts := strings.SplitN(hostPath, ":", 2)
	if len(parts) != 2 || len(parts[0]) != 1 {
		return "", fmt.Errorf("not a Windows path: %s", hostPath)
	}

	drive := strings.ToLower(parts[0])
	rest := parts[1]
	if rest == "" || rest == "\\" || rest == "/" {
		rest = "/"
	}
	// Normalize backslashes to forward slashes.
	rest = filepath.ToSlash(rest)

	return fmt.Sprintf("%s%s/%s", MntPrefix, drive, strings.TrimLeft(rest, "/")), nil
}

// GuestToHost converts a WSL2 path back to its Windows equivalent.
//
// Examples:
//   - "/mnt/c/Users/kurt/projects" → "C:\\Users\\kurt\\projects"
//   - "/home/agent/workspace"      → "" (not a host path, returns empty)
func (r *PathResolver) GuestToHost(guestPath string) (string, error) {
	if !strings.HasPrefix(guestPath, MntPrefix) {
		return "", nil // not a Windows mount path
	}

	parts := strings.SplitN(guestPath[len(MntPrefix):], "/", 2)
	if len(parts) != 2 || len(parts[0]) != 1 {
		return "", fmt.Errorf("not a /mnt/X/... path: %s", guestPath)
	}

	drive := strings.ToUpper(parts[0])
	rest := parts[1]
	if rest == "" {
		rest = "\\"
	} else if !strings.HasPrefix(rest, "\\") {
		// Ensure leading backslash for Windows convention.
		rest = "\\" + rest
	}
	// Normalize forward slashes to backslashes for Windows convention.
	rest = filepath.FromSlash(rest)

	return drive + ":" + rest, nil
}

// GuestHomePath returns the default import target under /home/<user>/<name>.
// This is where copy-mode workspaces live in WSL2 native storage for optimal
// performance (no cross-filesystem I/O penalties).
func GuestHomePath(user, name string) string {
	return fmt.Sprintf("%s/%s/%s", HomeDir, user, name)
}

// isLinuxPath returns true if the path looks like a Linux absolute path.
func isLinuxPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, MntPrefix)
}

// Normalize ensures a path uses forward slashes and has no trailing separators
// (except for root). This is useful for consistent comparison and display.
func Normalize(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
}
