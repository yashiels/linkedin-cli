// Package api provides LinkedIn Voyager API query helpers for job search and feed.
package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	// Job search
	queryJobSearch   = "JobCardsByJobSearch"
	queryIDJobSearch = "voyagerJobsDashJobCards.c7c69fb8e8f054fed088918d714be58a"

	// Job feed (deep link)
	queryJobFeed   = "JobCardsByJobSearchDeepLink"
	queryIDJobFeed = "voyagerJobsDashJobCards.d94f151c1c2d32ad5dbdedced6e4bba7"

	feedDeepLinkURL = "https://www.linkedin.com/jobs/search/?origin=JOBS_HOME_JYMBII"
)

// JobSearchParams holds all optional filters for a job search query.
type JobSearchParams struct {
	Keywords    string
	GeoURN      string   // e.g. "urn:li:fsd_geo:104035573"
	JobTypes    []string // F, P, C, T, I, V, O
	Experience  []string // 1-6
	Sort        string   // "R" (recent) or "DD" (default/relevant)
	PostedRange string   // "r86400" (24h), "r604800" (week), "r2592000" (month)
	EasyApply   bool
	Remote      bool
	Count       int
	Start       int
}

// SearchJobs calls the LinkedIn Voyager JobCardsByJobSearch GraphQL query.
func (c *Client) SearchJobs(params JobSearchParams) ([]types.JobCard, int, error) {
	vars := buildSearchVars(params)
	raw, err := c.QueryGraphQL(queryJobSearch, queryIDJobSearch, vars)
	if err != nil {
		return nil, 0, fmt.Errorf("jobs: search query failed: %w", err)
	}
	return parseJobCards(raw)
}

// FetchFeed calls the LinkedIn Voyager JobCardsByJobSearchDeepLink query.
func (c *Client) FetchFeed(count, start int) ([]types.JobCard, int, error) {
	vars := map[string]interface{}{
		"deepLinkUrl":     feedDeepLinkURL,
		"includeJobState": true,
		"count":           count,
		"start":           start,
	}
	raw, err := c.QueryGraphQL(queryJobFeed, queryIDJobFeed, vars)
	if err != nil {
		return nil, 0, fmt.Errorf("jobs: feed query failed: %w", err)
	}
	return parseJobCards(raw)
}

// buildSearchVars constructs the RestLi-encodable variable map for a job search.
//
// Discovery: the LinkedIn Voyager web API (voyagerJobsDashJobCards) validates
// `selectedFilters` as an input object type with NO exposed fields — any named
// field inside selectedFilters is rejected with a ValidationError. The correct
// encoding is to pass filters as TOP-LEVEL variables alongside count/start/query.
//
// Confirmed working top-level filter variable names (from live API testing):
//
//	applyWithLinkedin — Easy Apply; values: List("true")
//	experience        — Experience level codes; e.g. List("1","2")
//	jobType           — Job type codes; e.g. List("F","P")
//	timePostedRange   — Time range; e.g. List("r86400") = 24h
//	workplaceType     — Workplace; "1"=onsite, "2"=remote, "3"=hybrid
//	sortBy            — Sort order; List("R")=recent, List("DD")=relevant
func buildSearchVars(p JobSearchParams) map[string]interface{} {
	// Build the query inner object (no selectedFilters — filters go top-level).
	queryInner := map[string]interface{}{
		"keywords":               p.Keywords,
		"origin":                 "JOB_SEARCH_PAGE_SEARCH_BUTTON",
		"spellCorrectionEnabled": true,
	}
	if p.GeoURN != "" {
		queryInner["locationUnion"] = map[string]interface{}{
			"geoUrn": p.GeoURN,
		}
	}

	count := p.Count
	if count <= 0 {
		count = 10
	}

	vars := map[string]interface{}{
		"query":           queryInner,
		"includeJobState": true,
		"count":           count,
		"start":           p.Start,
	}

	// Filters are top-level variables, NOT nested in selectedFilters.
	if p.EasyApply {
		vars["applyWithLinkedin"] = []string{"true"}
	}
	if len(p.JobTypes) > 0 {
		vars["jobType"] = p.JobTypes
	}
	if len(p.Experience) > 0 {
		vars["experience"] = p.Experience
	}
	if p.PostedRange != "" {
		vars["timePostedRange"] = []string{p.PostedRange}
	}
	// Remote jobs: LinkedIn workplace type "2" = REMOTE.
	if p.Remote {
		vars["workplaceType"] = []string{"2"}
	}
	// Sort: "R" = most recent, "DD" = most relevant (LinkedIn default).
	if p.Sort == "R" {
		vars["sortBy"] = []string{"R"}
	}

	return vars
}

