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

func TestPluginsMergeAndBackupExistingConfigs(t *testing.T) {
	dir := t.TempDir()
	providers := []ProviderSpec{
		{
			Name:    "lm-studio",
			Port:    1234,
			BaseURL: "http://127.0.0.1:1234/v1",
			Models:  BuildModelSpecs([]string{"qwen-14b"}, 24.0),
		},
	}

	// Continue: preserves unrelated top-level keys and existing cloud providers, creates .bak.
	contPath := filepath.Join(dir, "continue.json")
	origCont := `{"version":"1.0","customSetting":"keep-me","providers":[{"title":"Claude Cloud","provider":"anthropic","model":"claude-3-7","apiBase":"https://api.anthropic.com"}]}`
	if err := os.WriteFile(contPath, []byte(origCont), 0o644); err != nil {
		t.Fatal(err)
	}
	cont := &ContinuePlugin{CustomPath: contPath}
	if _, err := cont.Apply(providers); err != nil {
		t.Fatalf("ContinuePlugin.Apply: %v", err)
	}
	if bak, err := os.ReadFile(contPath + ".bak"); err != nil || string(bak) != origCont {
		t.Fatalf("expected backup file with original contents, err=%v", err)
	}
	var mergedCont map[string]any
	rawCont, _ := os.ReadFile(contPath)
	if err := json.Unmarshal(rawCont, &mergedCont); err != nil {
		t.Fatal(err)
	}
	if mergedCont["customSetting"] != "keep-me" {
		t.Errorf("ContinuePlugin wiped customSetting: %+v", mergedCont)
	}
	if plist, ok := mergedCont["providers"].([]any); !ok || len(plist) != 2 {
		t.Errorf("ContinuePlugin expected 2 merged providers (existing + local), got %+v", mergedCont["providers"])
	}

	// Cline: preserves unrelated keys and creates .bak.
	clinePath := filepath.Join(dir, "cline.json")
	origCline := `{"customInstructions":"be concise","autoApprove":true}`
	if err := os.WriteFile(clinePath, []byte(origCline), 0o644); err != nil {
		t.Fatal(err)
	}
	cline := &ClinePlugin{CustomPath: clinePath}
	if _, err := cline.Apply(providers); err != nil {
		t.Fatalf("ClinePlugin.Apply: %v", err)
	}
	if bak, err := os.ReadFile(clinePath + ".bak"); err != nil || string(bak) != origCline {
		t.Fatalf("expected Cline backup file, err=%v", err)
	}
	var mergedCline map[string]any
	rawCline, _ := os.ReadFile(clinePath)
	if err := json.Unmarshal(rawCline, &mergedCline); err != nil {
		t.Fatal(err)
	}
	if mergedCline["customInstructions"] != "be concise" || mergedCline["openAiModelId"] != "qwen-14b" {
		t.Errorf("unexpected merged Cline config: %+v", mergedCline)
	}

	// Corrupted JSON file must NOT be silently overwritten.
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&ClinePlugin{CustomPath: badPath}).Apply(providers); err == nil {
		t.Error("expected error when existing config is invalid JSON")
	}
}
