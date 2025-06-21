package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"lapel/internal/config"
	"lapel/internal/jail"
	"lapel/internal/reconcile"
	"lapel/internal/state"
	"lapel/internal/workspace"
)

// defaultStatePath returns the default on-disk location for the state database,
// nested under a project-local .lapel directory so it is not confused with
// the manifest.
func defaultStatePath() (string, error) {
	dir := filepath.Join(".", ".lapel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating state dir: %w", err)
	}
	return filepath.Join(dir, "state.db"), nil
}

// openState opens the state store at path (or the default when empty).
func openState(path string) (*state.Store, error) {
	if path == "" {
		p, err := defaultStatePath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	return state.New(path)
}

// applyCmd persists the desired-state baseline from the effective manifest into
// the state store and reports current live status. Re-running it updates the
// stored baseline to match the manifest; diff/status then report drift against
// that committed reference.
func applyCmd() *cobra.Command {
	var (
		manifestPath string
		statePath    string
	)
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Persist the desired-state baseline and import workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := os.MkdirAll(".", 0o755); err != nil { // allow manifest discovery in cwd
				return fmt.Errorf("creating work dir: %w", err)
			}
			m, err := config.LoadEffective(manifestPath)
			if err != nil {
				return err
			}
			st, err := openState(statePath)
			if err != nil {
				return fmt.Errorf("opening state store: %w", err)
			}
			defer st.Close()

			res, err := reconcile.Diff(ctx, m, reconcile.NewOptions(ctx))
			if err != nil {
				return fmt.Errorf("reconciling: %w", err)
			}

			// Import workspace if needed (copy mode by default).
			wsManager := workspace.NewManager(m.Jail.User)
			importMode := workspace.ImportMode(m.Workspace.ImportMode)
			if importMode == "" {
				importMode = workspace.DefaultMode()
			}

			// Perform import if the workspace doesn't exist yet.
			if !wsManager.Exists(m.Workspace.Name, importMode) {
				fmt.Printf("Importing workspace %q (mode: %s)... ", m.Workspace.Name, importMode)
				_, err := wsManager.Import(m.Workspace.Path, m.Workspace.Name, importMode)
				if err != nil {
					return fmt.Errorf("importing workspace: %w", err)
				}
				fmt.Printf("OK\n")
			} else {
				fmt.Printf("Workspace %q already exists (mode: %s)\n", m.Workspace.Name, importMode)
			}

			// Persist the desired baseline (spec + content hash) for each resource.
			persisted, err := persistBaseline(st, m)
			if err != nil {
				return fmt.Errorf("persisting baseline: %w", err)
			}

			fmt.Printf("\nApplied baseline for %q (%s)\n", m.Name, m.APIVersion)
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "\tKIND\tNAME\tDESIRED_HASH")
			for _, p := range persisted {
				fmt.Fprintf(w, "\t%s\t%s\t%s\n", p.Kind, p.Name, shortHash(p.DesiredHash))
			}
			w.Flush()

			// Report provider type if specified.
			if m.Provider.Type != "" {
				fmt.Printf("\nProvider type: %s\n", m.Provider.Type)
			}

			fmt.Println("\nCurrent live status:")
			printDiff(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to lapel.yaml (default: discover in cwd/ancestors)")
	cmd.Flags().StringVar(&statePath, "state", "", "path to state database (default: ./.lapel/state.db)")
	return cmd
}

// persistBaseline records the desired baseline for each managed resource and
// returns what was written so the caller can report it.
func persistBaseline(st *state.Store, m *config.Manifest) ([]*state.DesiredResource, error) {
	type target struct {
		kind     string
		name     string
		specJSON string
	}
	targets := []target{
		{state.KindEnvironment, reconcile.EnvName(m), reconcile.CanonicalSpec(m.Environment)},
		{state.KindProvider, m.Provider.Name, reconcile.CanonicalSpec(m.Provider)},
		{state.KindWorkspace, m.Workspace.Name, reconcile.CanonicalSpec(m.Workspace)},
	}
	var out []*state.DesiredResource
	for _, t := range targets {
		if err := st.SetDesired(t.kind, t.name, t.specJSON, reconcile.HashSpecFromJSON(t.specJSON)); err != nil {
			return nil, err
		}
		out = append(out, &state.DesiredResource{Kind: t.kind, Name: t.name, DesiredHash: reconcile.HashSpecFromJSON(t.specJSON)})
	}
	return out, nil
}

// statusCmd compares current live reality against the persisted desired baseline.
// It reports drift over time independent of any manifest file currently present.
func statusCmd() *cobra.Command {
	var statePath string
	cmd := &cobra.Command{
		Use:    "status",
		Short:  "Report drift between live state and the persisted desired baseline",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := openState(statePath)
			if err != nil {
				return fmt.Errorf("opening state store: %w", err)
			}
			defer st.Close()

			baselines, err := st.DesiredResources()
			if err != nil {
				return fmt.Errorf("reading baselines: %w", err)
			}
			opts := reconcile.NewOptions(ctx)

			fmt.Println("Drift against persisted desired baseline:")
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "\tKIND\tNAME\tSTATUS\tACTUAL")
			for _, b := range baselines {
				status, actual := probeBaseline(ctx, opts, b)
				desired := "OK"
				if status != "OK" {
					desired = "DRIFT"
				}
				fmt.Fprintf(w, "\t%s\t%s\t%s\t%s\n", desired, b.Kind, b.Name, actual+" ("+status+")")
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&statePath, "state", "", "path to state database (default: ./.lapel/state.db)")
	return cmd
}

// probeBaseline reconstructs a resource's declared spec from its stored JSON and
// runs the matching default probe, returning a live status label ("OK"/"DRIFT")
// and a human-readable description.
func probeBaseline(ctx context.Context, opts reconcile.Options, b *state.DesiredResource) (status, actual string) {
	switch b.Kind {
	case state.KindEnvironment:
		var cfg config.Environment
		if err := json.Unmarshal([]byte(b.SpecJSON), &cfg); err != nil {
			return "DRIFT", "unreadable spec: " + err.Error()
		}
		st, err := opts.EnvProber(ctx, cfg)
		if err != nil {
			return "DRIFT", "probe error: " + err.Error()
		}
		ok := st.Exists && (cfg.Distribution == "" || strings.EqualFold(st.Distribution, cfg.Distribution))
		if !ok {
			return "DRIFT", reconcile.DescribeEnv(st)
		}
		return "OK", reconcile.DescribeEnv(st)
	case state.KindProvider:
		var cfg config.Provider
		if err := json.Unmarshal([]byte(b.SpecJSON), &cfg); err != nil {
			return "DRIFT", "unreadable spec: " + err.Error()
		}
		reachable, note := opts.ProviderProbe(cfg)
		if !reachable {
			return "DRIFT", note
		}
		return "OK", note
	case state.KindWorkspace:
		var cfg config.Workspace
		if err := json.Unmarshal([]byte(b.SpecJSON), &cfg); err != nil {
			return "DRIFT", "unreadable spec: " + err.Error()
		}
		exists, err := opts.WorkspaceExists(cfg)
		if err != nil {
			return "DRIFT", "probe error: " + err.Error()
		}
		if !exists {
			return "DRIFT", reconcile.DescribeWorkspace(cfg, false)
		}
		return "OK", reconcile.DescribeWorkspace(cfg, true)
	default:
		return "DRIFT", "unknown kind"
	}
}

// jailControlsCmd reports the enforcement controls active for a given profile.
func jailControlsCmd() *cobra.Command {
	var (
		user    string
		timeout int
	)
	cmd := &cobra.Command{
		Use:   "jail-controls",
		Short: "Show jail enforcement controls and their status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile := jail.Profile{
				Name: "developer",
				User: user,
				Processes: jail.ProcessConfig{TimeoutSeconds: timeout},
				Filesystem: map[string]string{
					"C:\\Users\\Kurt\\Projects": "read-write",
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
	cmd.Flags().IntVar(&timeout, "timeout", 300, "Process timeout in seconds")
	return cmd
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
