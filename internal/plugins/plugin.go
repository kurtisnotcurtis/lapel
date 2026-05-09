// Package plugins provides zero-touch configuration generation and model
// syncing for VS Code coding-agent extensions (Kilo Code, Continue, Cline)
// using live models discovered from LM Studio / Ollama and VRAM-aware context
// window sizing.
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ModelSpec represents a discovered local model and its hardware-tuned settings.
type ModelSpec struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SizeClass     string `json:"size_class"`
	ContextWindow int    `json:"context_window"`
}

// ProviderSpec represents a running local inference provider and its models.
type ProviderSpec struct {
	Name    string      `json:"name"`     // "lm-studio", "ollama"
	Port    int         `json:"port"`     // 1234, 11434
	BaseURL string      `json:"base_url"` // e.g., "http://127.0.0.1:1234/v1"
	Models  []ModelSpec `json:"models"`
}

// Plugin is the common interface implemented by supported IDE extensions.
type Plugin interface {
	// Name returns the canonical plugin identifier ("kilo-code", "continue", "cline").
	Name() string

	// ConfigPath returns the target path where the plugin's configuration file lives.
	ConfigPath() string

	// GenerateConfig renders the plugin's JSON configuration bytes for the given providers.
	GenerateConfig(providers []ProviderSpec) ([]byte, error)

	// Apply writes the generated configuration to ConfigPath() and returns the path written.
	Apply(providers []ProviderSpec) (string, error)
}

var paramRe = regexp.MustCompile(`(?i)(?:^|[^0-9])([0-9]{1,3})\s*b(?:$|[^a-z0-9])`)

// InferSizeClass extracts a parameter size bucket ("8b", "14b", "35b", "70b")
// from a model identifier (e.g. "ornith-35b-q4_k_m" -> "35b").
func InferSizeClass(modelID string) string {
	m := paramRe.FindStringSubmatch(modelID)
	if len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			switch {
			case n >= 60:
				return "70b"
			case n >= 24:
				return "35b"
			case n >= 12:
				return "14b"
			default:
				return "8b"
			}
		}
	}
	return "8b"
}

// ContextWindowForModel computes a safe, VRAM-aware context window length for
// a given model identifier and available GPU VRAM (in GB).
func ContextWindowForModel(modelID string, vramGB float64) int {
	sizeClass := InferSizeClass(modelID)

	switch {
	case vramGB >= 24:
		switch sizeClass {
		case "8b":
			return 32768
		case "14b", "35b":
			return 16384
		default:
			return 8192
		}
	case vramGB >= 16:
		switch sizeClass {
		case "8b":
			return 16384
		case "14b":
			return 12288
		case "35b":
			return 8192
		default:
			return 4096
		}
	case vramGB >= 12:
		switch sizeClass {
		case "8b":
			return 16384
		case "14b":
			return 8192
		default:
			return 4096
		}
	default:
		if sizeClass == "8b" {
			return 8192
		}
		return 4096
	}
}

// BuildModelSpecs converts raw model IDs into VRAM-tuned ModelSpec entries.
func BuildModelSpecs(modelIDs []string, vramGB float64) []ModelSpec {
	specs := make([]ModelSpec, 0, len(modelIDs))
	for _, id := range modelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		specs = append(specs, ModelSpec{
			ID:            id,
			Name:          id,
			SizeClass:     InferSizeClass(id),
			ContextWindow: ContextWindowForModel(id, vramGB),
		})
	}
	return specs
}

func userHomeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "."
}

func writeConfigFile(path string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating config dir for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing config %s: %w", path, err)
	}
	return path, nil
}
