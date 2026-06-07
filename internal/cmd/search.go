package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// knownLocations maps common location names (lowercase) to LinkedIn geo URN IDs.
var knownLocations = map[string]int64{
	"south africa": 104035573,
	"za":           104035573,
	"cape town":    105013608,
	"ct":           105013608,
	"johannesburg": 104273735,
	"jhb":          104273735,
	"joburg":       104273735,
	"pretoria":     105944906,
	"pta":          105944906,
	"durban":       106463985,
	"dbn":          106463985,
	"remote":       91000010, // LinkedIn global remote geo ID
}

// jobTypeMap maps friendly names to LinkedIn job type codes.
var jobTypeMap = map[string]string{
	"full-time":  "F",
	"fulltime":   "F",
	"full":       "F",
	"part-time":  "P",
	"parttime":   "P",
	"part":       "P",
	"contract":   "C",
	"temporary":  "T",
	"temp":       "T",
	"internship": "I",
	"intern":     "I",
	"volunteer":  "V",
	"other":      "O",
}

// experienceMap maps friendly level names to LinkedIn experience codes.
var experienceMap = map[string]string{
	"internship":  "1",
	"intern":      "1",
	"entry":       "2",
	"entry-level": "2",
	"associate":   "3",
	"mid-senior":  "4",
	"mid":         "4",
	"senior":      "4",
	"director":    "5",
	"executive":   "6",
	"exec":        "6",
}

// postedRangeMap maps human-readable time ranges to LinkedIn filter codes.
var postedRangeMap = map[string]string{
	"24h":   "r86400",
	"day":   "r86400",
	"week":  "r604800",
	"1w":    "r604800",
	"month": "r2592000",
	"1m":    "r2592000",
}

// NewSearchCmd returns the "lnk search" command.
// The boolean pointers reference root-level persistent flags so that
// --json, --plain, --no-color, --quiet, --verbose, --debug all work globally.
func NewSearchCmd(flagJSON, flagPlain, flagNoColor, flagQuiet, flagVerbose, flagDebug *bool) *cobra.Command {
	var (
		location  string
		jobTypes  string
		expLevels string
		easyApply bool
		remote    bool
		sortOrder string
		limit     int
		posted    string
	)

	cmd := &cobra.Command{
		Use:   "search <keywords>",
		Short: "Search LinkedIn job listings",
		Long: `Search LinkedIn for jobs matching the given keywords and filters.

Examples:
  lnk search "software engineer"
  lnk search "backend developer" --location "Cape Town" --type full-time
  lnk search "product manager" -l "South Africa" -e mid-senior --easy-apply
  lnk search "data scientist" --posted week --sort recent --limit 50`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keywords := strings.Join(args, " ")
			outFmt := resolveFormat(flagJSON, flagPlain)
			return runSearch(keywords, searchFlags{
				location:  location,
				jobTypes:  jobTypes,
				expLevels: expLevels,
				easyApply: easyApply,
				remote:    remote,
				sortOrder: sortOrder,
				limit:     limit,
				posted:    posted,
				outFmt:    outFmt,
				noColor:   *flagNoColor,
				quiet:     *flagQuiet,
				verbose:   *flagVerbose,
				debug:     *flagDebug,
			})
		},
	}

	f := cmd.Flags()
	f.StringVarP(&location, "location", "l", "", "Location name or geo URN (e.g. \"Cape Town\", \"South Africa\")")
	f.StringVarP(&jobTypes, "type", "t", "", "Comma-separated job types: full-time, part-time, contract, temporary, internship, volunteer")
	f.StringVarP(&expLevels, "experience", "e", "", "Comma-separated experience levels: internship, entry, associate, mid-senior, director, executive")
	f.BoolVar(&easyApply, "easy-apply", false, "Filter for Easy Apply jobs only")
	f.BoolVar(&remote, "remote", false, "Filter for remote jobs")
	f.StringVar(&sortOrder, "sort", "relevant", "Sort order: recent or relevant (default: relevant)")
	f.IntVarP(&limit, "limit", "n", 25, "Maximum number of results to return")
	f.StringVar(&posted, "posted", "", "Posted within: 24h, week, month")

	return cmd
}

