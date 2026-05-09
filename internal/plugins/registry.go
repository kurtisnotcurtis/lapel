package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// Registry manages all supported VS Code coding-agent plugins.
type Registry struct {
	plugins map[string]Plugin
}

// NewRegistry creates a registry populated with Kilo Code, Continue, and Cline.
func NewRegistry() *Registry {
	return &Registry{
		plugins: map[string]Plugin{
			"kilo-code": &KiloCodePlugin{},
			"continue":  &ContinuePlugin{},
			"cline":     &ClinePlugin{},
		},
	}
}

// Get returns a plugin by name (case-insensitive).
func (r *Registry) Get(name string) (Plugin, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	p, ok := r.plugins[key]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found (available: %s)", name, strings.Join(r.List(), ", "))
	}
	return p, nil
}

// List returns the sorted identifiers of all registered plugins.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns all registered plugins in deterministic name order.
func (r *Registry) All() []Plugin {
	names := r.List()
	out := make([]Plugin, 0, len(names))
	for _, n := range names {
		out = append(out, r.plugins[n])
	}
	return out
}