// --- Response parsing ---

// voyagerResponse is the top-level response envelope from LinkedIn's Voyager API.
type voyagerResponse struct {
	Data     json.RawMessage   `json:"data"`
	Included []json.RawMessage `json:"included"`
}

// rawEntity is a generic entity from the included array.
type rawEntity struct {
	Type      string          `json:"$type"`
	EntityURN string          `json:"entityUrn"`
	Raw       json.RawMessage `json:"-"`
}

// jobPostingEntity maps LinkedIn job posting fields.
type jobPostingEntity struct {
	EntityURN         string          `json:"entityUrn"`
	Title             string          `json:"title"`
	FormattedLocation string          `json:"formattedLocation"`
	ListedAt          int64           `json:"listedAt"`
	EasyApplyURL      string          `json:"easyApplyUrl"`
	WorkplaceTypes    []string        `json:"workplaceTypes"`
	ApplyMethod       json.RawMessage `json:"applyMethod"`
	CompanyDetails    json.RawMessage `json:"companyDetails"`
	JobState          json.RawMessage `json:"jobState"`
	// jobPostingCard embedded fields
	PremiumApplicantInsight json.RawMessage `json:"premiumApplicantInsight"`
}

// companyDetailsEntity holds company info from included entities.
type companyDetailsEntity struct {
	Name      string `json:"name"`
	EntityURN string `json:"entityUrn"`
}

// parseJobCards parses the raw Voyager API response into JobCard slices.
func parseJobCards(raw json.RawMessage) ([]types.JobCard, int, error) {
	var resp voyagerResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, fmt.Errorf("jobs: cannot parse response envelope: %w", err)
	}

	// Build entity map from included[].
	entityMap := buildEntityMap(resp.Included)

	// Extract elements from data — try multiple response shapes.
	elements, total, err := extractElements(resp.Data)
	if err != nil {
		return nil, 0, err
	}

	cards := make([]types.JobCard, 0, len(elements))
	for _, elem := range elements {
		card, err := resolveJobCard(elem, entityMap)
		if err != nil {
			// Skip unparseable cards rather than failing the whole batch.
			continue
		}
		cards = append(cards, card)
	}

	return cards, total, nil
}

// buildEntityMap indexes all included entities by their URN.
func buildEntityMap(included []json.RawMessage) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(included))
	for _, raw := range included {
		var e struct {
			EntityURN string `json:"entityUrn"`
		}
		if err := json.Unmarshal(raw, &e); err == nil && e.EntityURN != "" {
			m[e.EntityURN] = raw
		}
	}
	return m
}

