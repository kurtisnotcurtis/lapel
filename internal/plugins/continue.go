package plugins

import (
	"encoding/json"
	"path/filepath"
)

// ContinuePlugin generates configuration for the Continue VS Code extension.
type ContinuePlugin struct {
	CustomPath string
}

// ContinueConfig represents ~/.continue/config.json.
type ContinueConfig struct {
	Version   string             `json:"version"`
	Providers []ContinueProvider `json:"providers"`
}

// ContinueProvider describes a single model/provider entry for Continue.
type ContinueProvider struct {
	Title         string `json:"title"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	APIBase       string `json:"apiBase"`
	APIKey        string `json:"apiKey"`
	ContextLength int    `json:"contextLength,omitempty"`
}

func (p *ContinuePlugin) Name() string { return "continue" }

func (p *ContinuePlugin) ConfigPath() string {
	if p.CustomPath != "" {
		return p.CustomPath
	}
	return filepath.Join(userHomeDir(), ".continue", "config.json")
}

func (p *ContinuePlugin) GenerateConfig(providers []ProviderSpec) ([]byte, error) {
	var entries []ContinueProvider
	for _, prov := range providers {
		provType := "openai"
		if prov.Name == "ollama" {
			provType = "ollama"
		}
		for _, m := range prov.Models {
			entries = append(entries, ContinueProvider{
				Title:         m.Name,
				Provider:      provType,
				Model:         m.ID,
				APIBase:       prov.BaseURL,
				APIKey:        "local",
				ContextLength: m.ContextWindow,
			})
		}
	}

	cfg := ContinueConfig{
		Version:   "1.0",
		Providers: entries,
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func (p *ContinuePlugin) Apply(providers []ProviderSpec) (string, error) {
	data, err := p.GenerateConfig(providers)
	if err != nil {
		return "", err
	}
	return writeConfigFile(p.ConfigPath(), data)
}
