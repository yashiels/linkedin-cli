// Command lnk is a CLI tool for interacting with LinkedIn's internal API.
// It is built on a reverse-engineered Android APK and is intended for
// personal/research use only.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	lnkcmd "github.com/yashiels/linkedin-cli/internal/cmd"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	// Persistent flag state — shared with subcommands.
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
		Long: `lnk is a command-line interface for LinkedIn built on the internal API
used by the Android application.

Authentication:
  lnk auth login     Store your li_at and CSRF session cookies.

Common flags:
  --json             Output as JSON
  --plain            Output as plain tab-separated text (pipe-friendly)
  --no-color         Disable ANSI colour
  --quiet / -q       Suppress informational output
  --verbose          Show HTTP request details on stderr
  --debug            Show full request/response bodies on stderr`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent global flags available to every subcommand.
	pf := root.PersistentFlags()
	pf.BoolVar(&flagJSON, "json", false, "Output as JSON")
	pf.BoolVar(&flagPlain, "plain", false, "Output as plain tab-separated text")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress informational output")
	pf.BoolVar(&flagVerbose, "verbose", false, "Show HTTP request details on stderr")
	pf.BoolVar(&flagDebug, "debug", false, "Show full request/response bodies on stderr")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable ANSI colour output")
	pf.BoolVar(&flagNoInput, "no-input", false, "Disable interactive prompts (fail instead)")
	pf.StringVar(&flagConfig, "config", "", "Path to config file (default ~/.config/lnk/config.toml)")

	// Wire subcommands.
	root.AddCommand(lnkcmd.NewAuthCmd(&flagNoInput))

	// Stream 3: job detail, apply, and saved commands.
	root.AddCommand(lnkcmd.NewJobCmd(
		&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor,
	))
	root.AddCommand(lnkcmd.NewApplyCmd(
		&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor,
	))
	root.AddCommand(lnkcmd.NewSavedCmd(
		&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor,
	))

	// Stub subcommands — implemented by other streams.
	stubs := []struct {
		use   string
		short string
	}{
		{"search", "Search for jobs"},
		{"feed", "Browse the LinkedIn feed"},
		{"profile", "View a LinkedIn profile"},
		{"alerts", "Manage job alerts"},
		{"status", "Show API and auth status"},
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

	// Suppress unused variable warnings.
	_ = flagConfig

	return root
}
