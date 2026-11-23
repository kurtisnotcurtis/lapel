package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/jail"
	"lapel/internal/reconcile"
	"lapel/internal/workspace"
)

// applyCmd reconciles live state toward the effective lapel.yaml manifest:
// provisioning the WSL distribution, importing the workspace, optionally
// syncing VS Code plugins, and printing the resulting live status.
func applyCmd() *cobra.Command {
	var (
		manifestPath  string
		pluginName    string
		syncAll       bool
		allowFallback bool
		skipProvision bool
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Provision environment, import workspace, and optionally sync VS Code plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			// Sync VS Code plugins when --plugin or --all is requested.
			if pluginName != "" || syncAll {
				if err := syncPlugins(ctx, "", pluginName, syncAll, allowFallback); err != nil {
					return err
				}
			}

			m, err := config.LoadEffective(manifestPath)
			if err != nil {
				// If the user only requested plugin sync and has no lapel.yaml in cwd, succeed cleanly.
				if (pluginName != "" || syncAll) && manifestPath == "" {
					return nil
				}
				return err
			}

			targetDist := reconcile.EnvName(m)

			// Provision WSL environment if available and not skipped.
			if !skipProvision {
				b := wsl.NewBackend(targetDist)
				det, err := b.Detect(ctx)
				if err != nil {
					return fmt.Errorf("detecting WSL backend: %w", err)
				}
				if det.Available {
					fmt.Printf("Ensuring environment %q is provisioned...\n", targetDist)
					if err := b.Provision(ctx, m.EnvSpec()); err != nil {
						return fmt.Errorf("provisioning environment %q: %w", targetDist, err)
					}
					fmt.Printf("  ✓ Environment %q provisioned\n", targetDist)
				}
			}

			// Import workspace if needed.
			wsName := m.Workspace.Name
			if wsName == "" {
				wsName = "workspace"
			}
			wsPath := m.Workspace.Path
			if wsPath == "" {
				wsPath = "."
			}
			wsManager := workspace.NewManager(targetDist, m.Jail.User)
			importMode := workspace.ImportMode(m.Workspace.ImportMode)
			if importMode == "" {
				importMode = workspace.DefaultMode()
			}

			if !wsManager.Exists(wsName, importMode) {
				fmt.Printf("Importing workspace %q (mode: %s)... ", wsName, importMode)
				if _, err := wsManager.Import(wsPath, wsName, importMode); err != nil {
					return fmt.Errorf("importing workspace: %w", err)
				}
				fmt.Printf("OK\n")
			} else {
				fmt.Printf("Workspace %q already exists (mode: %s)\n", wsName, importMode)
			}

			res, err := reconcile.Diff(ctx, m, reconcile.NewOptions(ctx, targetDist))
			if err != nil {
				return fmt.Errorf("reconciling: %w", err)
			}

			fmt.Printf("\nApplied manifest %q (%s)\n", m.Name, m.APIVersion)
			if m.Provider.Type != "" {
				fmt.Printf("Provider type: %s\n", m.Provider.Type)
			}
			fmt.Println("\nCurrent live status:")
			printDiff(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to lapel.yaml (default: discover in cwd/ancestors)")
	cmd.Flags().StringVar(&pluginName, "plugin", "", "sync VS Code plugin configuration (kilo-code, continue, cline, or all)")
	cmd.Flags().BoolVar(&syncAll, "all", false, "sync configurations for all supported VS Code plugins")
	cmd.Flags().BoolVar(&allowFallback, "allow-fallback", false, "allow generating a fallback placeholder model config if no provider is running")
	cmd.Flags().BoolVar(&skipProvision, "skip-provision", false, "skip WSL distro provisioning step")
	return cmd
}

// statusCmd inspects live environment, provider, and workspace state against
// the effective lapel.yaml manifest without exiting non-zero on drift.
func statusCmd() *cobra.Command {
	var manifestPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report live status of environment, provider, and workspace against lapel.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			m, err := config.LoadEffective(manifestPath)
			if err != nil {
				return err
			}
			res, err := reconcile.Diff(ctx, m, reconcile.NewOptions(ctx, reconcile.EnvName(m)))
			if err != nil {
				return fmt.Errorf("checking live status: %w", err)
			}
			printDiff(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to lapel.yaml (default: discover in cwd/ancestors)")
	return cmd
}

// jailControlsCmd reports the enforcement controls active for a given profile.
func jailControlsCmd() *cobra.Command {
	var (
		user    string
		timeout int
	)
	cmd := &cobra.Command{
		Use:    "jail-controls",
		Short:  "Show jail enforcement controls and their honest enforcement level",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil || cwd == "" {
				cwd = "."
			}
			profile := jail.Profile{
				Name:      "developer",
				User:      user,
				Processes: jail.ProcessConfig{TimeoutSeconds: timeout},
				Filesystem: map[string]string{
					cwd: "read-write",
				},
				Network: jail.NetworkUnrestricted,
			}

			enforcer := jail.NewEnforcer(profile)
			controls := enforcer.Controls()

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "\tCONTROL\tSTATUS\tDETAILS")
			for _, c := range controls {
				fmt.Fprintf(w, "\t%s\t%s\t%s\n", c.Name, c.Level, c.Details)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "agent", "Linux user for jail context")
	cmd.Flags().IntVar(&timeout, "timeout", 0, "Process timeout in seconds (0 = disabled)")
	return cmd
}
