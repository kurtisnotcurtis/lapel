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
	"lapel/internal/config"
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
		distro      string
		endpointURL string
		modelName   string
		providerStr string
		batchSize   int
		noWarmup    bool
		share       bool
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run a standardized coding prompt benchmark against your active local provider",
		Long: `Measures warm end-to-end tokens/second and hardware profile metrics against your
active LM Studio or Ollama server, prints a Rig Benchmark Card, and optionally
shares the verified report when --share is passed (requires
LAPEL_COMMUNITY_ENABLED=true and LAPEL_COMMUNITY_ENDPOINT).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			targetDist := resolveDistro(distro, m)

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
				reg := provider.NewRegistry(targetDist)
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
			if !noWarmup {
				fmt.Println("  Warming up model (1 token)...")
			}
			fmt.Println("  Running standardized coding prompt...")

			res, err := runProviderBenchmark(ctx, baseURL, modelName, provName, vramGB, batchSize, !noWarmup)
			if err != nil {
				return fmt.Errorf("benchmark probe failed against %s: %w (ensure LM Studio or Ollama is running with a loaded model)", baseURL, err)
			}

			if _, postHW, postErr := buildHardwareProfile(ctx); postErr == nil && postHW != nil && postHW.GPU.VRAMUsedGB > 0 {
				res.VRAMGB = postHW.GPU.VRAMUsedGB
				gpuLabel = fmt.Sprintf("%s (%.1f / %.1f GB VRAM)", postHW.GPU.Name, postHW.GPU.VRAMUsedGB, postHW.GPU.VRAMGB)
			} else if hwInfo != nil && hwInfo.GPU.VRAMUsedGB > 0 {
				res.VRAMGB = hwInfo.GPU.VRAMUsedGB
				gpuLabel = fmt.Sprintf("%s (%.1f / %.1f GB VRAM)", hwInfo.GPU.Name, hwInfo.GPU.VRAMUsedGB, hwInfo.GPU.VRAMGB)
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
					fmt.Println("  Re-run with LAPEL_COMMUNITY_ENABLED=true LAPEL_COMMUNITY_ENDPOINT=<url> lapel benchmark --share to publish.")
					return nil
				}
				targetEndpoint := features.GetEndpoint()
				if targetEndpoint == "" {
					return fmt.Errorf("no community endpoint configured; set LAPEL_COMMUNITY_ENDPOINT=<url> (or use --dry-run)")
				}
				fmt.Printf("\nPublishing verified benchmark report to %s...\n", targetEndpoint)
				if err := shareReport(targetEndpoint, report); err != nil {
					return fmt.Errorf("publishing to community backend: %w", err)
				}
				fmt.Println("✓ Verified benchmark published!")
			} else {
				fmt.Println("\nTip: Pass --share (with LAPEL_COMMUNITY_ENABLED=true and LAPEL_COMMUNITY_ENDPOINT) to publish this benchmark.")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().StringVar(&endpointURL, "endpoint", "", "OpenAI-compatible provider base URL (default: auto-detect running provider)")
	cmd.Flags().StringVar(&modelName, "model", "", "Model identifier to benchmark (default: active provider model)")
	cmd.Flags().StringVar(&providerStr, "provider", "", "Provider label ('LM Studio' or 'Ollama')")
	cmd.Flags().IntVar(&batchSize, "batch-size", 16, "Configured physical batch size metadata")
	cmd.Flags().BoolVar(&noWarmup, "no-warmup", false, "skip the 1-token warmup request before timing")
	cmd.Flags().BoolVar(&share, "share", false, "Submit the measured benchmark report to the community backend (opt-in)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the measured JSON report payload without submitting")

	return cmd
}

// benchmarkHTTPClient is the HTTP client used by runProviderBenchmark (overridable in tests).
var benchmarkHTTPClient = &http.Client{Timeout: 60 * time.Second}

func completionURL(baseURL string) string {
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	return url + "/chat/completions"
}

// runProviderBenchmark sends an optional 1-token warmup request (to exclude
// connection setup and cold model loading from the timed sample), then sends a
// standardized chat completion request to baseURL and measures token throughput.
func runProviderBenchmark(ctx context.Context, baseURL, modelName, providerName string, vramGB float64, batchSize int, warmup bool) (*BenchmarkResult, error) {
	url := completionURL(baseURL)

	if warmup {
		warmupBody, _ := json.Marshal(map[string]any{
			"model": modelName,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
			"max_tokens":  1,
			"temperature": 0,
			"stream":      false,
		})
		if wreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(warmupBody)); err == nil {
			wreq.Header.Set("Content-Type", "application/json")
			if wresp, err := benchmarkHTTPClient.Do(wreq); err == nil {
				_ = wresp.Body.Close()
			}
		}
	}

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
