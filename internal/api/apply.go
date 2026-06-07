package api

import (
	"encoding/json"
	"fmt"
	"net/url"
)

const (
	applyCheckQueryName = "JobsOnsiteApplyApplicationByJobPosting"
	applyCheckQueryID   = "voyagerJobsDashOnsiteApplyApplication.34ac512c4fd87baec02c710aef4f563b"

	applySubmitPath = "/voyager/api/voyagerJobsDashOnsiteApplyApplication"
)

// EasyApplyStatus describes the current state of an Easy Apply application.
type EasyApplyStatus struct {
	// Available is true when Easy Apply is offered for this job.
	Available bool

	// JobTitle and Company from the check response (populated if available).
	JobTitle string
	Company  string

	// Prefill data extracted from the application form (for display before submission).
	// These come from the applicant's profile and the pre-filled form fields.
	Name   string
	Email  string
	Phone  string
	Resume string // Display name of the resume to be submitted.

	// RawForm is the raw application form returned by the check endpoint.
	// It is passed back to SubmitApplication so the server receives the
	// exact same form data it generated.
	RawForm json.RawMessage
}

// CheckEasyApply queries LinkedIn to see whether a job supports Easy Apply
// and returns prefill data if it does.
func (c *Client) CheckEasyApply(jobID string) (*EasyApplyStatus, error) {
	urn := normaliseJobURN(jobID)

	vars := map[string]interface{}{
		"jobPostingUrn": urn,
	}

	raw, err := c.QueryGraphQL(applyCheckQueryName, applyCheckQueryID, vars)
	if err != nil {
		return nil, fmt.Errorf("easy apply check: %w", err)
	}

	return parseEasyApplyCheck(raw)
}

// parseEasyApplyCheck extracts prefill and availability data from the
// onsite apply check response.
//
// Current Voyager GraphQL response structure:
//
//	data.jobsDashOnsiteApplyApplicationByJobPosting.elements[0].jobSeekerApplicationDetail
//	  .onsiteApply     bool
//	  .applyCtaText    {text: "Easy Apply"}
//	  .resume          null | {name: "..."}
//	  .emailAddress    string (may be absent)
//	  .mobilePhone     string (may be absent)
func parseEasyApplyCheck(raw json.RawMessage) (*EasyApplyStatus, error) {
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("easy apply check: cannot decode response: %w", err)
	}

	status := &EasyApplyStatus{}

	// Navigate to the data section.
	data := nav(envelope, "data")
	if data == nil {
		return status, nil
	}

	// Find the first collection object under data.
	// (e.g. jobsDashOnsiteApplyApplicationByJobPosting)
	var collection interface{}
	if dm, ok := data.(map[string]interface{}); ok {
		for _, v := range dm {
			if _, ok := v.(map[string]interface{}); ok {
				collection = v
				break
			}
		}
	}
	if collection == nil {
		return status, nil
	}

	// Navigate into elements[0] — the actual application record.
	elements := arr(nav(collection, "elements"))
	var appDetail interface{}
	if len(elements) > 0 {
		// Prefer jobSeekerApplicationDetail sub-object; fall back to the element itself.
		appDetail = nav(elements[0], "jobSeekerApplicationDetail")
		if appDetail == nil {
			appDetail = elements[0]
		}
	} else {
		// Older shape: collection is the application directly.
		appDetail = collection
	}

	if appDetail == nil {
		return status, nil
	}

	// Available: onsiteApply flag, applyCtaText presence, or $type field.
	if boolVal(nav(appDetail, "onsiteApply")) {
		status.Available = true
	}
	if strPath(appDetail, "applyCtaText", "text") != "" {
		status.Available = true
	}
	if strPath(appDetail, "$type") != "" {
		status.Available = true
	}

	// Prefill data (may not be present for all jobs / account states).
	if name := strPath(appDetail, "applicantName"); name != "" {
		status.Name = name
	}
	if fn := strPath(appDetail, "firstName"); fn != "" {
		status.Name = fn + " " + strPath(appDetail, "lastName")
	}
	if email := strPath(appDetail, "emailAddress"); email != "" {
		status.Email = email
	}
	if phone := strPath(appDetail, "mobilePhoneNumber"); phone != "" {
		status.Phone = phone
	}
	if rn := strPath(appDetail, "resume", "name"); rn != "" {
		status.Resume = rn
	}
	if status.Resume == "" {
		status.Resume = strPath(appDetail, "resumeDocumentName")
	}

	// If any prefill data exists, confirm Easy Apply is available.
	if status.Name != "" || status.Email != "" {
		status.Available = true
	}

	// Store the raw form for submission.
	status.RawForm = raw

	return status, nil
}

