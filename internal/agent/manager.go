package agent

import "context"

// Manager orchestrates agent detection, configuration, and launching.
type Manager struct {
	agents []Agent
}

// NewManager creates a new agent manager with default agents.
func NewManager() *Manager {
	return &Manager{
		agents: []Agent{
			NewPiAgent(),
			NewClaudeCodeAgent(),
			NewCopilotAgent(),
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

// DetectAll checks all agents and returns their detection status.
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
