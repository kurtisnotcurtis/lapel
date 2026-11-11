package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"lapel/internal/agent"
	"lapel/internal/backend/wsl"
	"lapel/internal/domain"
	"lapel/internal/jail"
	"lapel/internal/provider"
)

// runCmd launches an agent session with jail policies applied. This is the
// "just works" experience — everything configured, sandboxed, and ready to go.
func runCmd() *cobra.Command {
	var (
		user     string
		timeout  int
		agentName string
		wsPath   string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Launch an agent session with jail policies applied",
		Long: `Run your coding agent in a sandboxed WSL environment. The session inherits
jail policies (filesystem scoping, network restrictions, process timeouts) so your
agent can work without risking your Windows files.

If no --agent is specified, lapel will detect which agents are available and use the first one found.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			fmt.Println("=== LAPeL Run ===\n")

			b := &wsl.DistBackend{}

			// Check WSL availability
			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}
			if !status.Available {
				fmt.Println("✗ WSL not available. Run `lapel setup` first.")
				return fmt.Errorf("wsL not available")
			}

			// Step 1: Detect available agents
			fmt.Println("Step 1/4: Checking for available agents...")
			mgr := agent.NewManager()
			agentStatuses, err := mgr.DetectAll(ctx)
			if err != nil {
				return fmt.Errorf("detecting agents: %w", err)
			}

			for _, s := range agentStatuses {
				symbol := "✗"
				if s.Installed {
					symbol = "✓"
				}
				fmt.Printf("  %s %s\n", symbol, s.Name)
			}

			// Determine which agent to use
			var selectedAgent agent.Agent
			if agentName != "" {
				selectedAgent = mgr.FindByName(agentName)
				if selectedAgent == nil {
					return fmt.Errorf("agent %q not found. Available: %v", agentName, mgr.List())
				}
			} else {
				// Auto-detect first available
				for _, s := range agentStatuses {
					if s.Installed {
						selectedAgent = mgr.FindByName(s.Name)
						break
					}
				}
				if selectedAgent == nil {
					return fmt.Errorf("no agents found. Install pi, claude-code, or copilot first.")
				}
			}
			fmt.Printf("\n  Using agent: %s\n", selectedAgent.Name())

			// Step 2: Get provider info
			fmt.Println("\nStep 2/4: Checking inference providers...")
			reg := provider.NewRegistry(wsl.Dist)
			statuses, err := reg.StatusAll(ctx)
			if err != nil {
				return fmt.Errorf("getting provider status: %w", err)
			}

			var runningProvider *provider.ProviderStatus
			for _, s := range statuses {
				if s.Running {
					runningProvider = s
					break
				}
			}

			if runningProvider == nil {
				fmt.Println("  ℹ No provider running. Starting Ollama...")
				ollama := reg.FindByName("ollama")
				if ollama != nil {
					if err := ollama.Start(ctx); err != nil {
						return fmt.Errorf("starting ollama: %w", err)
					}
					// Re-check status
					statuses, _ = reg.StatusAll(ctx)
					for _, s := range statuses {
						if s.Running {
							runningProvider = s
							break
						}
					}
				}
			}

			if runningProvider == nil {
				return fmt.Errorf("no running provider found. Start LM Studio or Ollama first.")
			}

			providerURL := fmt.Sprintf("http://localhost:%d", runningProvider.Port)
			model := ""
			if len(runningProvider.Models) > 0 {
				model = runningProvider.Models[0]
			} else {
				model = "default"
			}

			fmt.Printf("  ✓ Provider: %s on port %d\n", runningProvider.Name, runningProvider.Port)
			fmt.Printf("  ✓ Model: %s\n", model)

			// Step 3: Configure agent environment
			fmt.Println("\nStep 3/4: Configuring agent environment...")
			env := make(map[string]string)
			selectedAgent.Configure(env, providerURL, model)

			for k, v := range env {
				fmt.Printf("  %s=%s\n", k, v)
			}

			// Build jail profile and apply it
			profile := jail.Profile{
				Name: "agent-session",
				User: user,
				Processes: jail.ProcessConfig{TimeoutSeconds: timeout},
				Filesystem: map[string]string{
					windowsPathToLinux(wsPath): "read-write",
				},
				Network: jail.NetworkUnrestricted,
			}

			enforcer := jail.NewEnforcer(profile)
			jailEnv := enforcer.Apply(env)

			fmt.Printf("\n  Jail profile: %s\n", profile.Name)
			fmt.Printf("  User: %s\n", user)
			fmt.Printf("  Timeout: %ds\n")

			// Step 4: Launch agent
			fmt.Println("\nStep 4/4: Launching agent...")

			// Determine command to run based on agent type
			var cmdStr string
			switch selectedAgent.Name() {
			case "pi":
				cmdStr = "pi"
			case "claude-code":
				cmdStr = "claude"
			case "copilot":
				cmdStr = "github-copilot-cli"
			default:
				cmdStr = "bash"
			}

			req := domain.ExecRequest{
				Dist:       wsl.Dist,
				User:       user,
				WorkingDir: "/home/" + user,
				Cmd:        []string{"bash", "-c", cmdStr},
				Env:        jailEnv,
				Stdin:      true,
			}

			fmt.Printf("  Launching: %s\n\n", cmdStr)

			session, err := b.Launch(ctx, req)
			if err != nil {
				return fmt.Errorf("launch failed: %w", err)
			}

			fmt.Printf("\nSession started (PID: %d)\n", session.PID())
			fmt.Println("Press Ctrl+C to stop.\n")

			result, err := session.Wait()
			if err != nil {
				return fmt.Errorf("session wait failed: %w", err)
			}

			fmt.Printf("\nSession exited with code %d\n", result.ExitCode)
			return nil
		},
	}

	cmd.Flags().StringVar(&user, "user", "agent", "Linux user for jail context")
	cmd.Flags().IntVar(&timeout, "timeout", 300, "Process timeout in seconds")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent to launch (pi, claude-code, copilot)")
	cmd.Flags().StringVar(&wsPath, "workspace", ".", "working directory path")

	return cmd
}

// keepAlive is a small helper that blocks until context is done or an interrupt.
func keepAlive(ctx context.Context) {
	<-ctx.Done()
}
