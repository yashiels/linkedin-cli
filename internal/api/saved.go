package api

import (
	"encoding/json"
	"fmt"

	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	savedJobsQueryName = "JobCardsByJobCollections"
	savedJobsQueryID   = "voyagerJobsDashJobCards.c7062defea421b65446793bbc6b1cca5"
)

// GetSavedJobs retrieves the user's saved jobs from LinkedIn.
// count controls how many results to return (default 25 if ≤ 0).
func (c *Client) GetSavedJobs(count int) ([]types.JobCard, error) {
	if count <= 0 {
		count = 25
	}

	// JobCardsByJobCollections requires:
	//   jobCollectionSlug (String!)  — the collection slug ("savedJobs")
	//   query (JobSearchQueryInput!) — minimal search context; origin must be a valid enum
	vars := map[string]interface{}{
		"count":             count,
		"start":             0,
		"jobCollectionSlug": "savedJobs",
		"includeJobState":   true,
		"query": map[string]interface{}{
			"origin":   "JOB_SEARCH_PAGE_SEARCH_BUTTON",
			"keywords": "",
		},
	}

	raw, err := c.QueryGraphQL(savedJobsQueryName, savedJobsQueryID, vars)
	if err != nil {
		return nil, fmt.Errorf("saved jobs: %w", err)
	}

	return parseSavedJobCards(raw)
}

// parseSavedJobCards extracts job cards from the saved jobs API response.
func parseSavedJobCards(raw json.RawMessage) ([]types.JobCard, error) {
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("saved jobs: cannot decode response: %w", err)
	}

	// Navigate to the elements array.
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
		return []types.JobCard{}, nil
	}

	cards := make([]types.JobCard, 0, len(elements))
	for _, elem := range elements {
		card := extractJobCard(elem, true)
		if card != nil {
			cards = append(cards, *card)
		}
	}

	return cards, nil
}

// extractJobCard extracts a JobCard from a raw job card element.
// isSaved marks the card as saved (for saved jobs list context).
func extractJobCard(elem interface{}, isSaved bool) *types.JobCard {
	if elem == nil {
		return nil
	}

	card := &types.JobCard{
		Saved: isSaved,
	}

	// Navigate through possible nesting layers.
	// LinkedIn wraps job cards: elem → jobCard → jobPostingCard → ...
	jc := nav(elem, "jobCard")
	if jc == nil {
		jc = nav(elem, "jobPostingCard")
	}
	if jc == nil {
		jc = elem
	}

	// URN.
	card.URN = strPath(jc, "entityUrn")
	if card.URN == "" {
		card.URN = strPath(elem, "entityUrn")
	}

	// Extract ID from URN.
	if u, err := types.ParseURN(card.URN); err == nil {
		card.ID = u.ID
	}

	// Job title.
	card.Title = strPath(jc, "jobPostingTitle", "title")
	if card.Title == "" {
		card.Title = strPath(jc, "title")
	}

	// Company name.
	card.Company = strPath(jc, "primaryDescription", "text")
	if card.Company == "" {
		card.Company = strPath(jc, "companyName")
	}

	// Company URN.
	card.CompanyURN = strPath(jc, "logo", "attributes", "0", "detailData", "nonEntityCompanyLogo", "companyUrn")
	if card.CompanyURN == "" {
		card.CompanyURN = strPath(jc, "companyUrn")
	}

	// Location.
	card.Location = strPath(jc, "secondaryDescription", "text")
	if card.Location == "" {
		card.Location = strPath(jc, "formattedLocation")
	}

	// Posted date.
	card.PostedAt = strPath(jc, "footerItems", "0", "timeAt")
	if card.PostedAt == "" {
		card.PostedAt = strPath(jc, "listedAt")
	}

	// Applicant count.
	card.ApplicantCount = strPath(jc, "applicantCountText")

	// Easy Apply.
	if ea := nav(jc, "easyApplyEnabled"); ea != nil {
		card.EasyApply = boolVal(ea)
	}
	if am := strPath(jc, "applyMethod", "$type"); am != "" {
		if am == "com.linkedin.jobs.shared.EasyApplyMethod" {
			card.EasyApply = true
		}
	}

	// Listing URL.
	if card.ID != "" {
		card.ListingURL = "https://www.linkedin.com/jobs/view/" + card.ID
	}

	// Skip empty cards.
	if card.URN == "" && card.Title == "" {
		return nil
	}

	return card
}