// resolveFormat converts the --json / --plain boolean flags to a format string.
func resolveFormat(flagJSON, flagPlain *bool) string {
	if flagJSON != nil && *flagJSON {
		return "json"
	}
	if flagPlain != nil && *flagPlain {
		return "plain"
	}
	return "table"
}

type searchFlags struct {
	location  string
	jobTypes  string
	expLevels string
	easyApply bool
	remote    bool
	sortOrder string
	limit     int
	posted    string
	outFmt    string
	noColor   bool
	quiet     bool
	verbose   bool
	debug     bool
}

func runSearch(keywords string, flags searchFlags) error {
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

	// Resolve location to geo URN.
	geoURN, err := resolveLocation(flags.location)
	if err != nil {
		return err
	}

	// Parse job types.
	jobTypeCodes, err := parseJobTypes(flags.jobTypes)
	if err != nil {
		return err
	}

	// Parse experience levels.
	expCodes, err := parseExperience(flags.expLevels)
	if err != nil {
		return err
	}

	// Parse sort.
	sortCode := parseSortOrder(flags.sortOrder)

	// Parse posted range.
	postedCode := ""
	if flags.posted != "" {
		code, ok := postedRangeMap[strings.ToLower(flags.posted)]
		if !ok {
			return fmt.Errorf("unknown --posted value %q; use: 24h, week, month", flags.posted)
		}
		postedCode = code
	}

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
	// NOTE: LinkedIn's Voyager GraphQL API (voyagerJobsDashJobCards) does not
	// accept filter field names inside selectedFilters for external callers; the
	// filter variables are passed as top-level variables and LinkedIn may or may
	// not honour them depending on the query schema version in use.
	// Easy Apply is additionally enforced client-side since we can detect it from
	// the EASY_APPLY_TEXT footerItem in the parsed job card.
	allCards := make([]types.JobCard, 0, flags.limit)
	pageSize := 10
	start := 0
	total := 0

	// Fetch extra results when easy-apply filtering client-side to hit the limit.
	fetchMultiplier := 1
	if flags.easyApply {
		fetchMultiplier = 3
	}

	for len(allCards) < flags.limit {
		fetch := pageSize * fetchMultiplier
		remaining := flags.limit - len(allCards)
		if fetch > remaining*fetchMultiplier {
			fetch = remaining * fetchMultiplier
		}
		if fetch < 1 {
			fetch = pageSize
		}

		params := api.JobSearchParams{
			Keywords:    keywords,
			GeoURN:      geoURN,
			JobTypes:    jobTypeCodes,
			Experience:  expCodes,
			Sort:        sortCode,
			PostedRange: postedCode,
			EasyApply:   flags.easyApply,
			Remote:      flags.remote,
			Count:       fetch,
			Start:       start,
		}

		cards, tot, err := client.SearchJobs(params)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		total = tot

		// Client-side Easy Apply filter: only add cards marked as Easy Apply.
		// This supplements the server-side filter (which LinkedIn may not honour
		// for all query versions) with a reliable post-processing step.
		if flags.easyApply {
			for _, c := range cards {
				if c.EasyApply {
					allCards = append(allCards, c)
				}
			}
		} else {
			allCards = append(allCards, cards...)
		}

		if len(cards) < fetch || (total > 0 && start+len(cards) >= total) {
			break
		}
		start += fetch
	}

	if len(allCards) > flags.limit {
		allCards = allCards[:flags.limit]
	}

	if !flags.quiet && total > 0 {
		ow.Info("Found %d jobs (showing %d)", total, len(allCards))
	}

	renderJobCards(ow, flags.outFmt, allCards)
	return nil
}

// resolveLocation converts a location name to a geo URN string.
// Returns empty string if location is empty. Returns error on unknown name.
func resolveLocation(location string) (string, error) {
	if location == "" {
		return "", nil
	}

	// If already a URN, use as-is.
	if strings.HasPrefix(location, "urn:li:") {
		return location, nil
	}

	// Lookup in known locations.
	key := strings.ToLower(strings.TrimSpace(location))
	if id, ok := knownLocations[key]; ok {
		return api.GeoURNFromID(id), nil
	}

	// Print available locations and return error.
	fmt.Fprintln(os.Stderr, "Unknown location. Available locations:")
	display := []struct {
		name string
		id   int64
	}{
		{"South Africa", 104035573},
		{"Cape Town", 105013608},
		{"Johannesburg", 104273735},
		{"Pretoria", 105944906},
		{"Durban", 106463985},
		{"Remote", 91000010},
	}
	for _, loc := range display {
		fmt.Fprintf(os.Stderr, "  %-20s (urn:li:fsd_geo:%d)\n", loc.name, loc.id)
	}
	fmt.Fprintln(os.Stderr, "\nOr pass a raw geo URN: --location urn:li:fsd_geo:<id>")
	return "", fmt.Errorf("unknown location %q", location)
}

