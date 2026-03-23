package agent

import "context"

// Manager orchestrates agent detection and configuration.
type Manager struct {
	dist   string
	agents []Agent
}

// NewManager creates a new agent manager targeting the specified WSL distro
// (defaults to "Ubuntu-Lean" if omitted).
func NewManager(distro ...string) *Manager {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &Manager{
		dist: d,
		agents: []Agent{
			NewPiAgent(d),
			NewClaudeCodeAgent(d),
			NewCopilotAgent(d),
		},
	}
}

// List returns all known agent types.
func (m *Manager) List() []string {
	var names []string
	for _, a := range m.agents {
		names = append(names, a.Name())
	}
	return names
}

// FindByName returns an agent by its name.
func (m *Manager) FindByName(name string) Agent {
	for _, a := range m.agents {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// DetectAll checks all agents inside the target WSL distro and returns their status.
func (m *Manager) DetectAll(ctx context.Context) ([]AgentStatus, error) {
	var statuses []AgentStatus
	for _, a := range m.agents {
		status := AgentStatus{
			Name: a.Name(),
		}
		installed, err := a.Detect(ctx)
		if err != nil {
			status.Error = err.Error()
		} else {
			status.Installed = installed
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// AgentStatus is runtime status for an agent.
type AgentStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}
