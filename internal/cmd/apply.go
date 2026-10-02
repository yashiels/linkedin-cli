package cmd

import (
	"bufio"
	"fmt"
	"io"
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

If Easy Apply is not available, an observed employer application URL is shown
as unverified. The LinkedIn listing URL is never presented as an employer URL.`,
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

var newApplyClient = api.New

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
	client := newApplyClient(creds,
		api.WithVerbose(opts.verbose),
		api.WithDebug(opts.debug),
		api.WithErrWriter(os.Stderr),
	)

	// Step 1: Fetch job detail for context.
	w.Info("Fetching job details…")
	detail, err := client.GetJobDetail(jobID)
	if err != nil {
		return fmt.Errorf("fetching job: %w", err)
	}
	if err := ensureApplicationCanProceed(detail); err != nil {
		return err
	}
	if detail.Application.Status != types.ApplicationAccepting {
		return writeNonEasyApplyResult(cmd.OutOrStdout(), w, detail, opts.json)
	}
	if !opts.json {
		printApplyJobHeader(cmd.OutOrStdout(), detail)
	}

	// Step 2: Check Easy Apply availability.
	w.Info("Checking application method…")
	status, err := client.CheckEasyApply(jobID)
	if err != nil {
		return fmt.Errorf("checking apply status: %w", err)
	}

	// Step 3: Not Easy Apply → external URL.
	if !status.Available {
		detail.EasyApply = false
		detail.Application.Status = types.ApplicationUnverified
		detail.Application.Source = "linkedin-easy-apply-check"
		detail.Application.Evidence = "LinkedIn did not return the current Easy Apply form control."
		detail.Application.Reason = "Easy Apply availability could not be confirmed."
		return writeNonEasyApplyResult(cmd.OutOrStdout(), w, detail, opts.json)
	}

	// Step 4: Show what will be submitted.
	if !opts.json {
		printEasyApplySummary(cmd.OutOrStdout(), status)
	}

	if opts.dryRun {
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
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Dry run — not submitting.")
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
	if opts.json {
		return w.JSON(map[string]interface{}{
			"success": true,
			"jobId":   detail.ID,
			"title":   detail.Title,
			"company": detail.Company,
		})
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "✓ Application submitted!")
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s at %s\n", detail.Title, detail.Company)

	return nil
}

func ensureApplicationCanProceed(detail *types.JobDetail) error {
	if detail.Application.Status == types.ApplicationClosed {
		return fmt.Errorf("job is closed; application will not be submitted")
	}
	return nil
}

func printApplyJobHeader(out io.Writer, detail *types.JobDetail) {
	_, _ = fmt.Fprintf(out, "\n%s\n", detail.Title)
	if detail.Company == "" && detail.Location == "" {
		return
	}
	parts := make([]string, 0, 2)
	if detail.Company != "" {
		parts = append(parts, detail.Company)
	}
	if detail.Location != "" {
		parts = append(parts, detail.Location)
	}
	_, _ = fmt.Fprintf(out, "%s\n\n", strings.Join(parts, " · "))
}

func printEasyApplySummary(out io.Writer, status *api.EasyApplyStatus) {
	_, _ = fmt.Fprintln(out, "Easy Apply available ⚡")
	_, _ = fmt.Fprintln(out, "\nApplication summary:")
	if status.Name != "" {
		_, _ = fmt.Fprintf(out, "  Name:   %s\n", status.Name)
	}
	if status.Email != "" {
		_, _ = fmt.Fprintf(out, "  Email:  %s\n", status.Email)
	}
	if status.Phone != "" {
		_, _ = fmt.Fprintf(out, "  Phone:  %s\n", status.Phone)
	}
	if status.Resume != "" {
		_, _ = fmt.Fprintf(out, "  Resume: %s\n", status.Resume)
	}
	if status.Name == "" && status.Email == "" {
		_, _ = fmt.Fprintln(out, "  (profile data will be submitted from your LinkedIn profile)")
	}
	_, _ = fmt.Fprintln(out)
}

func writeNonEasyApplyResult(out io.Writer, writer *output.Writer, detail *types.JobDetail, jsonMode bool) error {
	if jsonMode {
		return writer.JSON(map[string]interface{}{
			"easyApply":         false,
			"externalUrl":       detail.Application.ApplyURL,
			"applyUrl":          detail.Application.ApplyURL,
			"listingUrl":        detail.ListingURL,
			"applicationStatus": detail.Application.Status,
			"jobId":             detail.ID,
			"title":             detail.Title,
			"company":           detail.Company,
		})
	}
	printApplyJobHeader(out, detail)
	printNonEasyApply(out, detail)
	return nil
}

func printNonEasyApply(out io.Writer, detail *types.JobDetail) {
	_, _ = fmt.Fprintln(out, "This job does not have a verified LinkedIn Easy Apply control.")
	if detail.Application.ApplyURL != "" {
		_, _ = fmt.Fprintf(out, "Observed employer application URL (unverified): %s\n", detail.Application.ApplyURL)
	} else {
		_, _ = fmt.Fprintln(out, "No employer application URL was observed.")
	}
	if detail.ListingURL != "" {
		_, _ = fmt.Fprintf(out, "LinkedIn listing: %s\n", detail.ListingURL)
	}
}

// isTerminalStdin reports whether stdin is a real terminal.
func isTerminalStdin() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
