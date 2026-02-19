package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"lapel/internal/backend/wsl"
)

// ImportMode controls how a workspace is imported into WSL2.
type ImportMode string

const (
	// ModeCopy mirrors the host directory into WSL2 native storage using rsync -a.
	ModeCopy ImportMode = "copy"

	// ModeBind links or bind-mounts the host directory into WSL2.
	ModeBind ImportMode = "bind"
)

// Importer handles workspace import operations between the Windows host and WSL2 guest.
type Importer struct {
	resolver *PathResolver
	user     string // Linux user for home directory paths
}

// NewImporter creates a new workspace importer with the given resolver and user.
func NewImporter(resolver *PathResolver, user string) *Importer {
	if user == "" {
		user = "agent"
	}
	return &Importer{
		resolver: resolver,
		user:     user,
	}
}

// ImportResult captures what happened during an import operation.
type ImportResult struct {
	Mode      ImportMode
	GuestPath string // where the workspace lives in WSL2 (always forward-slash Linux path)
	HostPath  string // the source path on Windows host
	Synced    bool   // whether import completed successfully
}

// runGuestScript executes a shell script inside WSL when running on Windows,
// or directly via bash on native Linux.
func runGuestScript(script string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return exec.Command("wsl.exe", "-d", wsl.Dist, "--", "bash", "-lc", script).CombinedOutput()
	}
	return exec.Command("bash", "-lc", script).CombinedOutput()
}

// Import performs a workspace import according to the specified mode.
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
func (imp *Importer) importCopy(hostPath string, name string) (*ImportResult, error) {
	guestBase := GuestHomePath(imp.user, DefaultImportTarget)
	guestPath := path.Join(guestBase, name)

	absHost, err := filepath.Abs(hostPath)
	if err != nil {
		absHost = hostPath
	}
	guestHostPath, err := imp.resolver.HostToGuest(absHost)
	if err != nil {
		return nil, fmt.Errorf("converting host path: %w", err)
	}

	script := fmt.Sprintf("mkdir -p %q && rsync -a --delete %q/ %q/", guestPath, guestHostPath, guestPath)
	output, err := runGuestScript(script)
	if err != nil {
		return &ImportResult{
			Mode:      ModeCopy,
			GuestPath: guestPath,
			HostPath:  hostPath,
			Synced:    false,
		}, fmt.Errorf("rsync failed: %w\noutput: %s", err, strings.TrimSpace(string(output)))
	}

	return &ImportResult{
		Mode:      ModeCopy,
		GuestPath: guestPath,
		HostPath:  hostPath,
		Synced:    true,
	}, nil
}

// importBind links the already-mounted /mnt/<drive>/... path to the guest workspace
// directory inside WSL (avoiding interactive root password prompts for mount --bind).
func (imp *Importer) importBind(hostPath string, name string) (*ImportResult, error) {
	guestBase := GuestHomePath(imp.user, DefaultImportTarget)
	guestPath := path.Join(guestBase, name)

	absHost, err := filepath.Abs(hostPath)
	if err != nil {
		absHost = hostPath
	}
	guestHostPath, err := imp.resolver.HostToGuest(absHost)
	if err != nil {
		return nil, fmt.Errorf("converting host path: %w", err)
	}

	script := fmt.Sprintf("mkdir -p %q && ln -sfn %q %q", guestBase, guestHostPath, guestPath)
	output, err := runGuestScript(script)
	if err != nil {
		return &ImportResult{
			Mode:      ModeBind,
			GuestPath: guestPath,
			HostPath:  hostPath,
			Synced:    false,
		}, fmt.Errorf("bind link failed: %w\noutput: %s", err, strings.TrimSpace(string(output)))
	}

	return &ImportResult{
		Mode:      ModeBind,
		GuestPath: guestPath,
		HostPath:  hostPath,
		Synced:    true,
	}, nil
}

// Remove cleans up an imported workspace inside the guest.
func (imp *Importer) Remove(name string, mode ImportMode) error {
	guestPath := imp.Path(name, mode)
	switch mode {
	case ModeCopy, ModeBind:
		out, err := runGuestScript(fmt.Sprintf("rm -rf %q", guestPath))
		if err != nil {
			return fmt.Errorf("removing guest workspace %s: %w (%s)", guestPath, err, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return fmt.Errorf("unsupported mode: %s", mode)
	}
}

// Exists checks whether a workspace exists at the expected guest path.
func (imp *Importer) Exists(name string, mode ImportMode) bool {
	guestPath := imp.Path(name, mode)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(guestPath)
		return err == nil && info.IsDir()
	}
	_, err := runGuestScript(fmt.Sprintf("test -e %q", guestPath))
	return err == nil
}

// Path returns the forward-slash Linux guest path for a workspace.
func (imp *Importer) Path(name string, mode ImportMode) string {
	return path.Join(GuestHomePath(imp.user, DefaultImportTarget), name)
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
	if runtime.GOOS != "windows" {
		info, err := os.Stat(guestPath)
		if err != nil {
			return fmt.Errorf("guest path not accessible: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("guest path is not a directory: %s", guestPath)
		}
		return nil
	}
	if _, err := runGuestScript(fmt.Sprintf("test -d %q", guestPath)); err != nil {
		return fmt.Errorf("guest path not accessible in WSL: %s", guestPath)
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
