package plugins

import (
	"encoding/json"
	"path/filepath"
)

// ClinePlugin generates configuration for the Cline VS Code extension.
type ClinePlugin struct {
	CustomPath string
}

// ClineConfig represents ~/.cline/settings.json.
type ClineConfig struct {
	APIProvider     string      `json:"apiProvider"`
	OpenAIBaseURL   string      `json:"openAiBaseUrl"`
	OpenAIModelID   string      `json:"openAiModelId"`
	OpenAIAPIKey    string      `json:"openAiApiKey"`
	ContextWindow   int         `json:"contextWindow,omitempty"`
	AvailableModels []ModelSpec `json:"availableModels,omitempty"`
}

func (p *ClinePlugin) Name() string { return "cline" }

func (p *ClinePlugin) ConfigPath() string {
	if p.CustomPath != "" {
		return p.CustomPath
	}
	return filepath.Join(userHomeDir(), ".cline", "settings.json")
}

func buildClineConfig(providers []ProviderSpec) ClineConfig {
	cfg := ClineConfig{
		APIProvider:   "openai",
		OpenAIBaseURL: "http://127.0.0.1:1234/v1",
		OpenAIAPIKey:  "local",
	}

	for _, prov := range providers {
		if prov.BaseURL != "" {
			cfg.OpenAIBaseURL = prov.BaseURL
		}
		for _, m := range prov.Models {
			if cfg.OpenAIModelID == "" {
				cfg.OpenAIModelID = m.ID
				cfg.ContextWindow = m.ContextWindow
			}
			cfg.AvailableModels = append(cfg.AvailableModels, m)
		}
	}
	return cfg
}

func (p *ClinePlugin) GenerateConfig(providers []ProviderSpec) ([]byte, error) {
	return json.MarshalIndent(buildClineConfig(providers), "", "  ")
}

// Apply merges lapel's OpenAI-compatible provider settings into ~/.cline/settings.json,
// preserving all other existing user keys, and backs up any existing file to .bak.
func (p *ClinePlugin) Apply(providers []ProviderSpec) (string, error) {
	target := p.ConfigPath()
	existing, err := readExistingJSONObject(target)
	if err != nil {
		return "", err
	}

	cfg := buildClineConfig(providers)
	existing["apiProvider"] = cfg.APIProvider
	existing["openAiBaseUrl"] = cfg.OpenAIBaseURL
	existing["openAiModelId"] = cfg.OpenAIModelID
	existing["openAiApiKey"] = cfg.OpenAIAPIKey
	if cfg.ContextWindow > 0 {
		existing["contextWindow"] = cfg.ContextWindow
	}
	if len(cfg.AvailableModels) > 0 {
		existing["availableModels"] = cfg.AvailableModels
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return "", err
	}
	return writeConfigFile(target, data)
}
