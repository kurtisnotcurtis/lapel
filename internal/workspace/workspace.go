package workspace

import (
	"fmt"
)

// Workspace represents a project directory available to an agent session.
type Workspace struct {
	Name      string     `json:"name"`
	HostPath  string     `json:"hostPath"`
	GuestPath string     `json:"guestPath"`
	Mode      ImportMode `json:"mode"`
	Imported  bool       `json:"imported"`
}

// Manager provides high-level workspace operations including import, removal,
// and status queries.
type Manager struct {
	importer *Importer
	resolver *PathResolver
	user     string
}

// NewManager creates a new workspace manager targeting the specified WSL distro
// and Linux user.
func NewManager(distro, user string) *Manager {
	resolver := NewPathResolver()
	return &Manager{
		importer: NewImporter(resolver, distro, user),
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

// DefaultMode returns the default import mode for new workspaces.
func DefaultMode() ImportMode {
	return ModeBind
}
