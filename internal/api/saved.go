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
//
// The saved-jobs query (JobCardsByJobCollections) returns the same Voyager v2
// element shape as job search — elements arrive as
// {jobCard: {jobPostingCard: {...}}} — so it reuses the well-tested job-search
// extraction pipeline (resolveJobCard) rather than duplicating traversal logic.
// The only differences are the collection key under data and marking each card
// as saved.
func parseSavedJobCards(raw json.RawMessage) ([]types.JobCard, error) {
	var resp voyagerResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("saved jobs: cannot decode response: %w", err)
	}

	entityMap := buildEntityMap(resp.Included)

	elements := findCollectionElements(resp.Data)
	if len(elements) == 0 {
		return []types.JobCard{}, nil
	}

	cards := make([]types.JobCard, 0, len(elements))
	for _, elem := range elements {
		card, err := resolveJobCard(elem, entityMap)
		if err != nil {
			// Skip unparseable cards rather than failing the whole batch.
			continue
		}
		card.Saved = true
		if card.ListingURL == "" && card.ID != "" {
			card.ListingURL = "https://www.linkedin.com/jobs/view/" + card.ID
		}
		cards = append(cards, card)
	}

	return cards, nil
}

// findCollectionElements locates the elements array in the saved-jobs data
// envelope. The saved query nests results under a collection key
// (jobsDashJobCardsByJobCollections) whose exact name is matched generically:
// any sub-object carrying a non-empty "elements" array is used.
func findCollectionElements(dataRaw json.RawMessage) []json.RawMessage {
	if len(dataRaw) == 0 {
		return nil
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(dataRaw, &top); err == nil {
		for _, v := range top {
			var sub struct {
				Elements []json.RawMessage `json:"elements"`
			}
			if err := json.Unmarshal(v, &sub); err == nil && len(sub.Elements) > 0 {
				return sub.Elements
			}
		}
	}

	// Some responses carry elements directly on data.
	var direct struct {
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(dataRaw, &direct); err == nil && len(direct.Elements) > 0 {
		return direct.Elements
	}

	return nil
}
