package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// ownProfileMeta holds identity info extracted from /voyager/api/me.
type ownProfileMeta struct {
	VanityName     string          // LinkedIn public identifier / URL slug
	ProfileURN     string          // urn:li:fsd_profile:... (dashEntityUrn from miniProfile entity)
	MiniProfileRaw json.RawMessage // raw miniProfile entity from included[] — usable as basic profile data
}

// NewProfileCmd returns the "lnk profile" command.
func NewProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile [username]",
		Short: "View a LinkedIn profile",
		Long: `Show a LinkedIn member profile summary.

When called without arguments, displays your own profile.
When called with a username (LinkedIn public URL slug), displays that member's profile.

Examples:
  lnk profile
  lnk profile yashielsookdeo
  lnk profile satyanadella --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(cmd)
			if err != nil {
				return err
			}

			var vanityName, profileURN string
			var miniProfileRaw json.RawMessage
			if len(args) == 1 {
				vanityName = args[0]
			} else {
				meta, merr := getOwnProfileMeta(client)
				if merr != nil {
					return fmt.Errorf("fetching own profile: %w\nHint: try 'lnk profile <your-username>'", merr)
				}
				vanityName = meta.VanityName
				profileURN = meta.ProfileURN
				miniProfileRaw = meta.MiniProfileRaw
			}

			profile, err := fetchProfile(client, vanityName, profileURN, miniProfileRaw)
			if err != nil {
				return err
			}

			out := newOutputWriter(cmd)
			if isJSONMode(cmd) {
				return out.JSON(profile)
			}

			printProfile(cmd, profile)
			return nil
		},
	}
	return cmd
}

// getOwnProfileMeta calls /voyager/api/me and extracts:
//   - publicIdentifier (the vanity name / URL slug)
//   - dashEntityUrn    (the fsd_profile URN — needed for GraphQL profileUrn variable)
//   - the raw miniProfile entity (firstName, lastName, occupation, etc.)
//
// The Voyager /me response uses the normalized JSON format:
//
//	data["*miniProfile"] → URN reference string
//	included[]           → dereferenced entities, one of which is the MiniProfile
func getOwnProfileMeta(client *api.Client) (*ownProfileMeta, error) {
	raw, err := client.Get("/voyager/api/me", nil)
	if err != nil {
		return nil, err
	}

	meta := &ownProfileMeta{}

	// Current normalized format: publicIdentifier and dashEntityUrn live in included[].
	inclRaw := jget(raw, "included")
	if inclRaw != nil {
		var included []json.RawMessage
		if json.Unmarshal(inclRaw, &included) == nil {
			for _, entity := range included {
				if id := jstr(jget(entity, "publicIdentifier")); id != "" {
					meta.VanityName = id
					meta.ProfileURN = jstr(jget(entity, "dashEntityUrn"))
					meta.MiniProfileRaw = entity
					break
				}
			}
		}
	}

	// Legacy / embedded fallbacks.
	if meta.VanityName == "" {
		meta.VanityName = jstr(jget(raw, "miniProfile", "publicIdentifier"))
	}
	if meta.VanityName == "" {
		meta.VanityName = jstr(jget(raw, "publicIdentifier"))
	}

	if meta.VanityName == "" {
		return nil, fmt.Errorf("could not determine own vanity name from /me response")
	}
	return meta, nil
}

// fetchProfile fetches and assembles a Profile for the given vanity name.
// profileURN is optional — the fsd_profile URN from /me; used when available
// to bypass the deprecated REST endpoint.
// miniProfileRaw is optional — the raw miniProfile entity from /me; used as
// a base when no REST profile data is available.
func fetchProfile(client *api.Client, vanityName, profileURN string, miniProfileRaw json.RawMessage) (*types.Profile, error) {
	p := &types.Profile{VanityName: vanityName}

	// Seed basic fields from miniProfile if we already have it (own profile case).
	if miniProfileRaw != nil {
		parseMiniProfile(p, miniProfileRaw)
	}

	// 1. Fetch basic profile info via REST.
	basicRaw, err := client.Get("/voyager/api/identity/profiles/"+url.PathEscape(vanityName), nil)
	if err == nil {
		parseBasicProfile(p, basicRaw)
	} else {
		// REST endpoint is deprecated (410 Gone) — fall back to GraphQL.
		graphRaw, gerr := fetchProfileGraphQL(client, vanityName, profileURN)
		if gerr == nil {
			parseBasicProfile(p, graphRaw)
		}
		// If both fail and we had miniProfile data, continue with what we have.
		if p.FirstName == "" && p.LastName == "" && miniProfileRaw == nil {
			return nil, fmt.Errorf("fetching profile for %q: %w", vanityName, err)
		}
	}

	// 2. Fetch experience and education from profileView.
	viewRaw, err := client.Get(
		"/voyager/api/identity/profiles/"+url.PathEscape(vanityName)+"/profileView",
		nil,
	)
	if err == nil {
		parseProfileView(p, viewRaw)
	}

	// 3. Fetch connection count.
	netRaw, err := client.Get(
		"/voyager/api/identity/profiles/"+url.PathEscape(vanityName)+"/networkinfo",
		nil,
	)
	if err == nil {
		parseNetworkInfo(p, netRaw)
	}

	return p, nil
}

// fetchProfileGraphQL uses the Voyager GraphQL API as a fallback.
// If profileURN is non-empty it uses it as the profileUrn variable (required by
// ProfileTopCardCore). Falls back to memberIdentity for older query variants.
func fetchProfileGraphQL(client *api.Client, vanityName, profileURN string) (json.RawMessage, error) {
	// Primary: ProfileTopCardCore requires a full fsd_profile URN.
	if profileURN != "" {
		vars := map[string]interface{}{
			"profileUrn": profileURN,
		}
		raw, err := client.QueryGraphQL(
			"ProfileTopCardCore",
			"voyagerIdentityDashProfiles.f3eabbfa5c523c4af4d29c7de3a4a33e",
			vars,
		)
		if err == nil {
			if result := extractProfileFromGraphQL(raw); result != nil {
				return result, nil
			}
		}
	}

	// Fallback: try memberIdentity variable (works with some query versions).
	vars := map[string]interface{}{
		"memberIdentity": vanityName,
	}
	raw, err := client.QueryGraphQL(
		"ProfileTopCardCore",
		"voyagerIdentityDashProfiles.f3eabbfa5c523c4af4d29c7de3a4a33e",
		vars,
	)
	if err != nil {
		return nil, err
	}

	if result := extractProfileFromGraphQL(raw); result != nil {
		return result, nil
	}
	return nil, fmt.Errorf("no profile data returned from GraphQL")
}

// extractProfileFromGraphQL navigates a GraphQL profile response to find the
// profile data object. LinkedIn wraps it either as data.<key>.elements[0] (list
// result) or as data.<key> directly (single-object result).
func extractProfileFromGraphQL(raw json.RawMessage) json.RawMessage {
	// Try list form: data.<key>.elements[0].
	if elems := jdataElems(raw); len(elems) > 0 {
		return elems[0]
	}
	// Try single-object form: data.<key> directly (no elements array).
	dataRaw := jget(raw, "data")
	if dataRaw == nil {
		return nil
	}
	var dataMap map[string]json.RawMessage
	if json.Unmarshal(dataRaw, &dataMap) != nil {
		return nil
	}
	for _, v := range dataMap {
		// Return the first non-trivial object (skip scalars and empty maps).
		var check map[string]json.RawMessage
		if json.Unmarshal(v, &check) == nil && len(check) > 2 {
			return v
		}
	}
	return nil
}

// parseMiniProfile populates basic Profile fields from a raw MiniProfile entity
// (as returned in /voyager/api/me included[]). Used when REST profile is unavailable.
func parseMiniProfile(p *types.Profile, raw json.RawMessage) {
	if raw == nil {
		return
	}
	if fn := jstr(jget(raw, "firstName")); fn != "" {
		p.FirstName = fn
	}
	if ln := jstr(jget(raw, "lastName")); ln != "" {
		p.LastName = ln
	}
	// MiniProfile uses "occupation" for the headline/tagline.
	if occ := jstr(jget(raw, "occupation")); occ != "" && p.Headline == "" {
		p.Headline = occ
	}
	if urn := jstr(jget(raw, "dashEntityUrn")); urn != "" && p.URN == "" {
		p.URN = urn
	}
	if id := jstr(jget(raw, "publicIdentifier")); id != "" {
		p.VanityName = id
	}
}

// parseBasicProfile extracts name, headline, location, and summary
// from the /voyager/api/identity/profiles/<id> response or a GraphQL element.
func parseBasicProfile(p *types.Profile, raw json.RawMessage) {
	if raw == nil {
		return
	}

	// Try direct fields first (REST endpoint format).
	p.FirstName = firstNonEmpty(
		jstr(jget(raw, "firstName")),
	)
	p.LastName = firstNonEmpty(
		jstr(jget(raw, "lastName")),
	)
	p.Headline = firstNonEmpty(
		jstr(jget(raw, "headline")),
	)
	p.About = firstNonEmpty(
		jstr(jget(raw, "summary")),
	)
	p.URN = firstNonEmpty(
		jstr(jget(raw, "entityUrn")),
		jstr(jget(raw, "objectUrn")),
	)

	// Location: prefer "locationName", fall back to "geoCountryName".
	p.Location = firstNonEmpty(
		jstr(jget(raw, "locationName")),
		jstr(jget(raw, "geoCountryName")),
		jstr(jget(raw, "geoLocation", "geo", "defaultLocalizedName")),
	)

	// VanityName: may be returned as publicIdentifier.
	if id := jstr(jget(raw, "publicIdentifier")); id != "" {
		p.VanityName = id
	}
}

// parseProfileView extracts experience and education from the /profileView endpoint.
func parseProfileView(p *types.Profile, raw json.RawMessage) {
	if raw == nil {
		return
	}

	// Top-level profile fields (can override basic if richer).
	if profileNode := jget(raw, "profile"); profileNode != nil {
		parseBasicProfile(p, profileNode)
	}

	// Experience.
	for _, el := range jelems(jget(raw, "positionView")) {
		exp := types.Experience{
			Title:   jstr(jget(el, "title")),
			Company: firstNonEmpty(jstr(jget(el, "companyName")), jstr(jget(el, "company", "miniCompany", "name"))),
		}
		current := jbool(jget(el, "isCurrent"))

		tp := jget(el, "timePeriod")
		if tp != nil {
			exp.StartDate = formatYear(jget(tp, "startDate"))
			if current {
				exp.EndDate = "Present"
			} else {
				exp.EndDate = formatYear(jget(tp, "endDate"))
			}
		} else if current {
			exp.EndDate = "Present"
		}

		if exp.Title != "" || exp.Company != "" {
			p.Experience = append(p.Experience, exp)
		}
	}

	// Education.
	for _, el := range jelems(jget(raw, "educationView")) {
		edu := types.Education{
			School:       jstr(jget(el, "schoolName")),
			Degree:       jstr(jget(el, "degreeName")),
			FieldOfStudy: jstr(jget(el, "fieldOfStudy")),
		}
		tp := jget(el, "timePeriod")
		if tp != nil {
			edu.StartDate = formatYear(jget(tp, "startDate"))
			edu.EndDate = formatYear(jget(tp, "endDate"))
		}
		if edu.School != "" || edu.Degree != "" {
			p.Education = append(p.Education, edu)
		}
	}
}

// parseNetworkInfo extracts connection count from the /networkinfo response.
func parseNetworkInfo(p *types.Profile, raw json.RawMessage) {
	if raw == nil {
		return
	}
	n := jint(jget(raw, "connectionCount"))
	if n > 0 {
		p.Connections = formatConnections(n)
	}
}

// printProfile renders a human-readable profile summary to stdout.
func printProfile(cmd *cobra.Command, p *types.Profile) {
	w := cmd.OutOrStdout()

	// Header.
	name := p.FullName()
	if name == "" {
		name = p.VanityName
	}
	fmt.Fprintln(w, name)
	if p.Headline != "" {
		fmt.Fprintln(w, p.Headline)
	}
	if p.Location != "" {
		fmt.Fprintln(w, p.Location)
	}

	// About.
	if p.About != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "About:")
		for _, line := range wrapText(p.About, 72) {
			fmt.Fprintln(w, "  "+line)
		}
	}

	// Experience.
	if len(p.Experience) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Experience:")
		for _, e := range p.Experience {
			line := "  • "
			if e.Title != "" && e.Company != "" {
				line += e.Title + " at " + e.Company
			} else if e.Title != "" {
				line += e.Title
			} else {
				line += e.Company
			}
			if dates := e.DisplayDates(); dates != "" {
				line += " (" + dates + ")"
			}
			fmt.Fprintln(w, line)
		}
	}

	// Education.
	if len(p.Education) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Education:")
		for _, e := range p.Education {
			line := "  • "
			if deg := e.DisplayDegree(); deg != "" && e.School != "" {
				line += deg + ", " + e.School
			} else if e.School != "" {
				line += e.School
			} else {
				line += e.DisplayDegree()
			}
			if dates := e.DisplayDates(); dates != "" {
				line += " (" + dates + ")"
			}
			fmt.Fprintln(w, line)
		}
	}

	// Connections.
	if p.Connections != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, p.Connections)
	}
}

// wrapText breaks s into lines of at most maxLen runes.
func wrapText(s string, maxLen int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > maxLen {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// firstNonEmpty returns the first non-empty string from candidates.
func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}
