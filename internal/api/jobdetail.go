package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

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

	return parseJobDetail(raw, jobID)
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

	detail := &types.JobDetail{}

	// Determine the bare numeric ID for URLs etc.
	bareID := originalID
	if u, err := types.ParseURN(originalID); err == nil {
		bareID = u.ID
	}
	detail.ID = bareID
	detail.URN = normaliseJobURN(bareID)
	detail.ListingURL = "https://www.linkedin.com/jobs/view/" + bareID

	// Navigate to the elements array.
	// Typical path: data → jobsDashJobPostingDetailSectionsByCardSectionTypesV2 → elements
	data := nav(envelope, "data")

	// LinkedIn wraps responses with the (camelCased) query name as the key.
	// We search for any key that contains "elements" or try known paths.
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

	// Process each section by type.
	for _, elem := range elements {
		sectionType := strPath(elem, "cardSectionType")
		switch sectionType {
		case "TOP_CARD_V2":
			parseTopCard(detail, elem)
		case "JOB_DESCRIPTION_CARD":
			parseDescriptionCard(detail, elem)
		}
	}

	return detail, nil
}

// parseTopCard extracts header information from a TOP_CARD_V2 section.
func parseTopCard(d *types.JobDetail, section interface{}) {
	// LinkedIn nests components under "topCard", "components", or directly.
	// We try multiple common paths because the schema varies across API versions.

	// Try topCard.jobPostingTitle first.
	if t := strPath(section, "topCard", "jobPostingTitle", "title"); t != "" {
		d.Title = t
	}
	// Alternate: components.headerComponent.title
	if d.Title == "" {
		if t := strPath(section, "components", "headerComponent", "title"); t != "" {
			d.Title = t
		}
	}
	// Fallback: any "title" key inside the section.
	if d.Title == "" {
		if t := strPath(section, "title"); t != "" {
			d.Title = t
		}
	}

	// Company name.
	if c := strPath(section, "topCard", "companyName"); c != "" {
		d.Company = c
	}
	if d.Company == "" {
		if c := strPath(section, "components", "headerComponent", "subtitle", "text"); c != "" {
			d.Company = c
		}
	}
	if d.Company == "" {
		d.Company = str(nav(section, "companyName"))
	}

	// Company URN.
	if cu := strPath(section, "topCard", "companyResolutionResult", "entityUrn"); cu != "" {
		d.CompanyURN = cu
	}

	// Location.
	if loc := strPath(section, "topCard", "formattedLocation"); loc != "" {
		d.Location = loc
	}
	if d.Location == "" {
		if loc := strPath(section, "components", "headerComponent", "caption", "text"); loc != "" {
			d.Location = loc
		}
	}
	if d.Location == "" {
		d.Location = str(nav(section, "formattedLocation"))
	}

	// Posted date — comes as "listedAt" (unix ms) or "formattedTimePeriod".
	if p := strPath(section, "topCard", "postingDateText"); p != "" {
		d.PostedAt = p
	}
	if d.PostedAt == "" {
		if p := strPath(section, "components", "insightsComponent", "insightViewModels"); p != "" {
			// It's sometimes in the insights component.
		}
		d.PostedAt = str(nav(section, "postingDateText"))
	}

	// Applicant count — typically a string like "45 applicants".
	if ac := strPath(section, "topCard", "applicantCountText"); ac != "" {
		d.ApplicantCount = ac
	}
	if d.ApplicantCount == "" {
		d.ApplicantCount = str(nav(section, "applicantCountText"))
	}

	// Easy Apply flag.
	if ea := nav(section, "topCard", "easyApplyEnabled"); ea != nil {
		d.EasyApply = boolVal(ea)
	}
	if !d.EasyApply {
		if ea := nav(section, "easyApplyEnabled"); ea != nil {
			d.EasyApply = boolVal(ea)
		}
	}
	// Also check jobState for apply method.
	if am := strPath(section, "topCard", "applyMethod", "$type"); strings.Contains(am, "EasyApply") {
		d.EasyApply = true
	}

	// Salary.
	parseSalary(d, section)

	// Seniority / employment type.
	if sl := strPath(section, "topCard", "jobInsight", "text"); sl != "" {
		// "Mid-Senior level · Full-time" format
		parts := strings.SplitN(sl, " · ", 2)
		if len(parts) == 2 {
			d.SeniorityLevel = strings.TrimSpace(parts[0])
			d.EmploymentType = strings.TrimSpace(parts[1])
		} else {
			d.SeniorityLevel = sl
		}
	}

	// Job expired / closed.
	if closed := nav(section, "topCard", "jobState", "closed"); closed != nil {
		d.Expired = boolVal(closed)
	}
}

// parseSalary extracts salary information from various possible locations.
func parseSalary(d *types.JobDetail, section interface{}) {
	// Compensation/salary can live in multiple places.
	paths := [][]string{
		{"topCard", "salaryInsight", "text"},
		{"topCard", "compensationBenefit", "compensationBenefitText"},
		{"topCard", "compensationInsight", "text"},
		{"salaryInsight", "text"},
	}
	for _, path := range paths {
		if s := strPath(section, path...); s != "" {
			d.Salary = s
			return
		}
	}

	// Numeric salary range.
	minV := nav(section, "topCard", "salaryInsight", "compensationBreakdown", "minSalary")
	maxV := nav(section, "topCard", "salaryInsight", "compensationBreakdown", "maxSalary")
	curr := strPath(section, "topCard", "salaryInsight", "compensationBreakdown", "currencyCode")
	if minV != nil || maxV != nil {
		if minF, ok := minV.(float64); ok {
			d.SalaryMin = int64(minF)
		}
		if maxF, ok := maxV.(float64); ok {
			d.SalaryMax = int64(maxF)
		}
		if curr != "" {
			d.SalaryCurr = curr
		}
		if d.SalaryMin > 0 && d.SalaryMax > 0 {
			d.Salary = fmt.Sprintf("%s %d–%d/yr", d.SalaryCurr, d.SalaryMin, d.SalaryMax)
		}
	}
}

// parseDescriptionCard extracts the job description from a JOB_DESCRIPTION_CARD section.
func parseDescriptionCard(d *types.JobDetail, section interface{}) {
	// Try several possible paths for the description HTML/text.
	paths := [][]string{
		{"jobDescriptionCard", "descriptionSnippet", "text"},
		{"jobDescriptionCard", "description", "text"},
		{"components", "descriptionComponent", "text", "text"},
		{"description", "text"},
		{"descriptionSnippet", "text"},
	}
	for _, path := range paths {
		if s := strPath(section, path...); s != "" {
			d.Description = s
			return
		}
	}

	// Sometimes description is nested differently.
	if jdc := nav(section, "jobDescriptionCard"); jdc != nil {
		if t := strPath(jdc, "description", "text"); t != "" {
			d.Description = t
			return
		}
	}
}
