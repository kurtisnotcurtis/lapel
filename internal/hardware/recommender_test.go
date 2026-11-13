package hardware

import (
	"strings"
	"testing"
)

func TestDefaultRecommender_CodingModels(t *testing.T) {
	rec := NewDefaultRecommender()

	cases := []struct {
		name      string
		vramGB    float64
		wantTier  string
		wantModel string
	}{
		{"6GB GPU -> tiny", 6.0, "tiny", "Qwen2.5-Coder-7B-Instruct-Q4_K_M"},
		{"12GB GPU -> small Q5", 12.0, "small", "Qwen2.5-Coder-14B-Instruct-Q5_K_M"},
		{"16GB GPU -> medium 32B", 16.0, "medium", "Qwen2.5-Coder-32B-Instruct-Q4_K_M"},
		{"32GB GPU -> large 70B", 32.0, "large", "Llama-3.3-70B-Instruct-Q4_K_M"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &Info{
				OS: "windows",
				GPU: GPUInfo{
					Name:      "NVIDIA GeForce RTX Test",
					VRAMGB:    tc.vramGB,
					Available: true,
				},
				RAM: RAMInfo{TotalGB: 32, AvailableGB: 24},
			}
			got := rec.Recommend(info)
			if got.Tier.Name != tc.wantTier {
				t.Errorf("Tier.Name = %q, want %q", got.Tier.Name, tc.wantTier)
			}
			if got.ModelName != tc.wantModel {
				t.Errorf("ModelName = %q, want %q", got.ModelName, tc.wantModel)
			}
			if !strings.Contains(got.Reasoning, "VRAM") {
				t.Errorf("expected Reasoning to mention VRAM, got %q", got.Reasoning)
			}
		})
	}
}
