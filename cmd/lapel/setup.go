package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/domain"
	"lapel/internal/jail"
	"lapel/internal/provider"
	"lapel/internal/workspace"
)

// setupCmd is the one-command "try something" experience. It chains:
//   1. Quick doctor check (WSL availability, provider reachability)
//   2. Provision the distro if needed
//   3. Import workspace from current directory or prompt for path
//   4. Apply jail policies
//
// This is the hero command for new users — everything they need to get started.
func setupCmd() *cobra.Command {
	var (
		workspacePath string
		user          string
		force         bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-command guided setup: provision, import workspace, apply jail",
		Long: `Set up LAPeL in one command. Checks WSL availability, provisions the
distro if needed, imports your current project directory (or a path you specify),
and applies jail policies so your agent can't accidentally eat your Windows files.

If you're already set up, run setup again to refresh — it's idempotent.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			fmt.Println("=== LAPeL Setup ===\n")

			// Step 1: Quick doctor check
			fmt.Println("Step 1/4: Checking WSL environment...")
			b := &wsl.DistBackend{}
			status, err := b.Detect(ctx)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}

			if !status.Available {
				fmt.Println("  ✗ WSL not available. Install a distro via `wsl.exe --install` first.")
				fmt.Println("    Then set DIST=Ubuntu-Lean or run in native Linux.")
				return fmt.Errorf("WSL not available")
			}

			fmt.Printf("  ✓ WSL available (%s %s)\n", status.Name, status.Version)

			// Check GPU if available (informational)
			gStatus, err := wsl.CheckGPUStatus(ctx)
			if err == nil && len(gStatus.GPUs) > 0 {
				fmt.Printf("  ✓ GPU detected: %s\n", gStatus.GPUs[0])
			}

			// Step 2: Provider reachability
			fmt.Println("\nStep 2/4: Checking inference providers...")
			reg := provider.NewRegistry(wsl.Dist)
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

			// Step 3: Provision distro if needed
			fmt.Println("\nStep 3/4: Ensuring distro is provisioned...")
			spec := domain.Environment{Name: wsl.Dist}
			if err := b.Provision(ctx, spec); err != nil {
				return fmt.Errorf("provisioning failed: %w", err)
			}
			fmt.Printf("  ✓ Agent-toolkit baseline installed in %s\n", wsl.Dist)

			// Step 4: Import workspace
			fmt.Println("\nStep 4/4: Setting up workspace...")
			if workspacePath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				workspacePath = cwd
			}

			wsName := filepath.Base(workspacePath)
			if wsName == "" || wsName == "." {
				wsName = "project"
			}

			wsManager := workspace.NewManager(user)
			importMode := workspace.ImportMode("bind") // prefer bind mounts for dev

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

			// Apply jail policies
			fmt.Println("\nApplying jail policies...")
			profile := jail.Profile{
				Name: "developer",
				User: user,
				Filesystem: map[string]string{
					windowsPathToLinux(workspacePath): "read-write",
				},
				Network: jail.NetworkUnrestricted,
			}

			enforcer := jail.NewEnforcer(profile)
			controls := enforcer.Controls()

			fmt.Println("  Enforcement controls:")
			for _, c := range controls {
				symbol := "✓"
				if c.Level == "partial" || c.Level == "unsupported" {
					symbol = "⚠"
				} else if c.Level == "none" {
					symbol = "✗"
				}
				fmt.Printf("    %s %s: %s\n", symbol, c.Name, c.Details)
			}

			fmt.Println("\n=== Setup Complete ===")
			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  • Run your agent with: lapel run\n")
			fmt.Printf("  • Check provider status: lapel provider status\n")
			fmt.Printf("  • Inspect environment: lapel env status\n")

			return nil
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "path to workspace directory (default: current directory)")
	cmd.Flags().StringVar(&user, "user", "agent", "Linux user for jail context")
	cmd.Flags().BoolVar(&force, "force", false, "reimport workspace even if it exists")

	return cmd
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
	// Convert backslashes to forward slashes
	path = strings.ReplaceAll(path, "\\", "/")
	// Extract drive letter and convert to /mnt/<drive>/...
	if len(path) >= 2 && path[1] == ':' {
		drive := strings.ToLower(string(path[0]))
		return fmt.Sprintf("/mnt/%s%s", drive, path[2:])
	}
	return path
}
