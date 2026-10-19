package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"lapel/community"
	"lapel/internal/backend/wsl"
	"lapel/internal/features"
	"lapel/internal/provider"
)

const benchmarkPrompt = "Write a concise Go function named ReverseInts that reverses a slice of ints in place."

// BenchmarkResult captures the measured outcome of a provider benchmark run.
type BenchmarkResult struct {
	ProviderName     string
	BaseURL          string
	ModelName        string
	CompletionTokens int
	Duration         time.Duration
	TokensPerSec     float64
	VRAMGB           float64
	BatchSize        int
}

func benchmarkCmd() *cobra.Command {
	var (
		endpointURL string
		modelName   string
		providerStr string
		batchSize   int
		share       bool
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run a standardized coding prompt benchmark against your active local provider",
		Long: `Measures real tokens/second and hardware profile metrics against your active
LM Studio or Ollama server, prints a Rig Benchmark Card, and optionally shares
the verified report when --share is passed (requires LAPEL_COMMUNITY_ENABLED=true).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			profile, hwInfo, _ := buildHardwareProfile(ctx)
			hash := profile.Hash()

			vramGB := 0.0
			gpuLabel := "CPU-only"
			if hwInfo != nil && hwInfo.GPU.Name != "" {
				vramGB = hwInfo.GPU.VRAMGB
				gpuLabel = fmt.Sprintf("%s (%.1f GB VRAM)", hwInfo.GPU.Name, hwInfo.GPU.VRAMGB)
			}

			baseURL := strings.TrimRight(strings.TrimSpace(endpointURL), "/")
			provName := providerStr

			if baseURL == "" {
				reg := provider.NewRegistry(wsl.Dist)
				statuses, _ := reg.StatusAll(ctx)
				for _, s := range statuses {
					if s.Running {
						baseURL = fmt.Sprintf("http://127.0.0.1:%d", s.Port)
						if provName == "" {
							provName = s.Name
						}
						if modelName == "" && len(s.Models) > 0 {
							modelName = s.Models[0]
						}
						break
					}
				}
			}
			if baseURL == "" {
				baseURL = "http://127.0.0.1:1234"
			}
			if provName == "" {
				provName = "LM Studio"
			}
			if modelName == "" {
				modelName = "local-model"
			}

			fmt.Println("=== lapel benchmark ===")
			fmt.Printf("  Hardware: %s | %d GB RAM (hash: %s)\n", gpuLabel, profile.RAM, hash)
			fmt.Printf("  Target:   %s (%s) — model %q\n", provName, baseURL, modelName)
			fmt.Println("  Running standardized coding prompt...")

			res, err := runProviderBenchmark(ctx, baseURL, modelName, provName, vramGB, batchSize)
			if err != nil {
				return fmt.Errorf("benchmark probe failed against %s: %w (ensure LM Studio or Ollama is running with a loaded model)", baseURL, err)
			}

			fmt.Println("\n┌──────────────────────────────────────────────────────────────┐")
			fmt.Printf("│ ⚡ LAPEL RIG BENCHMARK CARD                                  │\n")
			fmt.Println("├──────────────────────────────────────────────────────────────┤")
			fmt.Printf("│ Hardware Hash : %-44s │\n", hash)
			fmt.Printf("│ GPU / VRAM    : %-44s │\n", truncateStr(gpuLabel, 44))
			fmt.Printf("│ Model         : %-44s │\n", truncateStr(res.ModelName+" ("+res.ProviderName+")", 44))
			fmt.Printf("│ Throughput    : %-44s │\n", fmt.Sprintf("%.1f tok/s (%d tokens in %v)", res.TokensPerSec, res.CompletionTokens, res.Duration.Round(time.Millisecond)))
			fmt.Println("└──────────────────────────────────────────────────────────────┘")

			report := community.Report{
				HardwareHash: hash,
				ReportedAt:   time.Now().UTC(),
				Models: []community.ModelPerf{
					{
						Name:      res.ModelName,
						Provider:  res.ProviderName,
						VRAMUsed:  res.VRAMGB,
						BatchSize: res.BatchSize,
						TokenRate: res.TokensPerSec,
					},
				},
			}

			if dryRun {
				data, _ := json.MarshalIndent(report, "", "  ")
				fmt.Printf("\n[dry-run] Verified Report Payload:\n%s\n", string(data))
				return nil
			}

			if share {
				if !features.IsEnabled(features.CommunityEnabled) {
					fmt.Println("\nℹ Community sharing is disabled by default (opt-in).")
					fmt.Println("  Re-run with LAPEL_COMMUNITY_ENABLED=true lapel benchmark --share to publish.")
					return nil
				}
				targetEndpoint := features.GetEndpoint()
				fmt.Printf("\nPublishing verified benchmark report to %s...\n", targetEndpoint)
				if err := shareReport(targetEndpoint, report); err != nil {
					fmt.Printf("⚠ Could not publish to community backend: %v\n", err)
					return nil
				}
				fmt.Println("✓ Verified benchmark published!")
			} else {
				fmt.Println("\nTip: Pass --share (with LAPEL_COMMUNITY_ENABLED=true) to contribute this benchmark to the community index.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&endpointURL, "endpoint", "", "OpenAI-compatible provider base URL (default: auto-detect running provider)")
	cmd.Flags().StringVar(&modelName, "model", "", "Model identifier to benchmark (default: active provider model)")
	cmd.Flags().StringVar(&providerStr, "provider", "", "Provider label ('LM Studio' or 'Ollama')")
	cmd.Flags().IntVar(&batchSize, "batch-size", 16, "Configured physical batch size metadata")
	cmd.Flags().BoolVar(&share, "share", false, "Submit the measured benchmark report to the community backend (opt-in)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the measured JSON report payload without submitting")

	return cmd
}

// benchmarkHTTPClient is the HTTP client used by runProviderBenchmark (overridable in tests).
var benchmarkHTTPClient = &http.Client{Timeout: 60 * time.Second}

// runProviderBenchmark sends a standardized chat completion request to baseURL
// and measures actual token throughput.
func runProviderBenchmark(ctx context.Context, baseURL, modelName, providerName string, vramGB float64, batchSize int) (*BenchmarkResult, error) {
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	url += "/chat/completions"

	reqBody := map[string]any{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a concise coding assistant."},
			{"role": "user", "content": benchmarkPrompt},
		},
		"max_tokens":  128,
		"temperature": 0,
		"stream":      false,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := benchmarkHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	elapsed := time.Since(start)
	if elapsed <= 0 {
		elapsed = time.Millisecond
	}

	tokens := parsed.Usage.CompletionTokens
	if tokens <= 0 && len(parsed.Choices) > 0 {
		// Estimate tokens (~4 chars/token) if the server omits usage.completion_tokens.
		content := strings.TrimSpace(parsed.Choices[0].Message.Content)
		tokens = (len(content) + 3) / 4
		if tokens == 0 && len(content) > 0 {
			tokens = 1
		}
	}
	if parsed.Model != "" && (modelName == "" || modelName == "local-model") {
		modelName = parsed.Model
	}

	tokPerSec := float64(tokens) / elapsed.Seconds()
	return &BenchmarkResult{
		ProviderName:     providerName,
		BaseURL:          baseURL,
		ModelName:        modelName,
		CompletionTokens: tokens,
		Duration:         elapsed,
		TokensPerSec:     tokPerSec,
		VRAMGB:           vramGB,
		BatchSize:        batchSize,
	}, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
