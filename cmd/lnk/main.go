// Command lnk is a command-line interface for LinkedIn built on the internal
// Voyager API reverse-engineered from the Android application.
// Intended for personal / research use only. Not affiliated with LinkedIn.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	lnkcmd "github.com/yashiels/linkedin-cli/internal/cmd"
)

// version is stamped at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Cobra already printed the error; just set a non-zero exit.
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	// Persistent flag state — values captured here flow into all subcommands
	// via cmd.Root().PersistentFlags().GetBool(...).
	var (
		flagJSON    bool
		flagPlain   bool
		flagQuiet   bool
		flagVerbose bool
		flagDebug   bool
		flagNoColor bool
		flagNoInput bool
		flagConfig  string
	)

	root := &cobra.Command{
		Use:     "lnk",
		Short:   "LinkedIn CLI — interact with LinkedIn from the terminal",
		Version: version,
		Long: `lnk is a command-line interface for LinkedIn, built on the internal API
used by the Android application (reverse-engineered from the APK).

DISCLAIMER: This is an unofficial tool. It may break at any time if LinkedIn
changes their API. Use responsibly and at your own risk.

Quick start:
  1. Obtain your li_at and JSESSIONID cookies from the browser.
  2. Run: lnk auth login
  3. Run: lnk search "software engineer" --location "Cape Town"

Global flags work with every command:
  --json     Output as JSON (machine-readable)
  --plain    Output tab-separated text (pipe-friendly)
  --no-color Disable ANSI colour
  -q/--quiet Suppress informational messages
  --verbose  Show HTTP requests on stderr
  --debug    Show full HTTP bodies on stderr`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags — available in every subcommand.
	pf := root.PersistentFlags()
	pf.BoolVar(&flagJSON, "json", false, "Output as JSON")
	pf.BoolVar(&flagPlain, "plain", false, "Output as plain tab-separated text")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress informational output")
	pf.BoolVar(&flagVerbose, "verbose", false, "Show HTTP request details on stderr")
	pf.BoolVar(&flagDebug, "debug", false, "Show full request/response bodies on stderr")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable ANSI colour output")
	pf.BoolVar(&flagNoInput, "no-input", false, "Disable interactive prompts (fail instead)")
	pf.StringVar(&flagConfig, "config", "", "Path to config file (default ~/.config/lnk/config.toml)")

	// --- Implemented subcommands ---
	root.AddCommand(lnkcmd.NewAuthCmd(&flagNoInput))
	root.AddCommand(lnkcmd.NewProfileCmd())
	root.AddCommand(lnkcmd.NewAlertsCmd())
	root.AddCommand(lnkcmd.NewStatusCmd())
	root.AddCommand(newCompletionCmd(root))

	// --- Stub subcommands (implemented by other streams) ---
	stubs := []struct {
		use   string
		short string
	}{
		{"search", "Search for jobs"},
		{"job", "View job details"},
		{"apply", "Apply to a job"},
		{"saved", "Manage saved jobs"},
		{"feed", "Browse the LinkedIn feed"},
	}
	for _, s := range stubs {
		s := s // capture
		root.AddCommand(&cobra.Command{
			Use:   s.use,
			Short: s.short,
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: not yet implemented\n", s.use)
				return nil
			},
		})
	}

	// Suppress "declared and not used" errors for flag vars used only via
	// PersistentFlags().GetBool() in subcommands.
	_, _, _, _, _, _, _ = flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor, flagConfig

	return root
}

// newCompletionCmd adds "lnk completion [bash|zsh|fish|powershell]".
func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate a shell completion script for lnk.

Bash:
  source <(lnk completion bash)
  # or to persist:
  lnk completion bash > /etc/bash_completion.d/lnk

Zsh:
  # Ensure compinit is loaded (add to ~/.zshrc if not already there):
  #   autoload -Uz compinit && compinit
  lnk completion zsh > "${fpath[1]}/_lnk"

Fish:
  lnk completion fish > ~/.config/fish/completions/lnk.fish

PowerShell:
  lnk completion powershell | Out-String | Invoke-Expression`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
}
