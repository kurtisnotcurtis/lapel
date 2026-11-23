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

func buildContinueEntries(providers []ProviderSpec) []ContinueProvider {
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
	return entries
}

func (p *ContinuePlugin) GenerateConfig(providers []ProviderSpec) ([]byte, error) {
	cfg := ContinueConfig{
		Version:   "1.0",
		Providers: buildContinueEntries(providers),
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// Apply merges lapel's discovered local provider entries into ~/.continue/config.json,
// preserving existing unrelated settings and non-colliding providers, and creates a
// .bak backup before modifying an existing file.
func (p *ContinuePlugin) Apply(providers []ProviderSpec) (string, error) {
	target := p.ConfigPath()
	existing, err := readExistingJSONObject(target)
	if err != nil {
		return "", err
	}

	newEntries := buildContinueEntries(providers)
	incomingKeys := make(map[string]bool, len(newEntries))
	for _, e := range newEntries {
		incomingKeys[e.Title+"|"+e.APIBase] = true
	}

	// Preserve existing providers that don't collide with the freshly synced entries.
	var merged []any
	if rawList, ok := existing["providers"].([]any); ok {
		for _, item := range rawList {
			if m, ok := item.(map[string]any); ok {
				title, _ := m["title"].(string)
				apiBase, _ := m["apiBase"].(string)
				if incomingKeys[title+"|"+apiBase] {
					continue
				}
			}
			merged = append(merged, item)
		}
	}
	for _, e := range newEntries {
		merged = append(merged, e)
	}

	if _, ok := existing["version"]; !ok {
		existing["version"] = "1.0"
	}
	existing["providers"] = merged

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", err
	}
	return writeConfigFile(target, data)
}
