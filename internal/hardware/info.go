// Package hardware detects system resources (GPU, RAM, CPU) and recommends
// appropriate model tiers based on available VRAM/RAM.
package hardware

import "context"

// Info holds detected system resource information.
type Info struct {
	GPU    GPUInfo   `json:"gpu"`
	RAM    RAMInfo   `json:"ram"`
	CPU    CPUInfo   `json:"cpu"`
	OS     string    `json:"os"` // "windows", "linux"
}

// IsLinux returns true if we're running on Linux (WSL).
func (i *Info) IsLinux() bool {
	return i.OS == "linux"
}

// GPUInfo contains graphics processing unit details.
type GPUInfo struct {
	Name      string  `json:"name"`
	VRAMGB    float64 `json:"vramGb"`
	Compute   string  `json:"compute,omitempty"` // CUDA compute capability or ROCm version
	Available bool    `json:"available"`
}

// RAMInfo contains memory details.
type RAMInfo struct {
	TotalGB     float64 `json:"totalGb"`
	AvailableGB float64 `json:"availableGb"`
	UsedPercent float64 `json:"usedPercent"`
}

// CPUInfo contains processor details.
type CPUInfo struct {
	Name    string `json:"name"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

// ModelTier represents a model size tier with hardware requirements.
type ModelTier struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	MinVRAMGB   float64 `json:"minVramGb"`
	Recommended bool    `json:"recommended"`
	Speed       string  `json:"speed"` // "fast", "medium", "slow"
}

// RecommendedModel contains the best model recommendation for current hardware.
type RecommendedModel struct {
	Tier      ModelTier `json:"tier"`
	ModelName string    `json:"modelName"`
	Reasoning string    `json:"reasoning"`
}

// Detector detects system hardware information.
type Detector interface {
	Detect(ctx context.Context) (*Info, error)
}

// Recommender recommends models based on hardware constraints.
type Recommender interface {
	Recommend(info *Info) *RecommendedModel
}
