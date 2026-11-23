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
	"lapel/internal/features"
	"lapel/internal/hardware"
)

// communityHTTPClient disables automatic redirect following so a DNS redirect
// (e.g. a parking page or marketing site returning 302 -> 200 HTML) can never
// be mistaken for an accepted API request.
var communityHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// hashPrefix returns up to n characters of h without panicking on short strings.
func hashPrefix(h string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(h) <= n {
		return h
	}
	return h[:n]
}

// communityCmd is the parent command for community features.
func communityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "community",
		Short: "Manage opt-in community hardware profiles, benchmarks, and tuning insights",
		Long: `Query verified hardware tuning profiles or share your benchmark results with
a configured community backend (strictly opt-in via LAPEL_COMMUNITY_ENABLED=true
and LAPEL_COMMUNITY_ENDPOINT=<url>).

Commands:
  status     - View your detected hardware hash and community connection status
  benchmark  - Run a standardized coding prompt benchmark against your local provider
  share      - Share session benchmark data with the community
  insights   - Get tuning recommendations from users with matching hardware`,
	}
	cmd.AddCommand(
		communityStatusCmd(),
		benchmarkCmd(),
		communityShareCmd(),
		communityInsightsCmd(),
	)
	return cmd
}

// buildHardwareProfile detects the local system's hardware and converts it into
// a community.HardwareProfile with a deterministic similarity hash.
func buildHardwareProfile(ctx context.Context) (community.HardwareProfile, *hardware.Info, error) {
	hwInfo, err := hardware.Detect(ctx)
	if err != nil {
		return community.HardwareProfile{}, nil, err
	}

	profile := community.HardwareProfile{
		OS:         hwInfo.OS,
		RAM:        int64(hwInfo.RAM.TotalGB),
		CPUModel:   hwInfo.CPU.Name,
		CPUCores:   hwInfo.CPU.Cores,
		DetectedAt: time.Now(),
	}
	if hwInfo.GPU.Name != "" {
		profile.GPUs = append(profile.GPUs, community.GPUInfo{
			Name:        hwInfo.GPU.Name,
			VRAMGB:      int(hwInfo.GPU.VRAMGB),
			CUDAVersion: hwInfo.GPU.Compute,
		})
	}
	return profile, hwInfo, nil
}

// communityStatusCmd shows your profile and community feature status.
func communityStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "View your hardware profile hash and community feature status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			profile, _, err := buildHardwareProfile(ctx)
			hash := "unknown"
			if err == nil {
				hash = profile.Hash()
			}

			if !features.IsEnabled(features.CommunityEnabled) {
				fmt.Println("Community features: disabled (default opt-in privacy mode)")
				fmt.Printf("Local hardware hash: %s\n", hash)
				fmt.Println("\nTo enable opt-in community tuning insights and benchmark sharing:")
				fmt.Println("  LAPEL_COMMUNITY_ENABLED=true LAPEL_COMMUNITY_ENDPOINT=<url> lapel community status")
				return nil
			}

			endpoint := features.GetEndpoint()
			endpointDisplay := endpoint
			if endpointDisplay == "" {
				endpointDisplay = "(unset — set LAPEL_COMMUNITY_ENDPOINT or LAPEL_USE_OFFICIAL_BACKEND=false)"
			}
			fmt.Println("Community features: enabled (opt-in)")
			fmt.Printf("Target endpoint:    %s\n", endpointDisplay)
			fmt.Printf("Hardware hash:      %s (tier prefix: %s)\n", hash, hashPrefix(hash, 8))
			if len(profile.GPUs) > 0 {
				fmt.Printf("Detected GPU:       %s (%d GB VRAM)\n", profile.GPUs[0].Name, profile.GPUs[0].VRAMGB)
			}
			fmt.Printf("System RAM:         %d GB\n", profile.RAM)

			return nil
		},
	}
}

