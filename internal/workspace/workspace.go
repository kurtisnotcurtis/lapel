package workspace

import (
	"fmt"
	"os"
)

// Workspace represents a project directory available to an agent session.
// It tracks both the host source path and the guest target path, along with
// the import mode that determines how they're connected.
type Workspace struct {
	Name       string    `json:"name"`
	HostPath   string    `json:"hostPath"`   // Windows host path (e.g., C:\Users\kurt\projects)
	GuestPath  string    `json:"guestPath"`  // WSL2 guest path (e.g., /home/agent/workspace/project)
	Mode       ImportMode `json:"mode"`      // "copy" or "bind"
	Imported   bool      `json:"imported"`   // whether the workspace has been imported
}

// Manager provides high-level workspace operations including import, removal,
// and status queries. It's the primary interface for CLI commands and jail adapters.
type Manager struct {
	importer *Importer
	resolver *PathResolver
	user     string
}

// NewManager creates a new workspace manager with sensible defaults.
func NewManager(user string) *Manager {
	resolver := NewPathResolver()
	return &Manager{
		importer: NewImporter(resolver, user),
		resolver: resolver,
		user:     user,
	}
}

// Import performs a workspace import according to the specified mode.
func (m *Manager) Import(hostPath string, name string, mode ImportMode) (*Workspace, error) {
	if err := ValidateHostPath(hostPath); err != nil {
		return nil, fmt.Errorf("validating host path: %w", err)
	}

	result, err := m.importer.Import(hostPath, name, mode)
	if err != nil {
		return nil, fmt.Errorf("importing workspace: %w", err)
	}

	guestPath, _ := m.resolver.HostToGuest(result.GuestPath)

	return &Workspace{
		Name:      name,
		HostPath:  hostPath,
		GuestPath: guestPath,
		Mode:      result.Mode,
		Imported:  result.Synced,
	}, nil
}

// Remove removes an imported workspace.
func (m *Manager) Remove(name string, mode ImportMode) error {
	return m.importer.Remove(name, mode)
}

// Exists checks whether a workspace exists at the expected guest path.
func (m *Manager) Exists(name string, mode ImportMode) bool {
	return m.importer.Exists(name, mode)
}

// Path returns the guest path for a workspace with the given name and mode.
func (m *Manager) Path(name string, mode ImportMode) string {
	return m.importer.Path(name, mode)
}

// ResolveHostToGuest converts a host path to its guest equivalent using the resolver.
func (m *Manager) ResolveHostToGuest(hostPath string) (string, error) {
	return m.resolver.HostToGuest(hostPath)
}

// ResolveGuestToHost converts a guest path back to its host equivalent.
func (m *Manager) ResolveGuestToHost(guestPath string) (string, error) {
	return m.resolver.GuestToHost(guestPath)
}

// DefaultMode returns the default import mode for new workspaces.
// Per Milestone 3 design: Copy mode is the default for optimal performance.
func DefaultMode() ImportMode {
	return ModeCopy
}

// ValidateImportMode checks that a mode string is valid.
func ValidateImportMode(mode string) error {
	switch ImportMode(mode) {
	case ModeCopy, ModeBind:
		return nil
	default:
		return fmt.Errorf("invalid import mode %q (must be 'copy' or 'bind')", mode)
	}
}

// Describe returns a human-readable description of a workspace.
func (w *Workspace) Describe() string {
	return fmt.Sprintf("[%s] %s → %s (imported: %v)", w.Mode, w.HostPath, w.GuestPath, w.Imported)
}

// EnsureDir creates a directory if it doesn't exist, returning no error if it already does.
func EnsureDir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// FileExists checks whether a file or directory exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
