package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"lapel/internal/config"
)

// initCmd scaffolds a starter lapel.yaml manifest in the current working
// directory. It writes a schema-complete, commented template that passes
// strict config.Load validation out of the box.
func initCmd() *cobra.Command {
	var (
		path  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a starter lapel.yaml manifest in this directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if path == "" {
				path = config.DefaultManifestName
			}
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() {
				if !force {
					return fmt.Errorf("%s already exists (use --force to overwrite)", path)
				}
				fmt.Printf("overwriting existing %s\n", path)
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("checking %s: %w", path, err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("creating parent dir: %w", err)
			}
			if err := os.WriteFile(path, []byte(starterManifest()), 0o644); err != nil {
				return fmt.Errorf("writing manifest %s: %w", path, err)
			}
			fmt.Printf("created %s\n", path)
			fmt.Println("\nNext steps:")
			fmt.Println("  lapel apply   # provision WSL distro and import workspace")
			fmt.Println("  lapel plan    # verify live state matches lapel.yaml")
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", fmt.Sprintf("manifest file to write (default: %s)", config.DefaultManifestName))
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest at --path")
	return cmd
}

// starterManifest returns the canonical starter lapel.yaml manifest. It is
// strictly valid YAML matching docs/examples/lapel.yaml.example.
func starterManifest() string {
	return `# lapel desired-state manifest
# See internal/config/manifest.go for field documentation.

apiVersion: lapel.dev/v1
name: lapel-project

environment:
  # WSL distribution name (defaults to "lapel").
  distribution: lapel
  # Base Linux image family and Ubuntu LTS version.
  base: ubuntu
  version: "24.04"
  # Boot WSL with systemd (true) or a lightweight [boot] hook for sshd only (false).
  systemd: true
  # Extra apt packages beyond the built-in agent-toolkit baseline.
  packages: []
  # Visual prompt, welcome banner, and Windows Terminal profile styling.
  # Presets: "lapel-dark" (default), "violet", "minimal", "none".
  style:
    preset: lapel-dark
    badge: ""
    banner: true
    tabColor: ""
    background: ""
    windowsTerminal: true

provider:
  name: local
  type: lm-studio       # "lm-studio" | "ollama" | "openai-compatible"
  baseURL: "http://127.0.0.1:1234"
  model: ""
  timeout: 60s
  connectionTimeout: 8s

agent:
  name: pi              # "pi" | "claude-code" | "copilot"
  provider: local

workspace:
  name: workspace
  path: "."
  importMode: bind      # "bind" (symlink) | "copy" (rsync snapshot)

jail:
  name: developer
  profile: developer
  user: agent
  filesystem: {}
  processes:
    privileged: false
    timeoutSeconds: 0   # 0 = no timeout for interactive sessions
  network: unrestricted # "unrestricted" | "outbound-only" | "none"
`
}
