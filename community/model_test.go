package community

import (
	"encoding/json"
	"testing"
)

// TestReportJSONRoundTrip is the shared-schema contract: any struct under this
// package must survive a JSON encode/decode without losing fields. Both the CLI
// and the private backend depend on it, so this guards against silent drift —
// the "internal test infra" that keeps the shared layer compatible (Chrome model).
func TestReportJSONRoundTrip(t *testing.T) {
	original := Report{
		HardwareHash: "abc123def456",
		Models: []ModelPerf{
			{Name: "Ornith 35B", Provider: "local", VRAMUsed: 18.2, BatchSize: 16, TokenRate: 42.5},
		},
		Agents: []AgentPerf{
			{Name: "pi", Provider: "local", ResponseTimeSec: 2.5, TokenRate: 60.0, ContextWindow: 32768},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.HardwareHash != original.HardwareHash {
		t.Errorf("hardware hash lost: got %q want %q", decoded.HardwareHash, original.HardwareHash)
	}
	if len(decoded.Models) != 1 || decoded.Models[0].Name != "Ornith 35B" {
		t.Errorf("model not preserved: %+v", decoded.Models)
	}
	if decoded.Models[0].TokenRate != 42.5 {
		t.Errorf("token rate lost: got %v want 42.5", decoded.Models[0].TokenRate)
	}
	if len(decoded.Agents) != 1 || decoded.Agents[0].Name != "pi" {
		t.Errorf("agent not preserved: %+v", decoded.Agents)
	}
	if decoded.Agents[0].TokenRate != 60.0 {
		t.Errorf("agent token rate lost: got %v want 60.0", decoded.Agents[0].TokenRate)
	}
}

// TestRecommendationJSONRoundTrip ensures recommendations survive transport.
func TestRecommendationJSONRoundTrip(t *testing.T) {
	rec := Recommendation{
		Type:        "model",
		Title:       "Ornith 35B",
		Description: "Popular on similar hardware",
		Confidence:  0.9,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Recommendation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Title != rec.Title || decoded.Confidence != 0.9 {
		t.Errorf("recommendation lost fields: %+v -> %+v", rec, decoded)
	}
}

func TestHardwareProfileHash_SimilarityPrefix(t *testing.T) {
	rigA := HardwareProfile{
		OS:       "Windows",
		RAM:      32,
		CPUModel: "AMD Ryzen 9 7950X",
		CPUCores: 16,
		GPUs:     []GPUInfo{{Name: "NVIDIA GeForce RTX 4090", VRAMGB: 24}},
	}
	rigB := HardwareProfile{
		OS:       "Windows",
		RAM:      32,
		CPUModel: "Intel Core i9-14900K",
		CPUCores: 24,
		GPUs:     []GPUInfo{{Name: "NVIDIA GeForce RTX 4090", VRAMGB: 24}},
	}
	rigDifferentGPU := HardwareProfile{
		OS:       "Windows",
		RAM:      32,
		CPUModel: "AMD Ryzen 9 7950X",
		CPUCores: 16,
		GPUs:     []GPUInfo{{Name: "NVIDIA GeForce RTX 4070", VRAMGB: 12}},
	}

	hashA := rigA.Hash()
	hashB := rigB.Hash()
	hashDiff := rigDifferentGPU.Hash()

	if len(hashA) != 16 || len(hashB) != 16 {
		t.Fatalf("expected 16-char hash, got %q and %q", hashA, hashB)
	}
	// Same GPU + VRAM + RAM should share the 8-char similarity prefix.
	if hashA[:8] != hashB[:8] {
		t.Errorf("expected matching 8-char prefix for same GPU/VRAM/RAM tier: %q vs %q", hashA, hashB)
	}
	// Different CPUs should still yield distinct full hashes.
	if hashA == hashB {
		t.Errorf("expected distinct full hash for different CPUs: %q", hashA)
	}
	// Different GPU/VRAM tier must have a different prefix.
	if hashA[:8] == hashDiff[:8] {
		t.Errorf("expected different 8-char prefix for different GPU/VRAM tier: %q vs %q", hashA, hashDiff)
	}
}
