package hardware

import "fmt"

// DefaultRecommender recommends models based on available VRAM/RAM.
type DefaultRecommender struct {
	// ModelTiers defines the model size tiers with their requirements.
	ModelTiers []ModelTier
}

func NewDefaultRecommender() *DefaultRecommender {
	return &DefaultRecommender{
		ModelTiers: []ModelTier{
			{Name: "tiny", Description: "7B–8B coding models (fast iteration, low VRAM)", MinVRAMGB: 4, Speed: "fast"},
			{Name: "small", Description: "14B coding models (balanced reasoning & speed)", MinVRAMGB: 8, Speed: "medium"},
			{Name: "medium", Description: "32B–35B coding models (strong multi-file reasoning)", MinVRAMGB: 12, Speed: "slow"},
			{Name: "large", Description: "70B+ parameter models (frontier local reasoning)", MinVRAMGB: 24, Speed: "very-slow"},
		},
	}
}

func (r *DefaultRecommender) Recommend(info *Info) *RecommendedModel {
	// Determine available VRAM for model loading
	var availableVRAM float64
	if info.GPU.Available && info.GPU.VRAMGB > 0 {
		availableVRAM = info.GPU.VRAMGB * 0.85 // Leave 15% for system/WSL/IDE overhead
	} else {
		// Fall back to RAM if no GPU
		availableVRAM = info.RAM.AvailableGB * 0.7
	}

	// Find the best fitting tier
	var bestTier *ModelTier
	for i := len(r.ModelTiers) - 1; i >= 0; i-- {
		tier := &r.ModelTiers[i]
		if availableVRAM >= tier.MinVRAMGB {
			bestTier = tier
			break
		}
	}

	if bestTier == nil {
		bestTier = &r.ModelTiers[0]
	}

	modelName := r.suggestModelName(bestTier, info)
	reasoning := r.generateReasoning(bestTier, availableVRAM, info)

	return &RecommendedModel{
		Tier:      *bestTier,
		ModelName: modelName,
		Reasoning: reasoning,
	}
}

func (r *DefaultRecommender) suggestModelName(tier *ModelTier, info *Info) string {
	switch tier.Name {
	case "tiny":
		return "Qwen2.5-Coder-7B-Instruct-Q4_K_M"
	case "small":
		if info.GPU.Available && info.GPU.VRAMGB >= 12 {
			return "Qwen2.5-Coder-14B-Instruct-Q5_K_M"
		}
		return "Qwen2.5-Coder-14B-Instruct-Q4_K_M"
	case "medium":
		return "Qwen2.5-Coder-32B-Instruct-Q4_K_M"
	case "large":
		return "Llama-3.3-70B-Instruct-Q4_K_M"
	default:
		return "Qwen2.5-Coder-7B-Instruct-Q4_K_M"
	}
}

func (r *DefaultRecommender) generateReasoning(tier *ModelTier, availableVRAM float64, info *Info) string {
	reasoning := fmt.Sprintf("Recommended %s tier (%.1f GB usable VRAM budget after 15%% OS/IDE headroom)", tier.Name, availableVRAM)

	if info.GPU.Available && info.GPU.VRAMGB > 0 {
		reasoning += fmt.Sprintf("\nGPU: %s with %.1f GB VRAM", info.GPU.Name, info.GPU.VRAMGB)
	} else {
		reasoning += "\nNo dedicated GPU detected, using system RAM"
	}

	if tier.Speed == "fast" {
		reasoning += "\nExpected: Fast inference (~60+ tokens/sec)"
	} else if tier.Speed == "medium" {
		reasoning += "\nExpected: Balanced performance (~35–55 tokens/sec)"
	} else {
		reasoning += "\nExpected: High reasoning depth (~18–35 tokens/sec)"
	}

	return reasoning
}
