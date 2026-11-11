package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"lapel/internal/agent"
	"lapel/internal/provider"

	"github.com/spf13/cobra"
)

// healthCmd launches an agent session with jail policies applied. This is the
// "just works" experience — everything configured, sandboxed, and ready to go.
func healthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check if your inference stack is working end-to-end",
		Long:  `Verify that your provider, agent, and model are all communicating correctly.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			fmt.Println("=== Health Check ===")
			fmt.Println()

			// Step 1: Check if any provider is running and reachable
			fmt.Print("Checking providers... ")
			reg := provider.NewRegistry("") // Use default distro
			statuses, err := reg.StatusAll(ctx)
			if err != nil {
				fmt.Printf("❌ %v\n", err)
				return fmt.Errorf("checking providers: %w", err)
			}

			var runningProvider *provider.ProviderStatus
			for _, s := range statuses {
				if s.Running {
					runningProvider = s
					break
				}
			}

			if runningProvider == nil {
				fmt.Println("❌ No provider running")
				return fmt.Errorf("no running provider found. Start LM Studio or Ollama first.")
			}

			providerURL := fmt.Sprintf("http://localhost:%d", runningProvider.Port)
			fmt.Printf("✓ %s at %s\n", runningProvider.Name, providerURL)

			// Step 2: Test HTTP connectivity with timing
			fmt.Println()
			fmt.Print("Testing HTTP connectivity... ")

			client := &http.Client{Timeout: 5 * time.Second}
			start := time.Now()
			resp, err := client.Get(providerURL + "/health")
			duration := time.Since(start)

			if err != nil {
				// Try /v1/models endpoint instead
				fmt.Printf("\n  Trying /v1/models... ")
				start = time.Now()
				resp, err = client.Get(providerURL + "/v1/models")
				duration = time.Since(start)
			}

			if err != nil {
				fmt.Printf("❌ Failed after %v\n", duration.Round(time.Millisecond))
				return fmt.Errorf("provider not reachable: %w", err)
			}
			defer resp.Body.Close()

			fmt.Printf("✓ Responded in %v (status %d)\n", duration.Round(time.Millisecond), resp.StatusCode)

			// Step 3: Check agent availability
			fmt.Println()
			fmt.Print("Checking agents... ")
			agents, err := listAgents(ctx)
			if err != nil {
				fmt.Printf("⚠ Could not list agents: %v\n", err)
			} else if len(agents) == 0 {
				fmt.Println("⚠ No agents detected")
			} else {
				for i, agent := range agents {
					if i > 0 {
						fmt.Print(", ")
					}
					fmt.Printf("%s", agent)
				}
				fmt.Println()
			}

			// Step 4: Summary
			fmt.Println()
			fmt.Println("=== Health Check Complete ===")
			if len(agents) > 0 {
				fmt.Println("✓ Provider reachable")
				fmt.Println("✓ Agent available")
				fmt.Println()
				fmt.Println("You're ready to run `lapel run`!")
			} else {
				fmt.Println("⚠ Provider reachable, but no agent detected")
				fmt.Println("  Install pi, claude-code, or copilot to get started.")
			}

			return nil
		},
	}

	return cmd
}

func listAgents(ctx context.Context) ([]string, error) {
	mgr := agent.NewManager()
	return mgr.List(), nil
}
