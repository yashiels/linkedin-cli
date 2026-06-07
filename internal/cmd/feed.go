package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// NewFeedCmd returns the "lnk feed" command.
// The boolean pointers reference root-level persistent flags so that
// --json, --plain, --no-color, --quiet, --verbose, --debug all work globally.
func NewFeedCmd(flagJSON, flagPlain, flagNoColor, flagQuiet, flagVerbose, flagDebug *bool) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Browse LinkedIn's recommended job feed",
		Long: `Fetch LinkedIn's personalised job recommendations (Jobs You May Be Interested In).

These are the same jobs shown on the LinkedIn Jobs home page.

Examples:
  lnk feed
  lnk feed --limit 50
  lnk feed --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outFmt := resolveFormat(flagJSON, flagPlain)
			return runFeed(feedFlags{
				limit:   limit,
				outFmt:  outFmt,
				noColor: *flagNoColor,
				quiet:   *flagQuiet,
				verbose: *flagVerbose,
				debug:   *flagDebug,
			})
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 25, "Maximum number of jobs to return")

	return cmd
}

type feedFlags struct {
	limit   int
	outFmt  string
	noColor bool
	quiet   bool
	verbose bool
	debug   bool
}

func runFeed(flags feedFlags) error {
	// Load credentials.
	store, err := auth.Default()
	if err != nil {
		return err
	}
	creds, err := store.Load()
	if err != nil {
		return err
	}
	if creds.LiAt == "" {
		return types.AuthError("not logged in — run: lnk auth login")
	}

	// Build output writer.
	ow := buildOutputWriter(flags.outFmt, flags.noColor, flags.quiet)

	// Build API client.
	apiOpts := []api.Option{}
	if flags.verbose {
		apiOpts = append(apiOpts, api.WithVerbose(true), api.WithErrWriter(os.Stderr))
	}
	if flags.debug {
		apiOpts = append(apiOpts, api.WithDebug(true), api.WithErrWriter(os.Stderr))
	}
	client := api.New(creds, apiOpts...)

	// Paginate to collect up to limit results.
	allCards := make([]types.JobCard, 0, flags.limit)
	pageSize := 10
	start := 0
	total := 0

	for len(allCards) < flags.limit {
		fetch := pageSize
		if remaining := flags.limit - len(allCards); remaining < pageSize {
			fetch = remaining
		}

		cards, tot, err := client.FetchFeed(fetch, start)
		if err != nil {
			return fmt.Errorf("feed: %w", err)
		}
		total = tot
		allCards = append(allCards, cards...)

		if len(cards) < fetch || (total > 0 && len(allCards) >= total) {
			break
		}
		start += fetch
	}

	if len(allCards) > flags.limit {
		allCards = allCards[:flags.limit]
	}

	if !flags.quiet && total > 0 {
		ow.Info("Found %d recommended jobs (showing %d)", total, len(allCards))
	}

	// Render output.
	if len(allCards) == 0 {
		if strings.ToLower(flags.outFmt) == "json" {
			_ = ow.JSON([]types.JobCard{})
		} else {
			fmt.Fprintln(os.Stdout, "No jobs found in your feed.")
		}
		return nil
	}

	renderJobCards(ow, flags.outFmt, allCards)
	return nil
}
