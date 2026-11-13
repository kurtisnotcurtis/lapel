package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"lapel/internal/agent"
	"lapel/internal/backend/wsl"
	"lapel/internal/config"
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

			fmt.Println("=== lapel run ===")

			// Load lapel.yaml if present to apply declarative defaults when CLI flags aren't explicitly set.
			m, manifestErr := config.LoadEffective("")
			if manifestErr == nil && m != nil {
				if !cmd.Flags().Changed("agent") && m.Agent.Name != "" {
					agentName = m.Agent.Name
				}
				if !cmd.Flags().Changed("user") && m.Jail.User != "" {
					user = m.Jail.User
				}
				if !cmd.Flags().Changed("timeout") && m.Jail.Processes.TimeoutSeconds > 0 {
					timeout = m.Jail.Processes.TimeoutSeconds
				}
				if !cmd.Flags().Changed("workspace") && m.Workspace.Path != "" {
					wsPath = m.Workspace.Path
				}
			}

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

			hostIP, err := provider.HostIP(ctx)
			if err != nil || hostIP == "" {
				fmt.Printf("  ⚠ Could not resolve WSL host gateway IP (%v); falling back to localhost\n", err)
				hostIP = "localhost"
			}
			providerURL := fmt.Sprintf("http://%s:%d", hostIP, runningProvider.Port)
			model := ""
			if manifestErr == nil && m != nil && m.Provider.Model != "" {
				model = m.Provider.Model
			} else if len(runningProvider.Models) > 0 {
				model = runningProvider.Models[0]
			} else {
				model = "default"
			}

			fmt.Printf("  ✓ Provider: %s at %s\n", runningProvider.Name, providerURL)
			fmt.Printf("  ✓ Model: %s\n", model)

			// Step 3: Configure agent environment
			fmt.Println("\nStep 3/4: Configuring agent environment...")
			env := make(map[string]string)
			selectedAgent.Configure(env, providerURL, model)

			for k, v := range env {
				fmt.Printf("  %s=%s\n", k, v)
			}

			// Build jail profile and apply it
			fsPolicy := map[string]string{
				windowsPathToLinux(wsPath): "read-write",
			}
			netPolicy := jail.NetworkUnrestricted
			profileName := "agent-session"
			if manifestErr == nil && m != nil {
				if m.Jail.Profile != "" {
					profileName = m.Jail.Profile
				}
				for k, v := range m.Jail.Filesystem {
					fsPolicy[windowsPathToLinux(k)] = v
				}
				if m.Jail.Network != "" {
					netPolicy = jail.NetworkConfig(m.Jail.Network)
				}
			}

			profile := jail.Profile{
				Name:       profileName,
				User:       user,
				Processes:  jail.ProcessConfig{TimeoutSeconds: timeout},
				Filesystem: fsPolicy,
				Network:    netPolicy,
			}

			enforcer := jail.NewEnforcer(profile)
			jailEnv := enforcer.Apply(env)

			fmt.Printf("\n  Jail profile: %s\n", profile.Name)
			fmt.Printf("  User: %s\n", user)
			fmt.Printf("  Timeout: %ds\n", timeout)

			// Step 4: Launch agent
			fmt.Println("\nStep 4/4: Launching agent...")

			cmdStr := selectedAgent.Binary()
			if cmdStr == "" {
				cmdStr = "bash"
			}

			req := domain.ExecRequest{
				Dist:       wsl.Dist,
				User:       user,
				WorkingDir: "/home/" + user,
				Cmd:        enforcer.WrapCommand([]string{"bash", "-c", cmdStr}),
				Env:        jailEnv,
				Stdin:      true,
			}

			fmt.Printf("  Launching: %s\n\n", cmdStr)

			session, err := b.Launch(ctx, req)
			if err != nil {
				return fmt.Errorf("launch failed: %w", err)
			}

			fmt.Printf("\nSession started (PID: %d)\n", session.PID())
			fmt.Println("Press Ctrl+C to stop.")

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
