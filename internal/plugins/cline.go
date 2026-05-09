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

func (p *ClinePlugin) GenerateConfig(providers []ProviderSpec) ([]byte, error) {
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

	return json.MarshalIndent(cfg, "", "  ")
}

func (p *ClinePlugin) Apply(providers []ProviderSpec) (string, error) {
	data, err := p.GenerateConfig(providers)
	if err != nil {
		return "", err
	}
	return writeConfigFile(p.ConfigPath(), data)
}