// extractElements navigates the data envelope to find job card elements.
// LinkedIn's Voyager API nests results differently depending on the query.
//
// NOTE: this function receives resp.Data (the value under "data"), not the
// full response envelope. All key lookups are therefore relative to that object.
func extractElements(dataRaw json.RawMessage) ([]json.RawMessage, int, error) {
	if dataRaw == nil {
		return nil, 0, fmt.Errorf("jobs: empty data in response")
	}

	// Primary shape — current Voyager GraphQL: data contains the result
	// collection keyed by query name ("jobsDashJobCardsByJobSearch" for search,
	// "jobsDashJobCardsByJobSearchDeepLink" for feed).
	var primary struct {
		Search *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobsDashJobCardsByJobSearch"`
		DeepLink *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobsDashJobCardsByJobSearchDeepLink"`
	}
	if err := json.Unmarshal(dataRaw, &primary); err == nil {
		if s := primary.Search; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
		if s := primary.DeepLink; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
	}

	// Fallback — older key names without the "jobsDash" prefix.
	var fallback struct {
		Search *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobCardsByJobSearch"`
		DeepLink *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobCardsByJobSearchDeepLink"`
		SearchData *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobCardsByJobSearchData"`
		DeepLinkData *struct {
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"jobCardsByJobSearchDeepLinkData"`
	}
	if err := json.Unmarshal(dataRaw, &fallback); err == nil {
		if s := fallback.Search; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
		if s := fallback.DeepLink; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
		if s := fallback.SearchData; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
		if s := fallback.DeepLinkData; s != nil && len(s.Elements) > 0 {
			return s.Elements, s.Paging.Total, nil
		}
	}

	// Last resort: top-level elements array (some older API versions).
	var shapeTopLevel struct {
		Paging struct {
			Total int `json:"total"`
		} `json:"paging"`
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(dataRaw, &shapeTopLevel); err == nil && len(shapeTopLevel.Elements) > 0 {
		return shapeTopLevel.Elements, shapeTopLevel.Paging.Total, nil
	}

	return nil, 0, nil // Return empty rather than error — caller handles gracefully.
}

// resolveJobCard converts a raw element (possibly a URN reference) to a JobCard.
func resolveJobCard(elem json.RawMessage, entityMap map[string]json.RawMessage) (types.JobCard, error) {
	// Elements can be:
	// (a) A direct job card object
	// (b) A map with "*jobPosting" / "*entityUrn" reference to an entity
	// (c) A wrapper with a "jobCard" sub-object
	// (d) A plain URN string

	var card types.JobCard

	// Parse into a generic map to inspect structure.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(elem, &generic); err != nil {
		// Try as plain URN string.
		var urnStr string
		if jsonErr := json.Unmarshal(elem, &urnStr); jsonErr == nil {
			return resolveURN(urnStr, entityMap)
		}
		return card, fmt.Errorf("jobs: cannot parse element: %w", err)
	}

	// Case: has "jobCard" wrapper.
	if jobCardRaw, ok := generic["jobCard"]; ok {
		return resolveJobCard(jobCardRaw, entityMap)
	}

	// Case: has "jobPostingCard" wrapper — Voyager GraphQL v2 shape.
	// Must be checked before the generic "jobPosting" case because jobPostingCard
	// objects contain a nested "jobPosting" sub-object that would be mishandled.
	if jpcRaw, ok := generic["jobPostingCard"]; ok {
		return extractJobCardFromPostingCard(jpcRaw, entityMap)
	}

	// Case: has "jobPosting" wrapper.
	if jpRaw, ok := generic["jobPosting"]; ok {
		return resolveJobCard(jpRaw, entityMap)
	}

	// Look for URN references (keys starting with "*").
	for key, val := range generic {
		if strings.HasPrefix(key, "*") {
			var urnStr string
			if err := json.Unmarshal(val, &urnStr); err == nil {
				if entityRaw, ok := entityMap[urnStr]; ok {
					return extractJobCardFromEntity(entityRaw, entityMap)
				}
			}
		}
	}

	// Try interpreting this element directly as a job posting entity.
	if _, hasTitle := generic["title"]; hasTitle {
		return extractJobCardFromEntity(elem, entityMap)
	}

	// Try URN in "entityUrn" field.
	if urnRaw, ok := generic["entityUrn"]; ok {
		var urnStr string
		if err := json.Unmarshal(urnRaw, &urnStr); err == nil {
			if entityRaw, ok := entityMap[urnStr]; ok {
				return extractJobCardFromEntity(entityRaw, entityMap)
			}
			return extractJobCardFromEntity(elem, entityMap)
		}
	}

	return card, fmt.Errorf("jobs: cannot resolve job card from element")
}

// resolveURN looks up an entity by URN and extracts a job card.
func resolveURN(urn string, entityMap map[string]json.RawMessage) (types.JobCard, error) {
	entityRaw, ok := entityMap[urn]
	if !ok {
		return types.JobCard{}, fmt.Errorf("jobs: entity %q not found in included", urn)
	}
	return extractJobCardFromEntity(entityRaw, entityMap)
}

// extractJobCardFromEntity converts a raw entity JSON into a JobCard.
func extractJobCardFromEntity(entityRaw json.RawMessage, entityMap map[string]json.RawMessage) (types.JobCard, error) {
	var entity jobPostingEntity
	if err := json.Unmarshal(entityRaw, &entity); err != nil {
		return types.JobCard{}, fmt.Errorf("jobs: cannot parse entity: %w", err)
	}

	// Extract job ID from entityUrn.
	jobID := ""
	jobURN := entity.EntityURN
	if jobURN != "" {
		urn, err := types.ParseURN(jobURN)
		if err == nil {
			jobID = urn.ID
		}
	}

	// Resolve company name from companyDetails.
	companyName := extractCompanyName(entity.CompanyDetails, entityMap)

	// Format posted time.
	postedAt := formatPostedTime(entity.ListedAt)

	// Detect easy apply.
	easyApply := entity.EasyApplyURL != ""
	if !easyApply {
		// Check applyMethod for easyApply indicator.
		if entity.ApplyMethod != nil {
			var applyMethod map[string]json.RawMessage
			if err := json.Unmarshal(entity.ApplyMethod, &applyMethod); err == nil {
				_, easyApply = applyMethod["easyApplyUrl"]
				if !easyApply {
					_, easyApply = applyMethod["com.linkedin.voyager.jobs.OffsiteApply"]
					easyApply = !easyApply // offsite = not easy apply
				}
			}
		}
	}

	// Check workplace type for remote.
	remote := false
	for _, wt := range entity.WorkplaceTypes {
		if strings.Contains(strings.ToLower(wt), "remote") ||
			wt == "urn:li:fs_workplaceType:2" ||
			wt == "REMOTE" {
			remote = true
			break
		}
	}

	return types.JobCard{
		URN:       jobURN,
		ID:        jobID,
		Title:     entity.Title,
		Company:   companyName,
		Location:  entity.FormattedLocation,
		PostedAt:  postedAt,
		EasyApply: easyApply,
		Remote:    remote,
	}, nil
}

// jobPostingCardShape is the Voyager GraphQL v2 job card format.
// Elements arrive as {jobCard: {jobPostingCard: <this>}}.
type jobPostingCardShape struct {
	JobPostingTitle    string `json:"jobPostingTitle"`
	PrimaryDescription struct {
		Text string `json:"text"`
	} `json:"primaryDescription"`
	SecondaryDescription struct {
		Text string `json:"text"`
	} `json:"secondaryDescription"`
	TertiaryDescription struct {
		Text string `json:"text"`
	} `json:"tertiaryDescription"`
	JobPosting struct {
		EntityURN string `json:"entityUrn"`
		Title     string `json:"title"`
	} `json:"jobPosting"`
	FooterItems []struct {
		Type   string `json:"type"`
		TimeAt int64  `json:"timeAt"`
		Text   *struct {
			Text string `json:"text"`
		} `json:"text"`
	} `json:"footerItems"`
	EntityURN string `json:"entityUrn"`
}

// extractJobCardFromPostingCard converts the Voyager GraphQL v2 jobPostingCard
// structure into a JobCard. Field sources:
//
//	title    ← jobPostingTitle (or jobPosting.title fallback)
//	company  ← primaryDescription.text
//	location ← secondaryDescription.text
//	id / urn ← jobPosting.entityUrn (numeric ID parsed from URN)
//	postedAt ← footerItems[type=LISTED_DATE].timeAt (unix ms)
//	easyApply← footerItems[type=EASY_APPLY_TEXT] presence
func extractJobCardFromPostingCard(raw json.RawMessage, _ map[string]json.RawMessage) (types.JobCard, error) {
	var jpc jobPostingCardShape
	if err := json.Unmarshal(raw, &jpc); err != nil {
		return types.JobCard{}, fmt.Errorf("jobs: cannot parse jobPostingCard: %w", err)
	}

	// Prefer jobPosting.entityUrn for the canonical job URN/ID. The card's own
	// entityUrn is a composite like "urn:li:fsd_jobPostingCard:(id,context)".
	jobURN := jpc.JobPosting.EntityURN
	if jobURN == "" {
		jobURN = jpc.EntityURN
	}
	jobID := ""
	if jobURN != "" {
		if urn, err := types.ParseURN(jobURN); err == nil {
			jobID = urn.ID
		}
	}

	// Title: prefer the dedicated jobPostingTitle field.
	title := jpc.JobPostingTitle
	if title == "" {
		title = jpc.JobPosting.Title
	}

	// Walk footerItems for listed date and easy-apply indicator.
	var listedAt int64
	easyApply := false
	for _, fi := range jpc.FooterItems {
		switch fi.Type {
		case "LISTED_DATE":
			listedAt = fi.TimeAt
		case "EASY_APPLY_TEXT":
			easyApply = true
		}
	}

	location := jpc.SecondaryDescription.Text
	remote := strings.Contains(strings.ToLower(location), "remote")

	return types.JobCard{
		URN:       jobURN,
		ID:        jobID,
		Title:     title,
		Company:   jpc.PrimaryDescription.Text,
		Location:  location,
		PostedAt:  formatPostedTime(listedAt),
		EasyApply: easyApply,
		Remote:    remote,
	}, nil
}

// extractCompanyName resolves the company name from the companyDetails JSON blob.
func extractCompanyName(companyDetails json.RawMessage, entityMap map[string]json.RawMessage) string {
	if companyDetails == nil {
		return ""
	}

	// Try multiple company details shapes.

	// Shape 1: {companyResolutionResult: {name: "..."}}
	var s1 struct {
		CompanyResolutionResult struct {
			Name string `json:"name"`
		} `json:"companyResolutionResult"`
	}
	if err := json.Unmarshal(companyDetails, &s1); err == nil && s1.CompanyResolutionResult.Name != "" {
		return s1.CompanyResolutionResult.Name
	}

	// Shape 2: {"*company": "urn:li:..."}
	var s2 map[string]json.RawMessage
	if err := json.Unmarshal(companyDetails, &s2); err == nil {
		for key, val := range s2 {
			if strings.Contains(key, "company") || strings.Contains(key, "Company") {
				var urnStr string
				if err := json.Unmarshal(val, &urnStr); err == nil && strings.HasPrefix(urnStr, "urn:") {
					if entityRaw, ok := entityMap[urnStr]; ok {
						var co companyDetailsEntity
						if err := json.Unmarshal(entityRaw, &co); err == nil && co.Name != "" {
							return co.Name
						}
					}
				}
			}
		}
	}

	// Shape 3: {name: "..."}
	var s3 struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(companyDetails, &s3); err == nil && s3.Name != "" {
		return s3.Name
	}

	// Shape 4: {company: {name: "..."}}
	var s4 struct {
		Company struct {
			Name string `json:"name"`
		} `json:"company"`
	}
	if err := json.Unmarshal(companyDetails, &s4); err == nil && s4.Company.Name != "" {
		return s4.Company.Name
	}

	return ""
}

// formatPostedTime converts a LinkedIn epoch-millisecond timestamp to a
// human-readable "Xd ago" / "Xh ago" / "Xm ago" string.
func formatPostedTime(epochMs int64) string {
	if epochMs <= 0 {
		return ""
	}
	t := time.Unix(epochMs/1000, 0)
	d := time.Since(t)
	switch {
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins <= 0 {
			mins = 1
		}
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2006")
	}
}

// ExtractJobID returns the numeric job ID from a LinkedIn job posting URN.
// Input: "urn:li:fsd_jobPosting:4418763611" → "4418763611"
func ExtractJobID(urn string) string {
	parsed, err := types.ParseURN(urn)
	if err != nil {
		return ""
	}
	return parsed.ID
}

// GeoURNFromID builds a geo URN from a numeric geo ID.
func GeoURNFromID(id int64) string {
	return "urn:li:fsd_geo:" + strconv.FormatInt(id, 10)
}
