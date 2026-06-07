package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// NewApplyCmd returns the "lnk apply" command.
func NewApplyCmd(noInput, flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor *bool) *cobra.Command {
	var (
		flagDryRun  bool
		flagConfirm bool
	)

	cmd := &cobra.Command{
		Use:   "apply <job-id>",
		Short: "Apply to a LinkedIn job via Easy Apply",
		Long: `Apply to a LinkedIn job posting.

This command:
  1. Fetches the job details (title, company)
  2. Checks whether Easy Apply is available
  3. Displays what will be submitted (profile data from LinkedIn)
  4. Asks for confirmation (unless --confirm or --no-input is set)
  5. Submits the application

If Easy Apply is not available, the external application URL is printed
instead and no application is submitted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runApply(cmd, args[0], runApplyOpts{
				json:    *flagJSON,
				plain:   *flagPlain,
				quiet:   *flagQuiet,
				verbose: *flagVerbose,
				debug:   *flagDebug,
				noColor: *flagNoColor,
				noInput: *noInput,
				dryRun:  flagDryRun,
				confirm: flagConfirm,
			})
		},
	}

	cmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Show what would be submitted without actually applying")
	cmd.Flags().BoolVar(&flagConfirm, "confirm", false, "Skip confirmation prompt and apply immediately")

	return cmd
}

type runApplyOpts struct {
	json, plain, quiet, verbose, debug, noColor bool
	noInput, dryRun, confirm                    bool
}

func runApply(cmd *cobra.Command, jobID string, opts runApplyOpts) error {
	store, err := auth.Default()
	if err != nil {
		return err
	}
	creds, err := store.Load()
	if err != nil {
		return err
	}
	if creds.LiAt == "" || creds.CSRFToken == "" {
		return types.AuthError("not logged in — run: lnk auth login")
	}

	// Build output writer.
	var outFmt output.Format
	switch {
	case opts.json:
		outFmt = output.FormatJSON
	case opts.plain:
		outFmt = output.FormatPlain
	default:
		outFmt = output.FormatAuto
	}

	w := output.New(
		output.WithFormat(outFmt),
		output.WithNoColor(opts.noColor),
		output.WithQuiet(opts.quiet),
		output.WithStdout(cmd.OutOrStdout()),
	)

	// Build API client.
	client := api.New(creds,
		api.WithVerbose(opts.verbose),
		api.WithDebug(opts.debug),
	)

	// Step 1: Fetch job detail for context.
	w.Info("Fetching job details…")
	detail, err := client.GetJobDetail(jobID)
	if err != nil {
		return fmt.Errorf("fetching job: %w", err)
	}

	// Print job header.
	fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", detail.Title)
	if detail.Company != "" || detail.Location != "" {
		parts := []string{}
		if detail.Company != "" {
			parts = append(parts, detail.Company)
		}
		if detail.Location != "" {
			parts = append(parts, detail.Location)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n", strings.Join(parts, " · "))
	}

	// Step 2: Check Easy Apply availability.
	w.Info("Checking application method…")
	status, err := client.CheckEasyApply(jobID)
	if err != nil {
		return fmt.Errorf("checking apply status: %w", err)
	}

	// Step 3: Not Easy Apply → external URL.
	if !status.Available && !detail.EasyApply {
		externalURL := api.ExternalApplyURL(jobID)
		fmt.Fprintf(cmd.OutOrStdout(), "This job does not support Easy Apply.\n")
		fmt.Fprintf(cmd.OutOrStdout(), "Apply externally at: %s\n", externalURL)

		if opts.json {
			return w.JSON(map[string]interface{}{
				"easyApply":   false,
				"externalUrl": externalURL,
				"jobId":       detail.ID,
				"title":       detail.Title,
				"company":     detail.Company,
			})
		}
		return nil
	}

	// Step 4: Show what will be submitted.
	fmt.Fprintf(cmd.OutOrStdout(), "Easy Apply available ⚡\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Application summary:\n")
	if status.Name != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Name:   %s\n", status.Name)
	}
	if status.Email != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Email:  %s\n", status.Email)
	}
	if status.Phone != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Phone:  %s\n", status.Phone)
	}
	if status.Resume != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Resume: %s\n", status.Resume)
	}
	if status.Name == "" && status.Email == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  (profile data will be submitted from your LinkedIn profile)\n")
	}
	fmt.Fprintln(cmd.OutOrStdout())

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run — not submitting.\n")
		if opts.json {
			return w.JSON(map[string]interface{}{
				"dryRun":    true,
				"easyApply": true,
				"jobId":     detail.ID,
				"title":     detail.Title,
				"company":   detail.Company,
				"name":      status.Name,
				"email":     status.Email,
				"resume":    status.Resume,
			})
		}
		return nil
	}

	// Step 5: Confirm (unless --confirm or --no-input).
	if !opts.confirm {
		if opts.noInput {
			return fmt.Errorf("confirmation required but --no-input is set (use --confirm to skip)")
		}
		if !isTerminalStdin() {
			return fmt.Errorf("no TTY detected: use --confirm to skip confirmation prompt")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Submit application? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer != "y" && answer != "yes" {
				fmt.Fprintf(cmd.OutOrStdout(), "Application cancelled.\n")
				return nil
			}
		}
	}

	// Step 6: Submit.
	w.Info("Submitting application…")
	if err := client.SubmitEasyApply(jobID, status); err != nil {
		return fmt.Errorf("submitting application: %w", err)
	}

	// Step 7: Success.
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Application submitted!\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  %s at %s\n", detail.Title, detail.Company)

	if opts.json {
		return w.JSON(map[string]interface{}{
			"success": true,
			"jobId":   detail.ID,
			"title":   detail.Title,
			"company": detail.Company,
		})
	}

	return nil
}

// isTerminalStdin reports whether stdin is a real terminal.
func isTerminalStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