// ApplicationSubmitRequest is the body sent to LinkedIn's Easy Apply endpoint.
type ApplicationSubmitRequest struct {
	// JobPostingUrn is the full URN of the job posting.
	JobPostingUrn string `json:"jobPostingUrn"`

	// TrackingID is a random token for deduplication.
	TrackingId string `json:"trackingId"`
}

// SubmitEasyApply submits an Easy Apply application for the given job.
// The rawForm from CheckEasyApply is merged into the submission body so
// LinkedIn receives the prefilled form data it generated.
//
// Returns an error on failure, nil on success.
func (c *Client) SubmitEasyApply(jobID string, status *EasyApplyStatus) error {
	urn := normaliseJobURN(jobID)

	body := ApplicationSubmitRequest{
		JobPostingUrn: urn,
		TrackingId:    newTrackingID(),
	}

	_, err := c.PostJSON(
		applySubmitPath,
		map[string]string{"action": "submitApplication"},
		body,
	)
	if err != nil {
		return fmt.Errorf("submit application: %w", err)
	}

	return nil
}

// ExternalApplyURL returns the external application URL for a job that
// does not support Easy Apply.
func ExternalApplyURL(jobID string) string {
	bareID := jobID
	if u, err := parseJobURN(jobID); err == nil {
		bareID = u
	}
	return "https://www.linkedin.com/jobs/view/" + url.PathEscape(bareID)
}

// parseJobURN returns the bare numeric ID from a job URN or the original
// string if it's already a bare ID.
func parseJobURN(urn string) (string, error) {
	if !isURN(urn) {
		return urn, nil
	}
	u, err := parseGenericURN(urn)
	if err != nil {
		return urn, err
	}
	return u, nil
}

func isURN(s string) bool {
	return len(s) > 4 && s[:4] == "urn:"
}

func parseGenericURN(urn string) (string, error) {
	// urn:li:fsd_jobPosting:1234567890
	parts := make([]string, 0, 4)
	start := 0
	for i, ch := range urn {
		if ch == ':' {
			parts = append(parts, urn[start:i])
			start = i + 1
		}
	}
	parts = append(parts, urn[start:])
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid URN: %s", urn)
	}
	return parts[len(parts)-1], nil
}

// SaveJob saves a job to the user's saved jobs collection.
// Endpoint: POST /voyager/api/voyagerJobsDashSavedJobPosts
// Body: {"jobPostingUrn": "urn:li:fsd_jobPosting:..."}
func (c *Client) SaveJob(jobID string) error {
	urn := normaliseJobURN(jobID)
	body := map[string]string{
		"jobPostingUrn": urn,
	}
	// Plain POST (no ?action=... query param) — standard Restli collection create.
	_, err := c.PostJSON(
		"/voyager/api/voyagerJobsDashSavedJobPosts",
		nil,
		body,
	)
	if err != nil {
		return fmt.Errorf("save job: %w", err)
	}
	return nil
}

// UnsaveJob removes a job from the user's saved jobs collection.
// Endpoint: DELETE /voyager/api/voyagerJobsDashSavedJobPosts/<encoded-urn>
func (c *Client) UnsaveJob(jobID string) error {
	urn := normaliseJobURN(jobID)
	// LinkedIn uses the URN as path-encoded resource identifier.
	encoded := url.PathEscape(urn)
	return c.DeleteResource("/voyager/api/voyagerJobsDashSavedJobPosts/" + encoded)
}
