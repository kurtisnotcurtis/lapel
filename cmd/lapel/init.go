package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"lapel/internal/config"
)

// initCmd scaffolds a starter lapel.yaml manifest in the current working
// directory. It writes a schema-complete, commented template that documents
// every managed section and its options; it never runs or modifies any real
// environment. Re-running overwrites only when --force is given.
func initCmd() *cobra.Command {
	var (
		path    string
		force   bool
	)
	cmd := &cobra.Command{
		Use:    "init",
		Short:  "Scaffold a starter lapel.yaml manifest in this directory",
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
			fmt.Println("  lapel plan   # reconcile this manifest against live state")
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", fmt.Sprintf("manifest file to write (default: %s)", config.DefaultManifestName))
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest at --path")
	return cmd
}

// starterManifest returns a schema-complete, commented example manifest. It is
// intentionally valid YAML and applies cleanly through config.Load defaults, so
// `lapel plan` works against it out of the box after edits.
func starterManifest() string {
	return `# lapel desired-state manifest
#
# This is a starter template. Every managed section below is optional; run
#   lapel init --help
# and see AGENTS.md for field-by-field guidance. Fields left blank are filled
# with sensible defaults by the CLI (see config.Load).

apiVersion: lapel.dev/v1
name: lapel-project

# The runtime environment this agent runs inside (WSL2 distro on Windows, or
# native Linux). Leave distribution blank to import a fresh pinned rootfs.
environment:
  # Pre-existing distro name (e.g. "Ubuntu-Lean"). Use "import" + base/version
  # below to have lapel create a new one.
  distribution: ""
  # base: ubuntu | debian ; version: e.g. "24.04" / "12"
  base: ubuntu
  version: "24.04"
  # Pin the distro rootfs image (+checksum) instead of importing from a name.
  rootfsImage: ""
  # Runtimes -> pinned major versions (the agent-toolkit baseline is always
  # applied; list extras here).
  runtimes:
    node: "20"
    python: "3"
    go: "1.23"
    rust: ""
  # Extra apt packages beyond the agent-toolkit minimum.
  packages: []

# The inference provider this agent talks to. A local LM Studio endpoint is
# shown by default; point baseURL at any OpenAI-compatible server to switch.
provider:
  name: local
  type: openai-compatible
  baseURL: "http://host.docker.internal:1234/v1"
  model: ""
  # Reference a key stored in the local secret store instead of embedding it.
  apiKeySecret: ""
  timeout: 60s
  connectionTimeout: 8s

# The agent harness configuration. provider is a logical name reference to
# the provider section above.
agent:
  name: pi
  harness: ""
  provider: local

# Where this agent's work happens. importMode "bind" shares the host path;
# "copy" mirrors it into the distro.
workspace:
  name: workspace
  path: "/tmp"
  source: ""
  importMode: bind

# The execution jail for untrusted code. Kept minimal by default.
jail:
  name: jail
  profile: developer
  user: agent
  filesystem: {}
  processes:
    privileged: false
    timeoutSeconds: 60
  resources:
    memory: ""
    cpu: 0
  network: unrestricted
`
}
