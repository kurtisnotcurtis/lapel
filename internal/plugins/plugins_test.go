package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestContextWindowForModel(t *testing.T) {
	if got := ContextWindowForModel("ornith-35b-q4_k_m", 24.0); got != 16384 {
		t.Errorf("35B on 24GB VRAM: got %d, want 16384", got)
	}
	if got := ContextWindowForModel("qwen2.5-coder-7b", 24.0); got != 32768 {
		t.Errorf("7B on 24GB VRAM: got %d, want 32768", got)
	}
	if got := ContextWindowForModel("ornith-35b-q4_k_m", 12.0); got != 4096 {
		t.Errorf("35B on 12GB VRAM: got %d, want 4096", got)
	}
}

func TestPluginsGenerateAndApply(t *testing.T) {
	dir := t.TempDir()
	providers := []ProviderSpec{
		{
			Name:    "lm-studio",
			Port:    1234,
			BaseURL: "http://127.0.0.1:1234/v1",
			Models:  BuildModelSpecs([]string{"ornith-35b", "qwen-14b"}, 24.0),
		},
	}

	kilo := &KiloCodePlugin{CustomPath: filepath.Join(dir, "kilo", "config.json")}
	kiloPath, err := kilo.Apply(providers)
	if err != nil {
		t.Fatalf("KiloCodePlugin.Apply: %v", err)
	}
	raw, err := os.ReadFile(kiloPath)
	if err != nil {
		t.Fatalf("read kilo config: %v", err)
	}
	var kiloCfg KiloConfig
	if err := json.Unmarshal(raw, &kiloCfg); err != nil {
		t.Fatalf("unmarshal kilo config: %v", err)
	}
	if len(kiloCfg.Provider.Models) != 2 || kiloCfg.Provider.Models["ornith-35b"].ContextWindow != 16384 {
		t.Errorf("unexpected kilo models: %+v", kiloCfg.Provider.Models)
	}

	cont := &ContinuePlugin{CustomPath: filepath.Join(dir, "continue", "config.json")}
	if _, err := cont.Apply(providers); err != nil {
		t.Fatalf("ContinuePlugin.Apply: %v", err)
	}

	cline := &ClinePlugin{CustomPath: filepath.Join(dir, "cline", "settings.json")}
	if _, err := cline.Apply(providers); err != nil {
		t.Fatalf("ClinePlugin.Apply: %v", err)
	}
}