// parseJobTypes converts comma-separated type names to LinkedIn type codes.
func parseJobTypes(jobTypes string) ([]string, error) {
	if jobTypes == "" {
		return nil, nil
	}
	codes := make([]string, 0)
	for _, t := range strings.Split(jobTypes, ",") {
		t = strings.TrimSpace(strings.ToLower(t))
		code, ok := jobTypeMap[t]
		if !ok {
			// Accept raw codes (F, P, C, T, I, V, O) directly.
			up := strings.ToUpper(t)
			if len(up) == 1 && strings.ContainsRune("FPCTIVO", rune(up[0])) {
				codes = append(codes, up)
				continue
			}
			return nil, fmt.Errorf("unknown job type %q; use: full-time, part-time, contract, temporary, internship, volunteer", t)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// parseExperience converts comma-separated experience level names to codes.
func parseExperience(levels string) ([]string, error) {
	if levels == "" {
		return nil, nil
	}
	codes := make([]string, 0)
	for _, l := range strings.Split(levels, ",") {
		l = strings.TrimSpace(strings.ToLower(l))
		code, ok := experienceMap[l]
		if !ok {
			// Accept raw numeric codes (1-6) directly.
			if len(l) == 1 && l >= "1" && l <= "6" {
				codes = append(codes, l)
				continue
			}
			return nil, fmt.Errorf("unknown experience level %q; use: internship, entry, associate, mid-senior, director, executive", l)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// parseSortOrder converts a friendly sort name to a LinkedIn sort code.
func parseSortOrder(sort string) string {
	switch strings.ToLower(sort) {
	case "recent", "r":
		return "R"
	default:
		return "DD"
	}
}

// buildOutputWriter creates an output.Writer from format and display flags.
func buildOutputWriter(outFmt string, noColor, quiet bool) *output.Writer {
	opts := []output.Option{
		output.WithNoColor(noColor),
		output.WithQuiet(quiet),
	}
	switch strings.ToLower(outFmt) {
	case "json":
		opts = append(opts, output.WithFormat(output.FormatJSON))
	case "plain", "tsv":
		opts = append(opts, output.WithFormat(output.FormatPlain))
	case "table":
		opts = append(opts, output.WithFormat(output.FormatTable))
	}
	return output.New(opts...)
}

// renderJobCards outputs job cards in the selected format.
func renderJobCards(ow *output.Writer, outFmt string, cards []types.JobCard) {
	switch strings.ToLower(outFmt) {
	case "json":
		_ = ow.JSON(cards)
	case "plain", "tsv":
		for _, c := range cards {
			apply := ""
			if c.EasyApply {
				apply = "easy-apply"
			}
			ow.Plain(c.ID, c.Title, c.Company, c.Location, c.PostedAt, apply)
		}
	default:
		renderJobTable(ow, cards)
	}
}

// renderJobTable renders job cards as an aligned table.
func renderJobTable(ow *output.Writer, cards []types.JobCard) {
	easyColor := color.New(color.FgCyan, color.Bold)

	cols := []output.Column{
		{Header: "ID"},
		{Header: "TITLE"},
		{Header: "COMPANY"},
		{Header: "LOCATION"},
		{Header: "POSTED"},
		{Header: "APPLY"},
	}

	rows := make([][]string, 0, len(cards))
	for _, c := range cards {
		applyStr := ""
		if c.EasyApply {
			applyStr = easyColor.Sprint("⚡ Easy")
		}
		rows = append(rows, []string{
			c.ID,
			truncateStr(c.Title, 40),
			truncateStr(c.Company, 25),
			truncateStr(c.Location, 25),
			c.PostedAt,
			applyStr,
		})
	}

	ow.Table(cols, rows)
}

// truncateStr truncates s to max runes, appending "…" if truncated.
func truncateStr(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
