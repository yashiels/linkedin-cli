// Command lnk is a CLI tool for interacting with LinkedIn's internal API.
// It is built on a reverse-engineered Android APK and is intended for
// personal/research use only.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	lnkcmd "github.com/yashiels/linkedin-cli/internal/cmd"
	"github.com/yashiels/linkedin-cli/internal/config"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		os.Exit(writeError(root, os.Stderr, err))
	}
}

func newRootCmd() *cobra.Command {
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
  --verbose          Show bounded HTTP method/status diagnostics on stderr
  --debug            Show bounded HTTP method/status diagnostics on stderr`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if flagConfig != "" {
				if err := os.Setenv("LNK_CONFIG", flagConfig); err != nil {
					return types.NewError(2, "cannot select configuration")
				}
			}
			if _, err := config.Load(); err != nil {
				return types.NewError(2, "configuration could not be loaded")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return types.NewError(2, "invalid command flags") })
	pf := root.PersistentFlags()
	pf.BoolVar(&flagJSON, "json", false, "Output as JSON")
	pf.BoolVar(&flagPlain, "plain", false, "Output as plain tab-separated text")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress informational output")
	pf.BoolVar(&flagVerbose, "verbose", false, "Show bounded HTTP method/status diagnostics on stderr")
	pf.BoolVar(&flagDebug, "debug", false, "Show bounded HTTP method/status diagnostics on stderr")
	pf.BoolVar(&flagNoColor, "no-color", false, "Disable ANSI colour output")
	pf.BoolVar(&flagNoInput, "no-input", false, "Disable interactive prompts (fail instead)")
	pf.StringVar(&flagConfig, "config", "", "Path to config file (default ~/.config/lnk/config.toml)")

	// Wire all subcommands.
	root.AddCommand(lnkcmd.NewAuthCmd(&flagNoInput))
	root.AddCommand(lnkcmd.NewSearchCmd(&flagJSON, &flagPlain, &flagNoColor, &flagQuiet, &flagVerbose, &flagDebug))
	root.AddCommand(lnkcmd.NewFeedCmd(&flagJSON, &flagPlain, &flagNoColor, &flagQuiet, &flagVerbose, &flagDebug))
	root.AddCommand(lnkcmd.NewJobCmd(&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor))
	root.AddCommand(lnkcmd.NewApplyCmd(&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor))
	root.AddCommand(lnkcmd.NewSavedCmd(&flagNoInput, &flagJSON, &flagPlain, &flagQuiet, &flagVerbose, &flagDebug, &flagNoColor))
	root.AddCommand(lnkcmd.NewProfileCmd())
	root.AddCommand(lnkcmd.NewAlertsCmd())
	root.AddCommand(lnkcmd.NewStatusCmd())
	root.AddCommand(lnkcmd.NewSafetyCmd())

	// Shell completion.
	root.AddCommand(&cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for lnk.

To load completions:

Bash:
  $ source <(lnk completion bash)

Zsh:
  $ source <(lnk completion zsh)

Fish:
  $ lnk completion fish | source`,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	})

	_ = flagJSON
	_ = flagPlain
	_ = flagQuiet
	_ = flagVerbose
	_ = flagDebug
	_ = flagNoColor
	_ = flagConfig

	return root
}

func writeError(root *cobra.Command, w io.Writer, err error) int {
	code := types.ExitGeneral
	var coded *types.LnkError
	reason := types.ReasonUnspecified
	if errors.As(err, &coded) {
		if coded.Code >= types.ExitGeneral && coded.Code <= types.ExitRateLimit {
			code = coded.Code
		}
		reason = coded.Reason
	}
	message, reasonName := safeDiagnosticReason(code, reason)
	fields := map[string]any{"error": message, "code": code, "reason": reasonName}
	if coded != nil {
		if coded.Status >= 100 && coded.Status <= 999 {
			fields["status"] = coded.Status
		}
		switch coded.Method {
		case "GET", "POST", "DELETE":
			fields["method"] = coded.Method
		}
		switch coded.Operation {
		case "read", "graphql", "normalized_read", "post", "delete":
			fields["operation"] = coded.Operation
		}
	}
	jsonMode, _ := root.PersistentFlags().GetBool("json")
	if jsonMode {
		_ = json.NewEncoder(w).Encode(fields)
	} else {
		fmt.Fprintf(w, "error: %s", message)
		for _, key := range []string{"reason", "method", "operation", "status"} {
			if value, ok := fields[key]; ok {
				fmt.Fprintf(w, " %s=%v", key, value)
			}
		}
		fmt.Fprintln(w)
	}
	return code
}

func safeDiagnosticReason(code int, reason types.DiagnosticReason) (string, string) {
	switch reason {
	case types.ReasonHTTPStatus:
		return "LinkedIn returned an unsuccessful HTTP status", "http_status"
	case types.ReasonRedirect:
		return "LinkedIn redirect rejected", "redirect"
	case types.ReasonTransport:
		return "LinkedIn request failed", "transport"
	case types.ReasonResponseRead:
		return "reading LinkedIn response failed", "response_read"
	case types.ReasonResponseSize:
		return "LinkedIn response exceeds size limit", "response_size"
	case types.ReasonJSON:
		return "LinkedIn returned invalid JSON", "invalid_json"
	case types.ReasonGraphQL:
		return "LinkedIn returned GraphQL errors", "graphql_errors"
	case types.ReasonRefusal:
		return "LinkedIn refused request; inspect safety status", "refusal"
	}
	switch code {
	case types.ExitUsage:
		return "invalid arguments or configuration", "usage"
	case types.ExitAuth:
		return "authentication or refusal stop; inspect safety status", "auth_stop"
	case types.ExitNetwork:
		return "network or safety state failure; inspect safety status", "network_state"
	case types.ExitRateLimit:
		return "request budget, cooldown or rate limit reached; inspect safety status", "rate_limit"
	default:
		return "command failed", "general"
	}
}
