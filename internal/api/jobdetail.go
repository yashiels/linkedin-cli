package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	jobDetailQueryName = "JobPostingDetailSectionsByCardSectionTypesV2"
	jobDetailQueryID   = "voyagerJobsDashJobPostingDetailSections.8195171dc4c610f8c1551eaef6546bd8"
)

// GetJobDetail fetches the full detail of a LinkedIn job posting by ID.
// jobID may be a bare numeric ID (e.g. "4418763611") or a full URN
// ("urn:li:fsd_jobPosting:4418763611") — both forms are handled.
func (c *Client) GetJobDetail(jobID string) (*types.JobDetail, error) {
	// Normalise job ID → full URN.
	urn := normaliseJobURN(jobID)

	vars := map[string]interface{}{
		"cardSectionTypes": []string{"TOP_CARD_V2", "JOB_DESCRIPTION_CARD"},
		"jobPostingUrn":    urn,
		"includeJobState":  true,
		"trackingId":       newTrackingID(),
	}

	raw, err := c.QueryGraphQL(jobDetailQueryName, jobDetailQueryID, vars)
	if err != nil {
		return nil, fmt.Errorf("job detail: %w", err)
	}

	detail, err := parseJobDetail(raw, jobID)
	if err != nil {
		return nil, err
	}
	detail.Application.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	return detail, nil
}

// normaliseJobURN converts a bare numeric ID to a LinkedIn job URN.
// If urn already starts with "urn:" it is returned unchanged.
func normaliseJobURN(id string) string {
	if strings.HasPrefix(id, "urn:") {
		return id
	}
	return "urn:li:fsd_jobPosting:" + id
}

// newTrackingID generates a random base64 tracking token similar to
// what the LinkedIn Android app sends on each request.
func newTrackingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: static value is fine for tracking purposes.
		return "AAAAAAAAAAAAAAAAAAAAAA=="
	}
	return base64.StdEncoding.EncodeToString(b)
}

// ─── JSON helpers ────────────────────────────────────────────────────────────

