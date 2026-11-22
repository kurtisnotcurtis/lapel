package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"lapel/internal/backend/wsl"
	"lapel/internal/config"
	"lapel/internal/domain"
)

// shellCmd opens an interactive shell inside the managed lapel WSL distro
// without requiring a running inference provider.
func shellCmd() *cobra.Command {
	var (
		distro string
		user   string
		cdDir  string
		inWT   bool
	)

	cmd := &cobra.Command{
		Use:     "shell [flags] [-- command...]",
		Aliases: []string{"enter"},
		Short:   "Open an interactive shell inside the lapel WSL distribution",
		Long: `Open an interactive shell inside the managed lapel WSL distribution.

Unlike 'lapel run', this does not require a running inference provider or
launch an agent — it drops you directly into the WSL distro with full terminal
TTY support.

Pass --wt to open the shell in a new Windows Terminal tab.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			targetDistro := wsl.Dist
			styleSpec := domain.EnvironmentStyle{}
			if m, err := config.LoadEffective(""); err == nil && m != nil {
				envSpec := m.EnvSpec()
				if envSpec.Distribution != "" {
					targetDistro = envSpec.Distribution
				}
				styleSpec = envSpec.Style
			}
			if cmd.Flags().Changed("distro") && strings.TrimSpace(distro) != "" {
				targetDistro = strings.TrimSpace(distro)
			}

			// Verify WSL and the target distribution exist before attaching.
			if _, err := exec.LookPath("wsl.exe"); err != nil {
				return fmt.Errorf("wsl.exe not found on PATH: %w", err)
			}
			names, err := wsl.ListDistros(ctx)
			if err != nil {
				return fmt.Errorf("listing WSL distributions: %w", err)
			}
			found := false
			for _, n := range names {
				if strings.EqualFold(n, targetDistro) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("WSL distro %q is not provisioned yet — run `lapel setup` or `lapel provision` first", targetDistro)
			}

			wslArgs := buildShellWSLArgs(targetDistro, user, cdDir, args)

			if inWT {
				wtExe, err := exec.LookPath("wt.exe")
				if err != nil {
					return fmt.Errorf("Windows Terminal (wt.exe) not found on PATH: %w", err)
				}
				resolved := wsl.ResolveStyle(targetDistro, styleSpec)
				wtArgs := buildShellWTArgs(targetDistro, resolved.TabColor, wslArgs)
				wtCmd := exec.CommandContext(ctx, wtExe, wtArgs...)
				if err := wtCmd.Start(); err != nil {
					return fmt.Errorf("launching Windows Terminal tab: %w", err)
				}
				fmt.Printf("✓ Opened %q shell in a new Windows Terminal tab\n", targetDistro)
				return nil
			}

			c := exec.CommandContext(ctx, "wsl.exe", wslArgs...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) && len(args) == 0 {
					// Interactive shells exit with the status of the last command run;
					// don't print a noisy Cobra error banner on normal exit.
					return nil
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&distro, "distro", "d", "", "WSL distribution name (default: lapel or manifest distribution)")
	cmd.Flags().StringVarP(&user, "user", "u", "", "Linux user inside WSL (default: distro default user)")
	cmd.Flags().StringVar(&cdDir, "cd", "~", "starting directory inside WSL (use '.' for current directory)")
	cmd.Flags().BoolVar(&inWT, "wt", false, "open the shell in a new Windows Terminal tab")

	return cmd
}

// buildShellWSLArgs constructs the argument list passed to wsl.exe for `lapel shell`.
func buildShellWSLArgs(distro, user, cdDir string, extraArgs []string) []string {
	args := []string{"-d", distro}
	if u := strings.TrimSpace(user); u != "" {
		args = append(args, "-u", u)
	}
	if d := strings.TrimSpace(cdDir); d != "" {
		args = append(args, "--cd", d)
	}
	if len(extraArgs) > 0 {
		args = append(args, "--")
		args = append(args, extraArgs...)
	}
	return args
}

// buildShellWTArgs constructs the argument list passed to wt.exe to open a new
// Windows Terminal tab running the given wsl.exe arguments.
func buildShellWTArgs(title, tabColor string, wslArgs []string) []string {
	args := []string{"-w", "0", "new-tab"}
	if t := strings.TrimSpace(title); t != "" {
		args = append(args, "--title", t)
	}
	if tc := strings.TrimSpace(tabColor); tc != "" {
		args = append(args, "--tabColor", tc)
	}
	args = append(args, "--", "wsl.exe")
	args = append(args, wslArgs...)
	return args
}
