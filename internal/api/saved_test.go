package api

import (
	"encoding/json"
	"testing"
)

// TestParseSavedJobCards_VoyagerV2 feeds a realistic saved-jobs response whose
// elements use the Voyager v2 jobCard→jobPostingCard shape and asserts that the
// title, company, clean job URN, numeric ID and listing URL all populate — and
// that the URL is built from the numeric job ID (jobs/view/<numeric>), not from
// the composite jobPostingCard URN.
func TestParseSavedJobCards_VoyagerV2(t *testing.T) {
	raw := json.RawMessage(`{
		"data": {
			"jobsDashJobCardsByJobCollections": {
				"paging": {"count": 1, "start": 0, "total": 3},
				"elements": [{
					"jobCard": {
						"jobPostingCard": {
							"jobPostingTitle": "Backend Engineer",
							"primaryDescription": {"text": "Acme Corp"},
							"secondaryDescription": {"text": "Cape Town, South Africa"},
							"jobPosting": {
								"entityUrn": "urn:li:fsd_jobPosting:4418763611",
								"title": "Backend Engineer"
							},
							"footerItems": [
								{"type": "LISTED_DATE", "timeAt": 1779000000000},
								{"type": "EASY_APPLY_TEXT", "text": {"text": "Easy Apply"}}
							],
							"entityUrn": "urn:li:fsd_jobPostingCard:(4418763611,JOB_COLLECTIONS)"
						}
					}
				}]
			}
		},
		"included": []
	}`)

	cards, err := parseSavedJobCards(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}

	card := cards[0]
	if card.Title != "Backend Engineer" {
		t.Errorf("expected title 'Backend Engineer', got %q", card.Title)
	}
	if card.Company != "Acme Corp" {
		t.Errorf("expected company 'Acme Corp', got %q", card.Company)
	}
	if card.Location != "Cape Town, South Africa" {
		t.Errorf("expected location, got %q", card.Location)
	}
	// URN must come from jobPosting.entityUrn (clean), not the composite card URN.
	if card.URN != "urn:li:fsd_jobPosting:4418763611" {
		t.Errorf("expected clean jobPosting URN, got %q", card.URN)
	}
	if card.ID != "4418763611" {
		t.Errorf("expected ID=4418763611, got %q", card.ID)
	}
	if card.ListingURL != "https://www.linkedin.com/jobs/view/4418763611" {
		t.Errorf("expected listing URL jobs/view/4418763611, got %q", card.ListingURL)
	}
	if card.PostedAt == "" {
		t.Error("expected PostedAt to be populated from footerItems LISTED_DATE timeAt")
	}
	if !card.EasyApply {
		t.Error("expected EasyApply=true (EASY_APPLY_TEXT footer item present)")
	}
	if !card.Saved {
		t.Error("expected Saved=true for a saved-jobs card")
	}
}

func TestParseSavedJobCards_Empty(t *testing.T) {
	raw := json.RawMessage(`{"data":{},"included":[]}`)
	cards, err := parseSavedJobCards(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
}
