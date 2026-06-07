package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// NewSavedCmd returns the "lnk saved" command tree.
func NewSavedCmd(noInput, flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor *bool) *cobra.Command {
	savedCmd := &cobra.Command{
		Use:   "saved",
		Short: "Manage saved LinkedIn jobs",
		Long:  "List, save, or remove jobs from your LinkedIn saved jobs collection.",
	}

	savedCmd.AddCommand(
		newSavedListCmd(flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor),
		newSavedAddCmd(flagQuiet, flagVerbose, flagDebug),
		newSavedRemoveCmd(flagQuiet, flagVerbose, flagDebug),
	)

	return savedCmd
}

// newSavedListCmd returns "lnk saved list".
func newSavedListCmd(flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor *bool) *cobra.Command {
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your saved LinkedIn jobs",
		Long:  "Fetch and display the jobs you have saved on LinkedIn.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedList(cmd, runSavedListOpts{
				json:    *flagJSON,
				plain:   *flagPlain,
				quiet:   *flagQuiet,
				verbose: *flagVerbose,
				debug:   *flagDebug,
				noColor: *flagNoColor,
				limit:   flagLimit,
			})
		},
	}

	cmd.Flags().IntVar(&flagLimit, "limit", 25, "Maximum number of saved jobs to return")

	return cmd
}

type runSavedListOpts struct {
	json, plain, quiet, verbose, debug, noColor bool
	limit                                       int
}

func runSavedList(cmd *cobra.Command, opts runSavedListOpts) error {
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
		api.WithErrWriter(os.Stderr),
	)

	// Fetch saved jobs.
	jobs, err := client.GetSavedJobs(opts.limit)
	if err != nil {
		return fmt.Errorf("fetching saved jobs: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No saved jobs found.")
		return nil
	}

	// Output.
	switch w.EffectiveFormat() {
	case output.FormatJSON:
		return w.JSON(jobs)
	case output.FormatPlain:
		for _, job := range jobs {
			easyApply := "false"
			if job.EasyApply {
				easyApply = "true"
			}
			w.Plain(job.ID, job.Title, job.Company, job.Location, job.PostedAt, easyApply)
		}
	default:
		printJobTable(w, jobs)
	}

	return nil
}

// printJobTable renders jobs in a formatted table (shared with search).
func printJobTable(w *output.Writer, jobs []types.JobCard) {
	cols := []output.Column{
		{Header: "ID"},
		{Header: "Title"},
		{Header: "Company"},
		{Header: "Location"},
		{Header: "Posted"},
		{Header: "Apply"},
	}

	rows := make([][]string, 0, len(jobs))
	for _, j := range jobs {
		apply := "External"
		if j.EasyApply {
			apply = "⚡ Easy Apply"
		}
		rows = append(rows, []string{
			j.ID,
			j.Title,
			j.Company,
			j.Location,
			j.PostedAt,
			apply,
		})
	}

	w.Table(cols, rows)
}

// newSavedAddCmd returns "lnk saved add <job-id>".
func newSavedAddCmd(flagQuiet, flagVerbose, flagDebug *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "add <job-id>",
		Short: "Save a job to your LinkedIn saved jobs",
		Long: `Save a job to your LinkedIn saved jobs collection.

The job-id can be a bare numeric ID (e.g. 4418763611) or a full URN
(urn:li:fsd_jobPosting:4418763611).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedAdd(cmd, args[0], *flagQuiet, *flagVerbose, *flagDebug)
		},
	}
}

func runSavedAdd(cmd *cobra.Command, jobID string, quiet, verbose, debug bool) error {
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

	client := api.New(creds,
		api.WithVerbose(verbose),
		api.WithDebug(debug),
	)

	if err := client.SaveJob(jobID); err != nil {
		return fmt.Errorf("saving job: %w", err)
	}

	if !quiet {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Job %s saved.\n", jobID)
	}
	return nil
}

// newSavedRemoveCmd returns "lnk saved remove <job-id>".
func newSavedRemoveCmd(flagQuiet, flagVerbose, flagDebug *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <job-id>",
		Short: "Remove a job from your LinkedIn saved jobs",
		Long: `Remove a job from your LinkedIn saved jobs collection.

The job-id can be a bare numeric ID (e.g. 4418763611) or a full URN
(urn:li:fsd_jobPosting:4418763611).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSavedRemove(cmd, args[0], *flagQuiet, *flagVerbose, *flagDebug)
		},
	}
}

func runSavedRemove(cmd *cobra.Command, jobID string, quiet, verbose, debug bool) error {
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

	client := api.New(creds,
		api.WithVerbose(verbose),
		api.WithDebug(debug),
	)

	if err := client.UnsaveJob(jobID); err != nil {
		return fmt.Errorf("removing saved job: %w", err)
	}

	if !quiet {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Job %s removed from saved jobs.\n", jobID)
	}
	return nil
}
