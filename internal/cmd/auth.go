// Package cmd provides Cobra command implementations for lnk subcommands.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yashiels/linkedin-cli/internal/auth"
)

// NewAuthCmd returns the "lnk auth" command tree.
func NewAuthCmd(noInput *bool) *cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage LinkedIn credentials",
		Long:  "Log in, check status, or log out of your LinkedIn account.",
	}

	authCmd.AddCommand(
		newAuthLoginCmd(noInput),
		newAuthStatusCmd(),
		newAuthLogoutCmd(),
	)
	return authCmd
}

// newAuthLoginCmd returns "lnk auth login".
func newAuthLoginCmd(noInput *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store LinkedIn session cookies",
		Long: `Store your LinkedIn session credentials so lnk can make API calls.

You need two values from your LinkedIn session cookies:

  li_at      — The primary session cookie (long hex/JWT string).
  JSESSIONID — Starts with "ajax:" followed by a numeric CSRF token.

To extract them:
  1. Open LinkedIn in a browser and log in.
  2. Open DevTools → Application → Cookies → linkedin.com.
  3. Copy the values for "li_at" and "JSESSIONID".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *noInput {
				return fmt.Errorf("--no-input is set; cannot prompt for credentials")
			}
			return runAuthLogin(cmd)
		},
	}
}

func runAuthLogin(cmd *cobra.Command) error {
	store, err := auth.Default()
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Enter your LinkedIn session cookies.")
	fmt.Fprintln(cmd.OutOrStdout(), "(Values are stored locally at "+store.Path()+")")
	fmt.Fprintln(cmd.OutOrStdout())

	liAt, err := promptSecret(cmd, "li_at cookie value: ")
	if err != nil {
		return fmt.Errorf("reading li_at: %w", err)
	}
	liAt = strings.TrimSpace(liAt)
	if liAt == "" {
		return fmt.Errorf("li_at cannot be empty")
	}

	// JSESSIONID is in the form "ajax:<csrf_value>".
	jsession, err := promptSecret(cmd, "JSESSIONID cookie value (ajax:<token>): ")
	if err != nil {
		return fmt.Errorf("reading JSESSIONID: %w", err)
	}
	jsession = strings.TrimSpace(jsession)

	// Extract the raw CSRF token — strip leading "ajax:" if present.
	csrf := jsession
	if strings.HasPrefix(csrf, "ajax:") {
		csrf = strings.TrimPrefix(csrf, "ajax:")
	}
	if csrf == "" {
		return fmt.Errorf("CSRF token cannot be empty")
	}

	bcookie, err := promptLine(cmd, "bcookie value (optional, press Enter to skip): ")
	if err != nil {
		return fmt.Errorf("reading bcookie: %w", err)
	}
	bcookie = strings.TrimSpace(bcookie)

	creds := auth.Credentials{
		LiAt:      liAt,
		CSRFToken: csrf,
		BCookie:   bcookie,
	}

	if err := store.Save(creds); err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), "\n✓ Credentials saved.")
	return nil
}

// newAuthStatusCmd returns "lnk auth status".
func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication state",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := auth.Default()
			if err != nil {
				return err
			}
			creds, err := store.Load()
			if err != nil {
				return err
			}
			if creds.LiAt == "" && creds.CSRFToken == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "Not logged in. Run: lnk auth login")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged in")
			fmt.Fprintf(cmd.OutOrStdout(), "  li_at:      %s…\n", truncate(creds.LiAt, 20))
			fmt.Fprintf(cmd.OutOrStdout(), "  csrf_token: %s…\n", truncate(creds.CSRFToken, 20))
			if creds.BCookie != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  bcookie:    %s…\n", truncate(creds.BCookie, 20))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  credentials file: %s\n", store.Path())
			return nil
		},
	}
}

// newAuthLogoutCmd returns "lnk auth logout".
func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := auth.Default()
			if err != nil {
				return err
			}
			if err := store.Clear(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out. Credentials removed.")
			return nil
		},
	}
}

// --- helpers ---

// promptSecret reads a value with echo disabled when on a real terminal,
// falling back to a plain line-reader for non-TTY inputs (e.g. CI pipes).
func promptSecret(cmd *cobra.Command, prompt string) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	// Try to read without echo if stdin is a real terminal.
	if isTerminal(os.Stdin) {
		val, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Fprintln(cmd.OutOrStdout()) // newline after hidden input
		if err != nil {
			return "", err
		}
		return string(val), nil
	}
	// Non-TTY fallback.
	return promptLine(cmd, "")
}

// promptLine reads a single line from stdin.
func promptLine(cmd *cobra.Command, prompt string) (string, error) {
	if prompt != "" {
		fmt.Fprint(cmd.OutOrStdout(), prompt)
	}
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
