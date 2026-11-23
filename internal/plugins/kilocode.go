package plugins

import (
	"encoding/json"
	"path/filepath"
	"time"
)

// KiloCodePlugin generates configuration for the Kilo Code VS Code extension.
type KiloCodePlugin struct {
	CustomPath string
}

// KiloConfig represents Kilo Code's configuration schema.
type KiloConfig struct {
	Version     string       `json:"version"`
	GeneratedAt time.Time    `json:"generated_at"`
	Provider    KiloProvider `json:"provider"`
}

// KiloProvider defines an OpenAI-compatible local provider for Kilo Code.
type KiloProvider struct {
	NPM     string               `json:"npm"`
	Name    string               `json:"name"`
	Options map[string]string    `json:"options"`
	Models  map[string]KiloModel `json:"models"`
}

// KiloModel describes a single model entry in Kilo Code.
type KiloModel struct {
	Name          string `json:"name"`
	ID            string `json:"id,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	SizeClass     string `json:"sizeClass,omitempty"`
	Status        string `json:"status,omitempty"`
}

func (p *KiloCodePlugin) Name() string { return "kilo-code" }

func (p *KiloCodePlugin) ConfigPath() string {
	if p.CustomPath != "" {
		return p.CustomPath
	}
	return filepath.Join(userHomeDir(), ".vscode", "extensions", "kilo-code", "config.json")
}

func buildKiloConfig(providers []ProviderSpec) KiloConfig {
	baseURL := "http://127.0.0.1:1234/v1"
	providerLabel := "LM Studio (local)"
	models := make(map[string]KiloModel)

	for _, prov := range providers {
		if prov.BaseURL != "" {
			baseURL = prov.BaseURL
		}
		if prov.Name != "" {
			providerLabel = prov.Name + " (local)"
		}
		for _, m := range prov.Models {
			models[m.ID] = KiloModel{
				Name:          m.Name,
				ID:            m.ID,
				ContextWindow: m.ContextWindow,
				SizeClass:     m.SizeClass,
				Status:        "ready",
			}
		}
	}

	return KiloConfig{
		Version:     "1.0",
		GeneratedAt: time.Now().UTC(),
		Provider: KiloProvider{
			NPM:  "@ai-sdk/openai-compatible",
			Name: providerLabel,
			Options: map[string]string{
				"baseURL": baseURL,
			},
			Models: models,
		},
	}
}

func (p *KiloCodePlugin) GenerateConfig(providers []ProviderSpec) ([]byte, error) {
	return json.MarshalIndent(buildKiloConfig(providers), "", "  ")
}

// Apply merges lapel's provider section into the Kilo Code config file,
// preserving unrelated keys and backing up any existing file to .bak.
func (p *KiloCodePlugin) Apply(providers []ProviderSpec) (string, error) {
	target := p.ConfigPath()
	existing, err := readExistingJSONObject(target)
	if err != nil {
		return "", err
	}

	cfg := buildKiloConfig(providers)
	existing["version"] = cfg.Version
	existing["generated_at"] = cfg.GeneratedAt
	existing["provider"] = cfg.Provider

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", err
	}
	return writeConfigFile(target, data)
}
