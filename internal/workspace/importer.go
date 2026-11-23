package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"lapel/internal/backend/wsl"
)

// ImportMode controls how a workspace is imported into WSL2.
type ImportMode string

const (
	// ModeCopy mirrors the host directory into WSL2 native storage using rsync -a.
	ModeCopy ImportMode = "copy"

	// ModeBind links the host directory into WSL2.
	ModeBind ImportMode = "bind"
)

var validWorkspaceNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidateWorkspaceName rejects empty names, path traversal, path separators,
// and shell metacharacters.
func ValidateWorkspaceName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name must not be empty")
	}
	if strings.Contains(name, "..") || !validWorkspaceNameRe.MatchString(name) {
		return fmt.Errorf("invalid workspace name %q (allowed: letters, digits, '.', '_', '-', no '..' or slashes)", name)
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Importer handles workspace import operations between the Windows host and WSL2 guest.
type Importer struct {
	resolver *PathResolver
	distro   string // target WSL distribution name
	user     string // Linux user for home directory paths
}

// NewImporter creates a new workspace importer with the given resolver, distro, and user.
func NewImporter(resolver *PathResolver, distro, user string) *Importer {
	if strings.TrimSpace(distro) == "" {
		distro = wsl.DefaultDist
	}
	if strings.TrimSpace(user) == "" {
		user = "agent"
	}
	return &Importer{
		resolver: resolver,
		distro:   strings.TrimSpace(distro),
		user:     strings.TrimSpace(user),
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
// or directly via bash on native Linux, passing the script via stdin so WSL
// never mangles command substitutions or quotes in argv.
func (imp *Importer) runGuestScript(script string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("wsl.exe", "-d", imp.distro, "bash", "-l")
	} else {
		cmd = exec.Command("bash", "-l")
	}
	cmd.Stdin = strings.NewReader(script + "\n")
	return cmd.CombinedOutput()
}

// buildCopyScript returns a POSIX-safe shell script for ModeCopy.
func buildCopyScript(guestHostPath, guestPath string) string {
	return fmt.Sprintf("mkdir -p %s && rsync -a --delete %s/ %s/",
		shellQuote(guestPath), shellQuote(guestHostPath), shellQuote(guestPath))
}

// buildBindScript returns a POSIX-safe shell script for ModeBind.
func buildBindScript(guestBase, guestHostPath, guestPath string) string {
	return fmt.Sprintf("mkdir -p %s && ln -sfn %s %s",
		shellQuote(guestBase), shellQuote(guestHostPath), shellQuote(guestPath))
}

// Import performs a workspace import according to the specified mode.
func (imp *Importer) Import(hostPath string, name string, mode ImportMode) (*ImportResult, error) {
	if err := ValidateWorkspaceName(name); err != nil {
		return nil, err
	}
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

	script := buildCopyScript(guestHostPath, guestPath)
	output, err := imp.runGuestScript(script)
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
// directory inside WSL.
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

	script := buildBindScript(guestBase, guestHostPath, guestPath)
	output, err := imp.runGuestScript(script)
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
	if err := ValidateWorkspaceName(name); err != nil {
		return err
	}
	guestPath := imp.Path(name, mode)
	switch mode {
	case ModeCopy, ModeBind:
		out, err := imp.runGuestScript("rm -rf " + shellQuote(guestPath))
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
	if ValidateWorkspaceName(name) != nil {
		return false
	}
	guestPath := imp.Path(name, mode)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(guestPath)
		return err == nil && info.IsDir()
	}
	_, err := imp.runGuestScript("test -e " + shellQuote(guestPath))
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