// nav navigates a chain of keys through an arbitrary JSON-decoded object tree
// (map[string]interface{} / []interface{}). Returns nil if any step fails.
func nav(v interface{}, keys ...string) interface{} {
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

// str extracts a string from a nav result.
func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// strPath navigates to a key path and returns a string.
func strPath(v interface{}, keys ...string) string {
	return str(nav(v, keys...))
}

// arr extracts a []interface{} from a nav result.
func arr(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return nil
}

// bool extracts a bool from a nav result.
func boolVal(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// ─── Response parsing ─────────────────────────────────────────────────────────

func parseJobDetail(raw json.RawMessage, originalID string) (*types.JobDetail, error) {
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("job detail: cannot decode response: %w", err)
	}

	detail := &types.JobDetail{
		Application: types.ApplicationAvailability{
			Status: types.ApplicationUnverified,
			Source: "linkedin-control",
			Reason: "LinkedIn returned no sufficient application-control evidence.",
		},
	}
	observations := applicationObservations{routes: map[string]*applicationRouteObservation{}}

	// Determine the bare numeric ID for URLs etc.
	bareID := originalID
	if u, err := types.ParseURN(originalID); err == nil {
		bareID = u.ID
	}
	detail.ID = bareID
	detail.URN = normaliseJobURN(bareID)
	detail.ListingURL = "https://www.linkedin.com/jobs/view/" + bareID

	// Navigate to the elements array.
	// Typical path: data → jobsDashJobPostingDetailSectionsByCardSectionTypes → elements
	data := nav(envelope, "data")
	var elements []interface{}
	if dm, ok := data.(map[string]interface{}); ok {
		for _, v := range dm {
			if sub, ok := v.(map[string]interface{}); ok {
				if elems, ok := sub["elements"]; ok {
					if a := arr(elems); len(a) > 0 {
						elements = a
						break
					}
				}
			}
		}
	}

	if len(elements) == 0 {
		return nil, fmt.Errorf("job detail: no section elements found in response (job may not exist or be expired)")
	}

	// Current Voyager GraphQL response: each top-level element wraps a
	// "jobPostingDetailSection" array. Sections identify themselves by which
	// sub-key is non-null (topCardV2, jobDescription, etc.) rather than via a
	// "cardSectionType" discriminant.
	for _, elem := range elements {
		sections := arr(nav(elem, "jobPostingDetailSection"))
		for _, section := range sections {
			if topCard := nav(section, "topCardV2"); topCard != nil {
				parseTopCard(detail, topCard, &observations)
			}
			if jobDesc := nav(section, "jobDescription"); jobDesc != nil {
				parseDescriptionCard(detail, jobDesc)
			}
		}
		// Legacy / fallback: sections without the jobPostingDetailSection wrapper.
		if topCard := nav(elem, "topCardV2"); topCard != nil {
			parseTopCard(detail, topCard, &observations)
		}
		if jobDesc := nav(elem, "jobDescription"); jobDesc != nil {
			parseDescriptionCard(detail, jobDesc)
		}
	}
	applyApplicationObservations(detail, observations)

	return detail, nil
}

type applicationObservations struct {
	positive      bool
	contradictory bool
	listed        bool
	closedState   string
	routes        map[string]*applicationRouteObservation
}

type applicationRouteObservation struct {
	ats map[string]string
}

// parseTopCard extracts header information from a topCardV2 section object.
// The argument is the topCardV2 value — not the outer section wrapper.
func parseTopCard(d *types.JobDetail, topCard interface{}, observations *applicationObservations) {
	// Current Voyager structure: topCardV2.jobPostingCard contains most fields.
	jpc := nav(topCard, "jobPostingCard")
	if jpc == nil {
		jpc = topCard // fall back: treat topCard itself as the card
	}

	// Title: jobPostingCard.jobPostingTitle (preferred) or jobPosting.title.
	if t := strPath(jpc, "jobPostingTitle"); t != "" {
		d.Title = t
	}
	if d.Title == "" {
		d.Title = strPath(jpc, "jobPosting", "title")
	}

	// Company: jobPostingCard.primaryDescription.text
	if c := strPath(jpc, "primaryDescription", "text"); c != "" {
		d.Company = c
	}
	if d.Company == "" {
		d.Company = strPath(jpc, "jobPosting", "companyDetails", "jobCompany", "company", "name")
	}
	if d.Company == "" {
		d.Company = strPath(jpc, "navigationBarSubtitle")
		// navigationBarSubtitle is "Company · Location" — take only the company part.
		if idx := strings.Index(d.Company, " · "); idx >= 0 {
			d.Company = strings.TrimSpace(d.Company[:idx])
		}
	}

	// Company URN.
	if cu := strPath(jpc, "jobPosting", "companyDetails", "jobCompany", "company", "entityUrn"); cu != "" {
		d.CompanyURN = cu
	}

	// Location, PostedAt, ApplicantCount from tertiaryDescription.text.
	// The text format is: "Location · Time ago · Applicant count<extra text>"
	if tertiary := strPath(jpc, "tertiaryDescription", "text"); tertiary != "" {
		parts := strings.SplitN(tertiary, " · ", 3)
		if len(parts) >= 1 && d.Location == "" {
			d.Location = strings.TrimSpace(parts[0])
		}
		if len(parts) >= 2 && d.PostedAt == "" {
			d.PostedAt = strings.TrimSpace(parts[1])
		}
		if len(parts) >= 3 && d.ApplicantCount == "" {
			part := parts[2]
			// LinkedIn concatenates "Over 100 applicants" with trailing text — cut at "applicant".
			if idx := strings.Index(part, "applicants"); idx >= 0 {
				d.ApplicantCount = strings.TrimSpace(part[:idx+len("applicants")])
			} else if idx := strings.Index(part, "applicant"); idx >= 0 {
				d.ApplicantCount = strings.TrimSpace(part[:idx+len("applicant")])
			}
		}
	}

	observeApplicationAvailability(observations, jpc)

	// Salary, employment type, seniority level from job insights.
	parseSalaryFromInsights(d, jpc)
}

func observeApplicationAvailability(observations *applicationObservations, jpc interface{}) {
	jobState := strings.ToUpper(str(nav(jpc, "jobPosting", "jobState")))
	if jobState == "LISTED" {
		observations.listed = true
	}
	if jobState == "CLOSED" || jobState == "SUSPENDED" {
		if observations.closedState == "" || jobState == "CLOSED" {
			observations.closedState = jobState
		}
	}
	resolution := nav(jpc, "primaryActionV2", "applyJobAction", "applyJobActionResolutionResult")
	if resolution == nil {
		return
	}
	ats := strPath(resolution, "applicantTrackingSystemName")
	applyURL := strPath(resolution, "companyApplyUrl")
	observeApplicationRoute(observations, applyURL, ats)
	cta := strings.Join(strings.Fields(strings.ToLower(strPath(resolution, "applyCtaText", "text"))), " ")
	onsiteApply, onsitePresent := nav(resolution, "onsiteApply").(bool)
	positive := jobState == "LISTED" && onsitePresent && onsiteApply && cta == "easy apply"
	if positive {
		observations.positive = true
		return
	}
	if onsitePresent || cta != "" || applyURL != "" || ats != "" {
		observations.contradictory = true
	}
}

func observeApplicationRoute(observations *applicationObservations, applyURL, ats string) {
	if applyURL == "" {
		return
	}
	route := observations.routes[applyURL]
	if route == nil {
		route = &applicationRouteObservation{ats: map[string]string{}}
		observations.routes[applyURL] = route
	}
	canonical := canonicalApplicantTrackingSystem(ats)
	if canonical == "" {
		return
	}
	key := strings.ToLower(canonical)
	if existing := route.ats[key]; existing == "" || canonical < existing {
		route.ats[key] = canonical
	}
}

func canonicalApplicantTrackingSystem(ats string) string {
	trimmed := strings.TrimSpace(ats)
	switch strings.ToLower(trimmed) {
	case "lever":
		return "Lever"
	case "workday":
		return "Workday"
	default:
		return trimmed
	}
}

func selectApplicationRoute(observations applicationObservations) (string, string, bool, bool) {
	applyURL := ""
	for observedURL := range observations.routes {
		if applyURL == "" || observedURL < applyURL {
			applyURL = observedURL
		}
	}
	if applyURL == "" {
		return "", "", false, false
	}
	route := observations.routes[applyURL]
	multipleURLs := len(observations.routes) > 1
	if multipleURLs || len(route.ats) > 1 {
		return applyURL, "", len(route.ats) > 1, multipleURLs
	}
	for _, ats := range route.ats {
		return applyURL, ats, false, false
	}
	return applyURL, "", false, false
}

func applyApplicationObservations(d *types.JobDetail, observations applicationObservations) {
	applyURL, ats, conflictingATS, multipleURLs := selectApplicationRoute(observations)
	d.Application.ApplyURL = applyURL
	d.Application.ApplicantTrackingSystem = ats
	d.Application.Source = "linkedin-control"
	if observations.closedState != "" {
		d.Expired = true
		d.EasyApply = false
		d.Application.Status = types.ApplicationClosed
		d.Application.Source = "linkedin-job-state"
		d.Application.Evidence = "LinkedIn returned jobState=" + observations.closedState + "."
		d.Application.Reason = "LinkedIn reports that this listing is not accepting applications."
		return
	}
	if observations.positive && !observations.contradictory {
		d.EasyApply = true
		d.Application.Status = types.ApplicationAccepting
		d.Application.Evidence = "LinkedIn returned jobState=LISTED, onsiteApply=true, and the Easy Apply CTA."
		d.Application.Reason = "LinkedIn returned consistent current Easy Apply metadata."
		return
	}
	d.EasyApply = false
	d.Application.Status = types.ApplicationUnverified
	if conflictingATS {
		d.Application.Evidence = "LinkedIn returned conflicting applicant tracking system metadata for the same employer application URL."
		d.Application.Reason = "The employer application provider could not be selected safely."
		return
	}
	if multipleURLs {
		d.Application.Evidence = "LinkedIn returned multiple employer application URLs."
		d.Application.Reason = "A deterministic observed URL was retained without selecting an application provider."
		return
	}
	if observations.positive && observations.contradictory {
		d.Application.Evidence = "LinkedIn returned contradictory application-control metadata."
		d.Application.Reason = "Application availability could not be verified consistently."
		return
	}
	if applyURL != "" {
		d.Application.Evidence = "LinkedIn returned an employer application URL."
		d.Application.Reason = "The employer application form has not been verified."
		return
	}
	if observations.contradictory {
		d.Application.Evidence = "LinkedIn returned application metadata without sufficient active-state and CTA proof."
		d.Application.Reason = "Easy Apply availability could not be verified."
		return
	}
	if observations.listed {
		d.Application.Evidence = "LinkedIn returned jobState=LISTED without sufficient application-control evidence."
	}
}

// parseSalaryFromInsights extracts salary, employment type, and seniority level
// from the jobInsightsV2ResolutionResults array in the jobPostingCard.
func parseSalaryFromInsights(d *types.JobDetail, jpc interface{}) {
	knownEmploymentTypes := map[string]bool{
		"Full-time": true, "Part-time": true, "Contract": true,
		"Internship": true, "Temporary": true, "Volunteer": true, "Other": true,
	}
	knownSeniorityLevels := map[string]bool{
		"Entry level": true, "Mid-Senior level": true, "Associate": true,
		"Director": true, "Executive": true, "Not Applicable": true,
	}

	insights := arr(nav(jpc, "jobInsightsV2ResolutionResults"))
	for _, insight := range insights {
		descriptions := arr(nav(insight, "jobInsightViewModel", "description"))
		for _, desc := range descriptions {
			text := strPath(desc, "text", "text")
			if text == "" {
				continue
			}
			switch {
			case knownEmploymentTypes[text]:
				d.EmploymentType = text
			case knownSeniorityLevels[text]:
				d.SeniorityLevel = text
			case d.Salary == "" &&
				(strings.Contains(text, "/yr") || strings.Contains(text, "/mo") ||
					strings.Contains(text, "/hr") || strings.Contains(text, "K/yr")):
				d.Salary = text
			}
		}
	}
}

// parseDescriptionCard extracts the job description from a jobDescription section object.
// The argument is the jobDescription value — not the outer section wrapper.
func parseDescriptionCard(d *types.JobDetail, jobDesc interface{}) {
	// Current Voyager structure: jobDescription.jobPosting.description.text
	if text := strPath(jobDesc, "jobPosting", "description", "text"); text != "" {
		d.Description = text
		return
	}
	// Posted date fallback: jobDescription.postedOnText.
	if d.PostedAt == "" {
		if posted := strPath(jobDesc, "postedOnText"); posted != "" {
			d.PostedAt = posted
		}
	}
	// Legacy fallback paths (older Voyager API versions).
	for _, path := range [][]string{
		{"jobDescriptionCard", "description", "text"},
		{"jobDescriptionCard", "descriptionSnippet", "text"},
		{"components", "descriptionComponent", "text", "text"},
		{"description", "text"},
		{"descriptionSnippet", "text"},
	} {
		if s := strPath(jobDesc, path...); s != "" {
			d.Description = s
			return
		}
	}
}
