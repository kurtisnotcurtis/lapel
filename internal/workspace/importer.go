package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ImportMode controls how a workspace is imported into WSL2.
type ImportMode string

const (
	// ModeCopy mirrors the host directory into WSL2 native storage using rsync -a.
	// This gives optimal performance for file-heavy operations (git status, linters,
	// build toolchains) because everything lives on ext4 with zero cross-filesystem
	// I/O penalties. Git metadata is fully preserved via rsync's archive mode.
	ModeCopy ImportMode = "copy"

	// ModeBind mounts the host directory directly into WSL2 using a bind mount.
	// This gives live editing without an import step, but suffers from cross-filesystem
	// I/O penalties on NTFS/ext4 boundaries. Use only when host-side live editing
	// without import is mandatory.
	ModeBind ImportMode = "bind"
)

// Importer handles workspace import operations between the Windows host and WSL2 guest.
type Importer struct {
	resolver *PathResolver
	user     string // Linux user for home directory paths
}

// NewImporter creates a new workspace importer with the given resolver and user.
func NewImporter(resolver *PathResolver, user string) *Importer {
	return &Importer{
		resolver: resolver,
		user:     user,
	}
}

// ImportResult captures what happened during an import operation.
type ImportResult struct {
	Mode      ImportMode
	GuestPath string // where the workspace lives in WSL2
	HostPath  string // the source path on Windows host
	Synced    bool   // whether rsync completed successfully (copy mode only)
}

// Import performs a workspace import according to the specified mode.
//
// For ModeCopy: runs `rsync -a --delete <host> <guest>` to mirror the directory.
// For ModeBind: creates a bind mount at the guest path pointing to the host path.
func (imp *Importer) Import(hostPath string, name string, mode ImportMode) (*ImportResult, error) {
	switch mode {
	case ModeCopy:
		return imp.importCopy(hostPath, name)
	case ModeBind:
		return imp.importBind(hostPath, name)
	default:
		return nil, fmt.Errorf("unsupported import mode: %s", mode)
	}
}

// importCopy mirrors the host directory into WSL2 native storage using rsync -a.
// This preserves Git metadata (history, refs, objects) and gives optimal ext4 performance.
func (imp *Importer) importCopy(hostPath string, name string) (*ImportResult, error) {
	guestBase := GuestHomePath(imp.user, DefaultImportTarget)
	guestPath := filepath.Join(guestBase, name)

	// Ensure the target directory exists.
	if err := os.MkdirAll(guestPath, 0o755); err != nil {
		return nil, fmt.Errorf("creating import target: %w", err)
	}

	// Convert host path to guest format for rsync.
	guestHostPath, err := imp.resolver.HostToGuest(hostPath)
	if err != nil {
		return nil, fmt.Errorf("converting host path: %w", err)
	}

	// Build the rsync command with archive mode and delete for sync.
	// rsync -a preserves permissions, timestamps, symlinks, etc.
	// --delete removes files in destination that don't exist in source.
	cmd := exec.Command("bash", "-c", fmt.Sprintf(
		"rsync -a --delete %q/ %q/",
		guestHostPath, guestPath,
	))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ImportResult{
			Mode:      ModeCopy,
			GuestPath: guestPath,
			HostPath:  hostPath,
			Synced:    false,
		}, fmt.Errorf("rsync failed: %w\noutput: %s", err, string(output))
	}

	return &ImportResult{
		Mode:      ModeCopy,
		GuestPath: guestPath,
		HostPath:  hostPath,
		Synced:    true,
	}, nil
}

// importBind creates a bind mount at the guest path pointing to the host path.
// This gives live editing without an import step but has cross-filesystem I/O costs.
func (imp *Importer) importBind(hostPath string, name string) (*ImportResult, error) {
	guestBase := GuestHomePath(imp.user, DefaultImportTarget)
	guestPath := filepath.Join(guestBase, name)

	// Ensure the mount point exists.
	if err := os.MkdirAll(guestPath, 0o755); err != nil {
		return nil, fmt.Errorf("creating bind mount point: %w", err)
	}

	// Convert host path to guest format for the bind mount source.
	guestHostPath, err := imp.resolver.HostToGuest(hostPath)
	if err != nil {
		return nil, fmt.Errorf("converting host path: %w", err)
	}

	// Use mount --bind to create the bind mount.
	cmd := exec.Command("sudo", "mount", "--bind", guestHostPath, guestPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ImportResult{
			Mode:      ModeBind,
			GuestPath: guestPath,
			HostPath:  hostPath,
			Synced:    false,
		}, fmt.Errorf("bind mount failed: %w\noutput: %s", err, string(output))
	}

	return &ImportResult{
		Mode:      ModeBind,
		GuestPath: guestPath,
		HostPath:  hostPath,
		Synced:    true,
	}, nil
}

// Remove cleans up an imported workspace. For copy mode, this removes the
// mirrored directory; for bind mode, it unmounts and optionally removes the mount point.
func (imp *Importer) Remove(name string, mode ImportMode) error {
	guestPath := filepath.Join(GuestHomePath(imp.user, DefaultImportTarget), name)

	switch mode {
	case ModeCopy:
		return os.RemoveAll(guestPath)
	case ModeBind:
		cmd := exec.Command("sudo", "umount", guestPath)
		if _, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("unmount failed: %w", err)
		}
		return os.Remove(guestPath)
	default:
		return fmt.Errorf("unsupported mode: %s", mode)
	}
}

// Exists checks whether a workspace exists at the expected guest path.
func (imp *Importer) Exists(name string, mode ImportMode) bool {
	guestPath := filepath.Join(GuestHomePath(imp.user, DefaultImportTarget), name)
	info, err := os.Stat(guestPath)
	return err == nil && info.IsDir()
}

// Path returns the guest path for a workspace with the given name and mode.
func (imp *Importer) Path(name string, mode ImportMode) string {
	return filepath.Join(GuestHomePath(imp.user, DefaultImportTarget), name)
}

// ValidateHostPath checks that a host path exists and is accessible.
func ValidateHostPath(hostPath string) error {
	abs, err := filepath.Abs(hostPath)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("host path not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("host path is not a directory: %s", abs)
	}
	return nil
}

// ValidateGuestPath checks that a guest path exists and is accessible from WSL2.
func ValidateGuestPath(guestPath string) error {
	info, err := os.Stat(guestPath)
	if err != nil {
		return fmt.Errorf("guest path not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("guest path is not a directory: %s", guestPath)
	}
	return nil
}

// Describe returns a human-readable description of an import result.
func (r *ImportResult) Describe() string {
	modeStr := strings.ToLower(string(r.Mode))
	syncStatus := "synced"
	if !r.Synced {
		syncStatus = "not synced"
	}
	return fmt.Sprintf("[%s] %s → %s (%s)", modeStr, r.HostPath, r.GuestPath, syncStatus)
}
