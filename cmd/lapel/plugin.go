package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/hardware"
	"lapel/internal/plugins"
	"lapel/internal/provider"
)

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage VS Code coding-agent plugin configurations (Kilo Code, Continue, Cline)",
	}
	cmd.AddCommand(
		pluginListCmd(),
		pluginSyncCmd(),
	)
	return cmd
}

func pluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List supported VS Code plugins and their configuration paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reg := plugins.NewRegistry()
			fmt.Println("Supported VS Code plugins:")
			for _, p := range reg.All() {
				cfgPath := p.ConfigPath()
				status := "not configured yet"
				if _, err := os.Stat(cfgPath); err == nil {
					status = "config present"
				}
				fmt.Printf("  • %-10s  [%s] → %s\n", p.Name(), status, cfgPath)
			}
			return nil
		},
	}
}

func pluginSyncCmd() *cobra.Command {
	var (
		pluginName string
		all        bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Generate and write VRAM-tuned model configs for VS Code plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pluginName == "" && !all {
				all = true
			}
			return syncPlugins(cmd.Context(), pluginName, all)
		},
	}
	cmd.Flags().StringVar(&pluginName, "plugin", "", "target plugin name (kilo-code, continue, cline)")
	cmd.Flags().BoolVar(&all, "all", false, "sync configurations for all supported plugins")
	return cmd
}

// discoverPluginProviderSpecs queries running providers and hardware VRAM to
// build VRAM-tuned ProviderSpec entries for IDE plugin config generation.
func discoverPluginProviderSpecs(ctx context.Context) []plugins.ProviderSpec {
	vramGB := 8.0
	if hwInfo, err := hardware.Detect(ctx); err == nil && hwInfo.GPU.VRAMGB > 0 {
		vramGB = hwInfo.GPU.VRAMGB
	}

	reg := provider.NewRegistry(wsl.Dist)
	statuses, _ := reg.StatusAll(ctx)

	var specs []plugins.ProviderSpec
	for _, s := range statuses {
		if !s.Running && len(s.Models) == 0 {
			continue
		}
		port := s.Port
		if port == 0 {
			port = 1234
		}
		modelIDs := s.Models
		if len(modelIDs) == 0 {
			if p := reg.FindByName(s.Type); p != nil {
				if listed, err := p.ListModels(ctx); err == nil {
					modelIDs = listed
				}
			}
		}
		specs = append(specs, plugins.ProviderSpec{
			Name:    s.Type,
			Port:    port,
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", port),
			Models:  plugins.BuildModelSpecs(modelIDs, vramGB),
		})
	}

	// Fallback if no provider is actively running so config generation still succeeds
	// with a sensible hardware-recommended default model.
	if len(specs) == 0 {
		defaultModel := "qwen2.5-coder-7b"
		if rec, err := hardware.Recommend(ctx); err == nil && rec.ModelName != "" {
			defaultModel = rec.ModelName
		}
		specs = append(specs, plugins.ProviderSpec{
			Name:    "lm-studio",
			Port:    1234,
			BaseURL: "http://127.0.0.1:1234/v1",
			Models:  plugins.BuildModelSpecs([]string{defaultModel}, vramGB),
		})
	}

	return specs
}

// syncPlugins writes generated configurations for one or all registered VS Code plugins.
func syncPlugins(ctx context.Context, pluginName string, all bool) error {
	reg := plugins.NewRegistry()
	var targets []plugins.Plugin

	if all || pluginName == "all" {
		targets = reg.All()
	} else {
		p, err := reg.Get(pluginName)
		if err != nil {
			return err
		}
		targets = []plugins.Plugin{p}
	}

	providerSpecs := discoverPluginProviderSpecs(ctx)
	modelCount := 0
	for _, ps := range providerSpecs {
		modelCount += len(ps.Models)
	}

	for _, t := range targets {
		writtenPath, err := t.Apply(providerSpecs)
		if err != nil {
			return fmt.Errorf("syncing plugin %s: %w", t.Name(), err)
		}
		fmt.Printf("✓ Synced %s config (%d model(s)) → %s\n", t.Name(), modelCount, writtenPath)
	}
	return nil
}
