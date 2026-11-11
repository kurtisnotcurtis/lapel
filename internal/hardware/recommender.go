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
			{Name: "tiny", Description: "7B parameter models (fast, basic reasoning)", MinVRAMGB: 4, Speed: "fast"},
			{Name: "small", Description: "13B parameter models (balanced performance)", MinVRAMGB: 8, Speed: "medium"},
			{Name: "medium", Description: "30-34B parameter models (good reasoning)", MinVRAMGB: 12, Speed: "slow"},
			{Name: "large", Description: "70B+ parameter models (best quality)", MinVRAMGB: 24, Speed: "very-slow"},
		},
	}
}

func (r *DefaultRecommender) Recommend(info *Info) *RecommendedModel {
	// Determine available VRAM for model loading
	var availableVRAM float64
	if info.GPU.Available && info.GPU.VRAMGB > 0 {
		availableVRAM = info.GPU.VRAMGB * 0.85 // Leave 15% for system overhead
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
		// Use smallest tier as fallback
		bestTier = &r.ModelTiers[0]
	}

	// Generate recommendation based on tier and hardware
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
		return "Mistral-7B-Q4_K_M"
	case "small":
		if info.GPU.Available && info.GPU.VRAMGB >= 12 {
			return "Llama-3-13B-Q5_K_M"
		}
		return "CodeLlama-13B-Q4_K_M"
	case "medium":
		return "Mixtral-8x7B-Q4_K_M"
	case "large":
		return "Llama-3-70B-Q4_K_M"
	default:
		return "default-model"
	}
}

func (r *DefaultRecommender) generateReasoning(tier *ModelTier, availableVRAM float64, info *Info) string {
	reasoning := fmt.Sprintf("Recommended %s model (%.1f GB VRAM available)", tier.Name, tier.MinVRAMGB)

	if info.GPU.Available && info.GPU.VRAMGB > 0 {
		reasoning += fmt.Sprintf("\nGPU: %s with %.1f GB VRAM", info.GPU.Name, info.GPU.VRAMGB)
	} else {
		reasoning += "\nNo dedicated GPU detected, using system RAM"
	}

	if tier.Speed == "fast" {
		reasoning += "\nExpected: Fast inference (~50+ tokens/sec)"
	} else if tier.Speed == "medium" {
		reasoning += "\nExpected: Balanced performance (~20-40 tokens/sec)"
	} else {
		reasoning += "\nExpected: Slower but higher quality (~10-20 tokens/sec)"
	}

	return reasoning
}
