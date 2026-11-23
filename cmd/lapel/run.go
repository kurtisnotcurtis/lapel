package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"lapel/internal/agent"
	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/domain"
	"lapel/internal/jail"
	"lapel/internal/provider"
	"lapel/internal/workspace"
)

// runCmd launches an interactive agent session inside the managed WSL distro
// with full terminal TTY support, non-root user execution (`-u <jail.user>`),
// clean Linux PATH, and provider environment variables wired up.
func runCmd() *cobra.Command {
	var (
		distro    string
		user      string
		timeout   int
		agentName string
		wsPath    string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Launch an interactive agent session inside the sandboxed WSL environment",
		Long: `Run your coding agent in the managed WSL environment with full terminal
interactivity. The session runs as the configured non-root jail user (-u),
applies a Linux-only PATH, wires your Windows inference provider, and returns
the child process's exit code.

If no --agent is specified, lapel detects which agents are available and launches
the first one found (or falls back to an interactive login shell).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			fmt.Println("=== lapel run ===")

			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			if m != nil {
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

			targetDist := resolveDistro(distro, m)
			b := wsl.NewBackend(targetDist)

			// Check WSL availability and distro presence.
			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}
			if !status.Available {
				fmt.Println("✗ WSL not available. Install WSL2 via `wsl.exe --install` first.")
				return fmt.Errorf("WSL not available")
			}
			if status.Note != "" {
				fmt.Printf("✗ WSL distro %q is not provisioned yet. Run `lapel setup` first.\n", targetDist)
				return fmt.Errorf("distro %q not provisioned", targetDist)
			}

			// Step 1: Detect available agents.
			fmt.Println("Step 1/4: Checking for available agents...")
			mgr := agent.NewManager(targetDist)
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

			var selectedAgent agent.Agent
			if cmd.Flags().Changed("agent") && agentName != "" {
				selectedAgent = mgr.FindByName(agentName)
				if selectedAgent == nil {
					return fmt.Errorf("agent %q not found. Available: %v", agentName, mgr.List())
				}
			} else {
				if agentName != "" {
					for _, s := range agentStatuses {
						if s.Installed && s.Name == agentName {
							selectedAgent = mgr.FindByName(s.Name)
							break
						}
					}
				}
				if selectedAgent == nil {
					for _, s := range agentStatuses {
						if s.Installed {
							selectedAgent = mgr.FindByName(s.Name)
							break
						}
					}
				}
			}
			if selectedAgent != nil {
				fmt.Printf("\n  Using agent: %s (%s)\n", selectedAgent.Name(), selectedAgent.Binary())
			} else {
				fmt.Println("\n  ℹ No coding agent CLI installed yet in WSL — launching interactive jailed shell.")
				fmt.Println("    (Tip: install an agent inside the shell via `npm i -g @anthropic-ai/claude-code` or `npm i -g @github/copilot`)")
			}

			// Step 2: Check running inference providers.
			fmt.Println("\nStep 2/4: Checking inference providers...")
			reg := provider.NewRegistry(targetDist)
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
				return fmt.Errorf("no running inference provider found; start LM Studio (port 1234) or Ollama (port 11434) on Windows first")
			}

			hostIP, err := provider.HostIP(ctx, targetDist)
			if err != nil || hostIP == "" {
				fmt.Printf("  ⚠ Could not resolve WSL host gateway IP (%v); falling back to localhost\n", err)
				hostIP = "localhost"
			}
			providerURL := fmt.Sprintf("http://%s:%d", hostIP, runningProvider.Port)
			model := ""
			if m != nil && m.Provider.Model != "" {
				model = m.Provider.Model
			} else if len(runningProvider.Models) > 0 {
				model = runningProvider.Models[0]
			} else {
				model = "default"
			}

			fmt.Printf("  ✓ Provider: %s at %s\n", runningProvider.Name, providerURL)
			fmt.Printf("  ✓ Model: %s\n", model)

			// Step 3: Configure agent and jail environment.
			fmt.Println("\nStep 3/4: Configuring agent environment...")
			env := make(map[string]string)
			if selectedAgent != nil {
				selectedAgent.Configure(env, providerURL, model)
			} else {
				mgr.FindByName("pi").Configure(env, providerURL, model)
				mgr.FindByName("claude-code").Configure(env, providerURL, model)
			}

			for k, v := range env {
				fmt.Printf("  %s=%s\n", k, v)
			}

			fsPolicy := map[string]string{
				windowsPathToLinux(wsPath): "read-write",
			}
			netPolicy := jail.NetworkUnrestricted
			profileName := "agent-session"
			if m != nil {
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

			fmt.Printf("\n  Distro:  %s\n", targetDist)
			fmt.Printf("  Profile: %s\n", profile.Name)
			fmt.Printf("  User:    %s\n", user)
			if timeout > 0 {
				fmt.Printf("  Timeout: %ds\n", timeout)
			} else {
				fmt.Printf("  Timeout: none (interactive)\n")
			}

			// Step 4: Launch interactive session attached to the current terminal.
			fmt.Println("\nStep 4/4: Launching session...")

			launchCmd := []string{"bash", "-l"}
			launchLabel := "bash -l"
			if selectedAgent != nil && selectedAgent.Binary() != "" {
				launchLabel = selectedAgent.Binary()
				launchCmd = []string{"bash", "-lc", "exec " + launchLabel}
			}

			workDir := windowsPathToLinux(wsPath)
			wsName := filepath.Base(wsPath)
			if m != nil && m.Workspace.Name != "" {
				wsName = m.Workspace.Name
			}
			if wsName == "" || wsName == "." {
				wsName = "project"
			}
			wsMgr := workspace.NewManager(targetDist, user)
			importMode := workspace.ModeBind
			if m != nil && m.Workspace.ImportMode != "" {
				importMode = workspace.ImportMode(m.Workspace.ImportMode)
			}
			if wsMgr.Exists(wsName, importMode) {
				workDir = wsMgr.Path(wsName, importMode)
			}

			req := domain.ExecRequest{
				Dist:       targetDist,
				User:       user,
				WorkingDir: workDir,
				Cmd:        enforcer.WrapCommand(launchCmd),
				Env:        jailEnv,
			}

			fmt.Printf("  Launching: %s\n\n", launchLabel)

			exitCode, err := b.RunInteractive(ctx, req)
			if err != nil {
				return fmt.Errorf("launch failed: %w", err)
			}
			if exitCode != 0 {
				return &ExitCodeError{Code: exitCode}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().StringVar(&user, "user", "agent", "Linux user for jail context")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "optional process timeout in seconds (0 = no timeout for interactive sessions)")
	cmd.Flags().StringVar(&agentName, "agent", "", "agent to launch (pi, claude-code, copilot)")
	cmd.Flags().StringVar(&wsPath, "workspace", ".", "working directory path")

	return cmd
}
