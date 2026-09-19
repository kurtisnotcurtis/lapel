// Package community defines the public data model for lapel's community features.
// This schema is used by both the public CLI and the private backend API server.
package community

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Report represents a community performance report from a lapel client.
// Private-only backend fields (such as internal user tracking) are added by the
// backend server rather than declared here.
type Report struct {
	ID           string      `json:"id,omitempty"`
	HardwareHash string      `json:"hardware_hash"`
	Models       []ModelPerf `json:"models"`
	Agents       []AgentPerf `json:"agents,omitempty"`
	ConfigHash   string      `json:"config_hash,omitempty"`
	ReportedAt   time.Time   `json:"reported_at"`
}

// ModelPerf captures model performance metrics.
type ModelPerf struct {
	Name     string `json:"name"`     // "Ornith 35B"
	Provider string `json:"provider"` // "LM Studio", "Ollama"

	VRAMUsed  float64 `json:"vram_used_gb"` // GB used
	BatchSize int     `json:"batch_size"`   // Physical batch size

	LoadTimeSec float64 `json:"load_time_sec,omitempty"` // Time to load model
	TokenRate   float64 `json:"token_rate"`              // Tokens/second during inference

	UserRating int    `json:"user_rating,omitempty"` // 1-5 stars
	Notes      string `json:"notes,omitempty"`       // "Works great with pi-vcc compaction"
}

// AgentPerf captures agent performance metrics.
type AgentPerf struct {
	Name     string `json:"name"`     // "pi", "copilot", "claude"
	Provider string `json:"provider"` // "local", "api"

	ResponseTimeSec float64 `json:"response_time_sec"` // Average response time
	TokenRate       float64 `json:"token_rate"`        // Tokens/second

	SystemPromptSize int `json:"system_prompt_size_chars,omitempty"` // Characters in system prompt
	ContextWindow    int `json:"context_window,omitempty"`           // Active context window size

	Notes string `json:"notes,omitempty"` // "pi-vcc compaction reduces context penalty"
}

// Recommendation represents a personalized recommendation for a user.
type Recommendation struct {
	Type        string  `json:"type"`         // "model", "agent", "config"
	Title       string  `json:"title"`        // "Ornith 35B with batch size 16"
	Description string  `json:"description"`  // "Works great on RTX 4090 with 24GB VRAM"
	Confidence  float64 `json:"confidence"`   // 0.0 - 1.0 (based on sample size)
	SourceCount int     `json:"source_count"` // Number of users who confirmed this

	MatchReason string `json:"match_reason"` // "Similar hardware: RTX 4090, 32GB RAM"
}

// HardwareProfile represents the detected host system configuration.
type HardwareProfile struct {
	OS   string    `json:"os"`             // "Windows", "macOS", "Linux"
	RAM  int64     `json:"ram_gb"`         // Total GB of system memory
	GPUs []GPUInfo `json:"gpus,omitempty"` // Discovered GPUs

	CPUModel   string    `json:"cpu_model,omitempty"` // Processor details
	CPUCores   int       `json:"cpu_cores,omitempty"` // Physical cores
	DetectedAt time.Time `json:"detected_at"`         // When profile was created
}

// Hash computes a deterministic 16-character hex identifier for the hardware profile.
// The first 8 hex characters encode the primary GPU + VRAM + RAM tier (used by the
// similarity engine for prefix matching), and the remaining 8 hex characters encode
// the host OS and CPU details.
func (p HardwareProfile) Hash() string {
	gpuName := "cpu-only"
	vramGB := 0
	if len(p.GPUs) > 0 {
		gpuName = strings.ToLower(strings.TrimSpace(p.GPUs[0].Name))
		vramGB = p.GPUs[0].VRAMGB
	}

	tierKey := fmt.Sprintf("gpu=%s|vram=%d|ram=%d", gpuName, vramGB, p.RAM)
	tierSum := sha256.Sum256([]byte(tierKey))

	hostKey := fmt.Sprintf("os=%s|cpu=%s|cores=%d",
		strings.ToLower(strings.TrimSpace(p.OS)),
		strings.ToLower(strings.TrimSpace(p.CPUModel)),
		p.CPUCores,
	)
	hostSum := sha256.Sum256([]byte(hostKey))

	return hex.EncodeToString(tierSum[:4]) + hex.EncodeToString(hostSum[:4])
}

// GPUInfo captures GPU specifications.
type GPUInfo struct {
	Name          string `json:"name"`                     // "NVIDIA RTX 4090"
	VRAMGB        int    `json:"vram_gb"`                  // GB of video memory
	DriverVersion string `json:"driver_version,omitempty"` // NVIDIA driver version
	CUDAVersion   string `json:"cuda_version,omitempty"`   // CUDA toolkit version (if detected)
}

// ConfigHash represents a hash of key configuration choices.
type ConfigHash struct {
	BatchSize    int    `json:"batch_size"`              // Physical batch size
	Compaction   string `json:"compaction"`              // "pi-vcc", "default", etc.
	SystemPrompt string `json:"system_prompt,omitempty"` // Key system prompt modifications

	Hash string `json:"hash"` // SHA256 of normalized config
}