// communityShareCmd shares current session performance data.
func communityShareCmd() *cobra.Command {
	var (
		modelName string
		provider  string
		vramUsed  float64
		batchSize int
		tokenRate float64
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "share",
		Short: "Share session benchmark data with a configured community backend (opt-in)",
		Long: `Record your model usage, VRAM consumption, batch size, and token rate.
Requires LAPEL_COMMUNITY_ENABLED=true and an explicit LAPEL_COMMUNITY_ENDPOINT
(or LAPEL_USE_OFFICIAL_BACKEND=false for a local self-hosted backend).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !features.IsEnabled(features.CommunityEnabled) {
				fmt.Println("Community features are disabled (strictly opt-in).")
				fmt.Println("Run with LAPEL_COMMUNITY_ENABLED=true to share benchmark reports.")
				return nil
			}

			if strings.TrimSpace(modelName) == "" {
				return fmt.Errorf("--model is required (e.g., --model=\"Ornith 35B\")")
			}

			ctx := cmd.Context()
			profile, _, err := buildHardwareProfile(ctx)
			hash := "unknown"
			if err == nil {
				hash = profile.Hash()
			}

			report := community.Report{
				HardwareHash: hash,
				ReportedAt:   time.Now().UTC(),
				Models: []community.ModelPerf{
					{
						Name:      modelName,
						Provider:  provider,
						VRAMUsed:  vramUsed,
						BatchSize: batchSize,
						TokenRate: tokenRate,
					},
				},
			}

			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return fmt.Errorf("marshaling report: %w", err)
			}

			fmt.Printf("Prepared benchmark report (hardware hash %s):\n%s\n", hash, string(data))

			if dryRun {
				fmt.Println("\n[dry-run] Report was not sent.")
				return nil
			}

			endpoint := features.GetEndpoint()
			if endpoint == "" {
				return fmt.Errorf("no community endpoint configured; set LAPEL_COMMUNITY_ENDPOINT=<url> (or use --dry-run)")
			}
			fmt.Printf("\nSubmitting report to %s...\n", endpoint)
			if err := shareReport(endpoint, report); err != nil {
				return fmt.Errorf("submitting report to %s: %w", endpoint, err)
			}

			fmt.Println("✓ Benchmark report submitted. Thank you for contributing!")
			return nil
		},
	}

	cmd.Flags().StringVar(&modelName, "model", "", "Model name (required, e.g., 'Ornith 35B')")
	cmd.Flags().StringVar(&provider, "provider", "LM Studio", "Inference provider ('LM Studio' or 'Ollama')")
	cmd.Flags().Float64Var(&vramUsed, "vram", 0, "VRAM used in GB")
	cmd.Flags().IntVar(&batchSize, "batch-size", 0, "Physical batch size")
	cmd.Flags().Float64Var(&tokenRate, "token-rate", 0, "Token rate (tokens/second)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the JSON payload without sending it")

	return cmd
}

// communityInsightsCmd shows recommendations from similar users.
func communityInsightsCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Get hardware-matched tuning recommendations",
		Long: `Fetch verified tuning recommendations for your GPU and RAM tier from a
configured community backend, falling back to local hardware heuristics when offline.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			profile, hwInfo, hwErr := buildHardwareProfile(ctx)
			hash := "unknown"
			if hwErr == nil {
				hash = profile.Hash()
			}

			var recommendations []community.Recommendation
			sourceLabel := "local hardware heuristic (offline)"

			if features.IsEnabled(features.CommunityEnabled) {
				endpoint := features.GetEndpoint()
				if endpoint == "" {
					fmt.Println("  ℹ No LAPEL_COMMUNITY_ENDPOINT configured; using local hardware heuristic.")
				} else {
					fmt.Printf("Querying tuning profiles from %s (hardware hash: %s)...\n", endpoint, hash)
					recs, err := fetchRecommendations(endpoint, hash, limit)
					if err == nil && len(recs) > 0 {
						recommendations = recs
						sourceLabel = fmt.Sprintf("community backend (%s)", endpoint)
					} else if err != nil {
						fmt.Printf("  ℹ Backend unreachable (%v); using local hardware heuristic.\n", err)
					} else {
						fmt.Println("  ℹ No matching community reports found yet; using local hardware heuristic.")
					}
				}
			} else {
				fmt.Println("ℹ Community network disabled (opt-in); computing local hardware tuning heuristic.")
			}

			if len(recommendations) == 0 && hwInfo != nil {
				rec := hardware.NewDefaultRecommender().Recommend(hwInfo)
				recommendations = []community.Recommendation{
					{
						Type:        "model",
						Title:       fmt.Sprintf("%s (%s tier)", rec.ModelName, rec.Tier.Name),
						Description: strings.ReplaceAll(strings.TrimSpace(rec.Reasoning), "\n", " "),
						Confidence:  0.80,
						SourceCount: 1,
						MatchReason: fmt.Sprintf("Local hardware heuristic (%.1f GB VRAM, %.1f GB RAM)", hwInfo.GPU.VRAMGB, hwInfo.RAM.TotalGB),
					},
				}
			}

			fmt.Printf("\n=== Tuning Recommendations [%s] ===\n", sourceLabel)
			for i, rec := range recommendations {
				if limit > 0 && i >= limit {
					break
				}
				fmt.Printf("\n%d. [%s] %s\n", i+1, rec.Type, rec.Title)
				fmt.Printf("   %s (confidence: %.0f%%, sources: %d)\n",
					rec.Description, rec.Confidence*100, rec.SourceCount)
				fmt.Printf("   Match reason: %s\n", rec.MatchReason)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum number of recommendations to show")

	return cmd
}

// normalizeAPIEndpoint strips a trailing /v1 or / so callers can safely append /api/v1/...
func normalizeAPIEndpoint(endpoint string) string {
	ep := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	ep = strings.TrimSuffix(ep, "/api/v1")
	ep = strings.TrimSuffix(ep, "/v1")
	return ep
}

// fetchRecommendations makes an HTTP request to the backend API without following redirects.
func fetchRecommendations(endpoint string, hash string, limit int) ([]community.Recommendation, error) {
	base := normalizeAPIEndpoint(endpoint)
	if base == "" {
		return nil, fmt.Errorf("community endpoint is empty")
	}
	url := fmt.Sprintf("%s/api/v1/recommendations?hash=%s&limit=%d", base, hash, limit)

	resp, err := communityHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("refused HTTP %d redirect to %q (check LAPEL_COMMUNITY_ENDPOINT)", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var recommendations []community.Recommendation
	if err := json.NewDecoder(resp.Body).Decode(&recommendations); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return recommendations, nil
}

// shareReport sends a performance report to the backend API without following redirects.
func shareReport(endpoint string, report community.Report) error {
	base := normalizeAPIEndpoint(endpoint)
	if base == "" {
		return fmt.Errorf("community endpoint is empty")
	}
	url := fmt.Sprintf("%s/api/v1/reports", base)

	data, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	resp, err := communityHTTPClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("refused HTTP %d redirect to %q (check LAPEL_COMMUNITY_ENDPOINT)", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return nil
}
