package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"lapel/community"
	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/domain"
	"lapel/internal/features"
	"lapel/internal/hardware"
	"lapel/internal/jail"
	"lapel/internal/provider"
	"lapel/internal/workspace"
)

// setupCmd is the one-command guided setup experience. It chains:
//  1. Quick doctor check (WSL availability, GPU)
//  2. Provider reachability check
//  3. Provision the target WSL distro if needed
//  4. Import workspace and summarize jail controls applied at `lapel run`
func setupCmd() *cobra.Command {
	var (
		distro        string
		workspacePath string
		user          string
		force         bool
		systemd       bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-command guided setup: provision distro, import workspace, inspect jail controls",
		Long: `Set up lapel in one command. Checks WSL availability, provisions the
managed distro if needed, imports your current project directory (or a path you
specify), and summarizes the jail controls enforced during 'lapel run'.

If you're already set up, run setup again to refresh — it's idempotent.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			fmt.Println("=== lapel setup ===")

			m, err := config.LoadOptional("")
			if err != nil {
				return err
			}
			targetDist := resolveDistro(distro, m)

			// Show feature flag status.
			if features.IsEnabled(features.CommunityEnabled) {
				fmt.Printf("\n🌐 Community features enabled\n")
				if ep := features.GetEndpoint(); ep != "" {
					fmt.Printf("   Community endpoint: %s\n", ep)
				} else {
					fmt.Printf("   Community endpoint: unset (set LAPEL_COMMUNITY_ENDPOINT to share/query)\n")
				}
			} else {
				fmt.Println("\n🔒 Community features disabled")
			}

			// Step 1: Quick doctor check.
			fmt.Println("Step 1/4: Checking WSL environment...")
			b := wsl.NewBackend(targetDist)
			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}

			if !status.Available {
				fmt.Printf("  ✗ WSL not available. Install WSL2 via `wsl.exe --install` first.\n")
				return fmt.Errorf("WSL not available")
			}

			fmt.Printf("  ✓ WSL available (%s %s)\n", status.Name, status.Version)
			if status.Note != "" {
				fmt.Printf("  ℹ %s\n", status.Note)
			} else {
				gStatus, err := wsl.CheckGPUStatus(ctx, targetDist)
				if err == nil && len(gStatus.GPUs) > 0 {
					fmt.Printf("  ✓ GPU detected: %s\n", gStatus.GPUs[0])
				}
			}

			// Detect hardware for community profile (if enabled).
			if features.IsEnabled(features.CommunityEnabled) {
				fmt.Println("\nDetecting hardware profile...")
				hwInfo, err := hardware.Detect(ctx)
				if err == nil {
					profile := community.HardwareProfile{
						OS:       hwInfo.OS,
						RAM:      int64(hwInfo.RAM.TotalGB),
						CPUModel: hwInfo.CPU.Name,
						CPUCores: hwInfo.CPU.Cores,
					}
					if hwInfo.GPU.Name != "" {
						profile.GPUs = append(profile.GPUs, community.GPUInfo{
							Name:        hwInfo.GPU.Name,
							VRAMGB:      int(hwInfo.GPU.VRAMGB),
							CUDAVersion: hwInfo.GPU.Compute,
						})
					}
					fmt.Printf("  ✓ Hardware profile created (%d GB RAM", profile.RAM)
					if len(profile.GPUs) > 0 {
						fmt.Printf(", %s with %d GB VRAM", profile.GPUs[0].Name, profile.GPUs[0].VRAMGB)
					}
					fmt.Printf(", hash: %s)\n", profile.Hash())
				} else {
					fmt.Printf("  ⚠ Could not detect hardware: %s\n", err)
				}
			}

			// Step 2: Provider reachability.
			fmt.Println("\nStep 2/4: Checking inference providers...")
			reg := provider.NewRegistry(targetDist)
			statuses, err := reg.StatusAll(ctx)
			if err != nil {
				fmt.Printf("  ⚠ Could not check providers: %s\n", err)
			} else {
				found := false
				for _, s := range statuses {
					if s.Running {
						fmt.Printf("  ✓ %s running on port %d\n", s.Name, s.Port)
						found = true
					} else if s.Installed {
						fmt.Printf("  ⚠ %s installed but not running (port %d)\n", s.Name, s.Port)
					}
				}
				if !found && !anyInstalled(statuses) {
					fmt.Println("  ℹ No inference providers detected. Start LM Studio or Ollama to enable local AI.")
				}
			}

			// Step 3: Provision distro if needed.
			fmt.Println("\nStep 3/4: Ensuring distro is provisioned...")
			spec := domain.Environment{Name: targetDist, Distribution: targetDist}
			if m != nil {
				spec = m.EnvSpec()
				spec.Name = targetDist
				spec.Distribution = targetDist
				if !cmd.Flags().Changed("user") && m.Jail.User != "" {
					user = m.Jail.User
				}
				if !cmd.Flags().Changed("workspace") && m.Workspace.Path != "" {
					workspacePath = m.Workspace.Path
				}
			}
			if cmd.Flags().Changed("systemd") {
				s := systemd
				spec.Systemd = &s
			}
			if err := b.Provision(ctx, spec); err != nil {
				return fmt.Errorf("provisioning failed: %w", err)
			}
			fmt.Printf("  ✓ Agent-toolkit baseline installed in %s\n", spec.Name)
			resolvedStyle := wsl.ResolveStyle(spec.Name, spec.Style)
			if resolvedStyle.Enabled {
				fmt.Printf("  ✓ Shell prompt & Windows Terminal style applied (%s)\n", resolvedStyle.Preset)
			}

			// Step 4: Import workspace.
			fmt.Println("\nStep 4/4: Setting up workspace...")
			if workspacePath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				workspacePath = cwd
			}

			wsName := filepath.Base(workspacePath)
			if m != nil && m.Workspace.Name != "" {
				wsName = m.Workspace.Name
			}
			if wsName == "" || wsName == "." {
				wsName = "project"
			}

			wsManager := workspace.NewManager(targetDist, user)
			importMode := workspace.ModeBind
			if m != nil && m.Workspace.ImportMode != "" {
				importMode = workspace.ImportMode(m.Workspace.ImportMode)
			}

			if wsManager.Exists(wsName, importMode) && !force {
				fmt.Printf("  ℹ Workspace %q already exists. Use --force to reimport.\n", wsName)
			} else {
				fmt.Printf("  Importing workspace from %s (mode: %s)... ", workspacePath, importMode)
				_, err := wsManager.Import(workspacePath, wsName, importMode)
				if err != nil {
					return fmt.Errorf("importing workspace: %w", err)
				}
				fmt.Printf("OK\n")
			}

			// Summarize jail controls that will be applied during `lapel run`.
			fmt.Println("\nJail policy summary (applied at `lapel run`):")
			profile := jail.Profile{
				Name: "developer",
				User: user,
				Filesystem: map[string]string{
					windowsPathToLinux(workspacePath): "read-write",
				},
				Network: jail.NetworkUnrestricted,
			}

			enforcer := jail.NewEnforcer(profile)
			for _, c := range enforcer.Controls() {
				symbol := jailControlSymbol(c.Level)
				fmt.Printf("  %s %-18s [%s] %s\n", symbol, c.Name, c.Level, c.Details)
			}

			fmt.Println("\n=== Setup Complete ===")
			if features.IsEnabled(features.CommunityEnabled) {
				fmt.Printf("\n🌐 Community features ready\n")
				fmt.Printf("   Share your performance data: lapel community share\n")
				fmt.Printf("   View recommendations: lapel community insights\n")
			}

			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  • Open a shell in WSL: lapel shell (or restart Windows Terminal for the %q profile)\n", spec.Name)
			fmt.Printf("  • Run your agent with: lapel run\n")
			fmt.Printf("  • Sync VS Code plugins: lapel plugin sync --plugin=kilo-code (or --all)\n")
			fmt.Printf("  • Benchmark your rig: lapel benchmark\n")
			fmt.Printf("  • Check provider status: lapel provider status\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: manifest distribution or lapel)")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "path to workspace directory (default: current directory)")
	cmd.Flags().StringVar(&user, "user", "agent", "Linux user for jail context")
	cmd.Flags().BoolVar(&force, "force", false, "reimport workspace even if it exists")
	cmd.Flags().BoolVar(&systemd, "systemd", true, "enable systemd in /etc/wsl.conf (use --systemd=false for lightweight sshd-only boot)")

	return cmd
}

func jailControlSymbol(level jail.EnforcementLevel) string {
	switch level {
	case jail.Enforced:
		return "✓"
	case jail.Unsupported:
		return "✗"
	default: // jail.Requested, jail.NeedsConfig
		return "⚠"
	}
}

// anyInstalled returns true if at least one provider is installed.
func anyInstalled(statuses []*provider.ProviderStatus) bool {
	for _, s := range statuses {
		if s.Installed {
			return true
		}
	}
	return false
}

// windowsPathToLinux converts a Windows path to Linux format for jail filesystem scoping.
func windowsPathToLinux(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(string(path[0]))
		return fmt.Sprintf("/mnt/%s%s", drive, path[2:])
	}
	return path
}
