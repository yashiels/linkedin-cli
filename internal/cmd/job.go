package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	htmlutil "github.com/yashiels/linkedin-cli/internal/html"
	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// NewJobCmd returns the "lnk job" command.
func NewJobCmd(noInput, flagJSON, flagPlain, flagQuiet, flagVerbose, flagDebug, flagNoColor *bool) *cobra.Command {
	var flagOpen bool

	cmd := &cobra.Command{
		Use:   "job <job-id>",
		Short: "View full details of a LinkedIn job posting",
		Long: `Fetch and display the full details of a LinkedIn job posting.

The job-id can be a bare numeric ID (e.g. 4418763611) or a full URN
(urn:li:fsd_jobPosting:4418763611).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJobDetail(cmd, args[0], runJobDetailOpts{
				json:    *flagJSON,
				plain:   *flagPlain,
				quiet:   *flagQuiet,
				verbose: *flagVerbose,
				debug:   *flagDebug,
				noColor: *flagNoColor,
				open:    flagOpen,
			})
		},
	}

	cmd.Flags().BoolVar(&flagOpen, "open", false, "Open the job URL in the default browser")

	return cmd
}

type runJobDetailOpts struct {
	json, plain, quiet, verbose, debug, noColor, open bool
}

func runJobDetail(cmd *cobra.Command, jobID string, opts runJobDetailOpts) error {
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
	)

	// Build API client.
	clientOpts := []api.Option{
		api.WithVerbose(opts.verbose),
		api.WithDebug(opts.debug),
		api.WithErrWriter(os.Stderr),
	}
	client := api.New(creds, clientOpts...)

	// Fetch job detail.
	detail, err := client.GetJobDetail(jobID)
	if err != nil {
		return fmt.Errorf("fetching job: %w", err)
	}

	// Open in browser if requested.
	if opts.open {
		jobURL := "https://www.linkedin.com/jobs/view/" + detail.ID
		if err := exec.Command("open", jobURL).Start(); err != nil {
			w.Warn("could not open browser: %v", err)
		} else {
			w.Info("Opening %s", jobURL)
		}
	}

	// Output.
	switch w.EffectiveFormat() {
	case output.FormatJSON:
		return w.JSON(detail)
	case output.FormatPlain:
		printJobDetailPlain(w, detail)
	default:
		printJobDetailHuman(cmd.OutOrStdout(), detail)
	}

	return nil
}

// printJobDetailHuman renders a human-friendly job detail view to the given writer.
func printJobDetailHuman(out io.Writer, d *types.JobDetail) {
	// Title line.
	title := d.Title
	if title == "" {
		title = "(untitled)"
	}
	fmt.Fprintf(out, "%s\n", title)

	// Company · Location.
	var headerParts []string
	if d.Company != "" {
		headerParts = append(headerParts, d.Company)
	}
	if d.Location != "" {
		headerParts = append(headerParts, d.Location)
	}
	if len(headerParts) > 0 {
		fmt.Fprintf(out, "%s\n", strings.Join(headerParts, " · "))
	}

	// Metadata line: posted, applicants, easy apply.
	var metaParts []string
	if d.PostedAt != "" {
		metaParts = append(metaParts, "Posted "+d.PostedAt)
	}
	if d.ApplicantCount != "" {
		metaParts = append(metaParts, d.ApplicantCount)
	}
	if d.EasyApply {
		metaParts = append(metaParts, "⚡ Easy Apply")
	}
	if len(metaParts) > 0 {
		fmt.Fprintf(out, "%s\n", strings.Join(metaParts, " · "))
	}

	// Seniority / employment type.
	var typesParts []string
	if d.SeniorityLevel != "" {
		typesParts = append(typesParts, d.SeniorityLevel)
	}
	if d.EmploymentType != "" {
		typesParts = append(typesParts, d.EmploymentType)
	}
	if len(typesParts) > 0 {
		fmt.Fprintf(out, "%s\n", strings.Join(typesParts, " · "))
	}

	if d.Expired {
		fmt.Fprintf(out, "\n⚠  This job posting has expired.\n")
	}

	// Description section.
	if d.Description != "" {
		fmt.Fprintf(out, "\nDescription:\n")
		text := htmlutil.ToText(d.Description)
		fmt.Fprintf(out, "%s\n", htmlutil.Indent(text, "  "))
	}

	// Skills.
	if len(d.Skills) > 0 {
		fmt.Fprintf(out, "\nSkills:\n")
		for _, s := range d.Skills {
			fmt.Fprintf(out, "  • %s\n", s)
		}
	}

	// Salary.
	if d.Salary != "" {
		fmt.Fprintf(out, "\nSalary: %s\n", d.Salary)
	} else if d.SalaryMin > 0 && d.SalaryMax > 0 {
		curr := d.SalaryCurr
		if curr == "" {
			curr = "USD"
		}
		fmt.Fprintf(out, "\nSalary: %s %s–%s/yr\n",
			curr,
			formatSalary(d.SalaryMin),
			formatSalary(d.SalaryMax),
		)
	}

	// Listing URL.
	if d.ListingURL != "" {
		fmt.Fprintf(out, "\nURL: %s\n", d.ListingURL)
	}
}

// printJobDetailPlain prints tab-separated job fields for piping.
func printJobDetailPlain(w *output.Writer, d *types.JobDetail) {
	easyApply := "false"
	if d.EasyApply {
		easyApply = "true"
	}
	desc := htmlutil.ToText(d.Description)
	// Collapse newlines for plain output.
	desc = strings.ReplaceAll(desc, "\n", " ")

	w.Plain(d.ID, d.Title, d.Company, d.Location, d.PostedAt, easyApply, d.Salary, desc)
}

// formatSalary formats a salary integer with thousands separators.
func formatSalary(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	// Insert commas.
	result := make([]byte, 0, len(s)+len(s)/3)
	for i, ch := range s {
		pos := len(s) - i
		if i > 0 && pos%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return string(result)
}
