package provider

import "context"

// Registry manages multiple inference providers.
type Registry struct {
	providers []Manager
}

// NewRegistry creates a new provider registry with default providers.
// The distro parameter specifies which WSL distribution to use (defaults to "Ubuntu-Lean").
func NewRegistry(distro ...string) *Registry {
	d := "Ubuntu-Lean"
	if len(distro) > 0 && distro[0] != "" {
		d = distro[0]
	}
	return &Registry{
		providers: []Manager{
			NewLMStudio(d),
			NewOllama(d),
		},
	}
}

// Providers returns all registered providers.
func (r *Registry) Providers() []Manager {
	return r.providers
}

// FindByName returns a provider by its type identifier.
func (r *Registry) FindByName(name string) Manager {
	for _, p := range r.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// DetectAll checks all providers and returns their detection status.
func (r *Registry) DetectAll(ctx context.Context) ([]*ProviderStatus, error) {
	var statuses []*ProviderStatus
	for _, p := range r.providers {
		status := &ProviderStatus{
			Name: p.Name(),
			Type: p.Name(),
		}
		installed, err := p.Detect(ctx)
		if err != nil {
			status.Error = err.Error()
		} else {
			status.Installed = installed
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// StatusAll returns runtime status for all providers.
func (r *Registry) StatusAll(ctx context.Context) ([]*ProviderStatus, error) {
	var statuses []*ProviderStatus
	for _, p := range r.providers {
		status, err := p.Status(ctx)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}
