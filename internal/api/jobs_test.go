package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBuildSearchVars_MinimalParams(t *testing.T) {
	p := JobSearchParams{
		Keywords: "software engineer",
		Count:    10,
		Start:    0,
	}
	vars := buildSearchVars(p)

	// Must have top-level keys.
	if vars["count"] != 10 {
		t.Errorf("expected count=10, got %v", vars["count"])
	}
	if vars["start"] != 0 {
		t.Errorf("expected start=0, got %v", vars["start"])
	}
	if vars["includeJobState"] != true {
		t.Errorf("expected includeJobState=true, got %v", vars["includeJobState"])
	}

	q, ok := vars["query"].(map[string]interface{})
	if !ok {
		t.Fatalf("query should be a map, got %T", vars["query"])
	}
	if q["keywords"] != "software engineer" {
		t.Errorf("expected keywords='software engineer', got %v", q["keywords"])
	}
	if q["spellCorrectionEnabled"] != true {
		t.Errorf("expected spellCorrectionEnabled=true")
	}

	// With no filters set, selectedFilters must be absent (not even an empty map).
	if _, hasFilters := q["selectedFilters"]; hasFilters {
		t.Error("expected no selectedFilters when no filter flags are set")
	}

	// No location should mean no locationUnion.
	if _, hasLoc := q["locationUnion"]; hasLoc {
		t.Error("expected no locationUnion when GeoURN is empty")
	}
}

func TestBuildSearchVars_WithAllFilters(t *testing.T) {
	// NOTE: Filters go as TOP-LEVEL variables, not inside query.selectedFilters.
	// The LinkedIn Voyager web API rejects all field names inside selectedFilters
	// for the voyagerJobsDashJobCards query. Confirmed via live API testing.
	p := JobSearchParams{
		Keywords:    "backend developer",
		GeoURN:      "urn:li:fsd_geo:105013608",
		JobTypes:    []string{"F", "C"},
		Experience:  []string{"2", "3"},
		Sort:        "R",
		PostedRange: "r86400",
		EasyApply:   true,
		Remote:      true,
		Count:       25,
		Start:       10,
	}
	vars := buildSearchVars(p)

	// Filters must be at the top level.
	if _, hasEasy := vars["applyWithLinkedin"]; !hasEasy {
		t.Error("expected applyWithLinkedin at top-level vars when EasyApply is true")
	}
	if _, hasType := vars["jobType"]; !hasType {
		t.Error("expected jobType at top-level vars")
	}
	if _, hasExp := vars["experience"]; !hasExp {
		t.Error("expected experience at top-level vars")
	}
	if _, hasPosted := vars["timePostedRange"]; !hasPosted {
		t.Error("expected timePostedRange at top-level vars")
	}
	if _, hasRemote := vars["workplaceType"]; !hasRemote {
		t.Error("expected workplaceType at top-level vars when Remote is true")
	}
	if _, hasSort := vars["sortBy"]; !hasSort {
		t.Error("expected sortBy at top-level vars when Sort is R")
	}
	if vars["count"] != 25 {
		t.Errorf("expected count=25, got %v", vars["count"])
	}
	if vars["start"] != 10 {
		t.Errorf("expected start=10, got %v", vars["start"])
	}

	q, ok := vars["query"].(map[string]interface{})
	if !ok {
		t.Fatal("expected query map")
	}
	// query must NOT have selectedFilters with any filter fields (they'd be rejected).
	if sf, hasSF := q["selectedFilters"]; hasSF {
		if sfm, ok := sf.(map[string]interface{}); ok && len(sfm) > 0 {
			t.Error("selectedFilters inside query should be empty or absent; LinkedIn rejects named fields")
		}
	}

	locUnion, ok := q["locationUnion"].(map[string]interface{})
	if !ok {
		t.Fatal("expected locationUnion map")
	}
	if locUnion["geoUrn"] != "urn:li:fsd_geo:105013608" {
		t.Errorf("expected geoUrn='urn:li:fsd_geo:105013608', got %v", locUnion["geoUrn"])
	}
}

func TestBuildSearchVars_DefaultCount(t *testing.T) {
	p := JobSearchParams{Keywords: "dev", Count: 0}
	vars := buildSearchVars(p)
	if vars["count"] != 10 {
		t.Errorf("expected default count=10, got %v", vars["count"])
	}
}

func TestExtractElements_EmptyData(t *testing.T) {
	elems, total, err := extractElements(json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(elems) != 0 {
		t.Errorf("expected 0 elements, got %d", len(elems))
	}
	if total != 0 {
		t.Errorf("expected total=0, got %d", total)
	}
}

func TestExtractElements_Shape1(t *testing.T) {
	// Primary shape: data value contains jobsDashJobCardsByJobSearch (current Voyager API).
	raw := json.RawMessage(`{
		"jobsDashJobCardsByJobSearch": {
			"paging": {"count": 10, "start": 0, "total": 42},
			"elements": [{"title": "Engineer"}, {"title": "Developer"}]
		}
	}`)
	elems, total, err := extractElements(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 42 {
		t.Errorf("expected total=42, got %d", total)
	}
	if len(elems) != 2 {
		t.Errorf("expected 2 elements, got %d", len(elems))
	}
}

func TestExtractElements_Shape1_DeepLink(t *testing.T) {
	// Feed variant uses jobsDashJobCardsByJobSearchDeepLink.
	raw := json.RawMessage(`{
		"jobsDashJobCardsByJobSearchDeepLink": {
			"paging": {"count": 5, "start": 0, "total": 99},
			"elements": [{"title": "PM"}]
		}
	}`)
	elems, total, err := extractElements(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 99 {
		t.Errorf("expected total=99, got %d", total)
	}
	if len(elems) != 1 {
		t.Errorf("expected 1 element, got %d", len(elems))
	}
}

func TestExtractElements_Shape1_OldKey(t *testing.T) {
	// Fallback: old key name without the "jobsDash" prefix.
	raw := json.RawMessage(`{
		"jobCardsByJobSearch": {
			"paging": {"count": 10, "start": 0, "total": 42},
			"elements": [{"title": "Engineer"}, {"title": "Developer"}]
		}
	}`)
	elems, total, err := extractElements(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 42 {
		t.Errorf("expected total=42, got %d", total)
	}
	if len(elems) != 2 {
		t.Errorf("expected 2 elements, got %d", len(elems))
	}
}

func TestExtractElements_Shape2(t *testing.T) {
	raw := json.RawMessage(`{
		"jobCardsByJobSearchData": {
			"paging": {"total": 100},
			"elements": [{"entityUrn": "urn:li:fsd_jobPosting:123"}]
		}
	}`)
	elems, total, err := extractElements(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 100 {
		t.Errorf("expected total=100, got %d", total)
	}
	if len(elems) != 1 {
		t.Errorf("expected 1 element, got %d", len(elems))
	}
}

func TestBuildEntityMap(t *testing.T) {
	included := []json.RawMessage{
		json.RawMessage(`{"entityUrn":"urn:li:fsd_jobPosting:111","title":"Eng"}`),
		json.RawMessage(`{"entityUrn":"urn:li:fsd_jobPosting:222","title":"Dev"}`),
		json.RawMessage(`{"title":"no-urn"}`), // should be skipped
	}
	m := buildEntityMap(included)
	if len(m) != 2 {
		t.Errorf("expected 2 entities, got %d", len(m))
	}
	if _, ok := m["urn:li:fsd_jobPosting:111"]; !ok {
		t.Error("expected entity urn:li:fsd_jobPosting:111")
	}
}

func TestExtractJobCardFromEntity_Basic(t *testing.T) {
	entityRaw := json.RawMessage(`{
		"entityUrn": "urn:li:fsd_jobPosting:4418763611",
		"title": "Senior Software Engineer",
		"formattedLocation": "Cape Town, South Africa",
		"listedAt": 1717123456789,
		"easyApplyUrl": "https://www.linkedin.com/easy-apply/..."
	}`)

	card, err := extractJobCardFromEntity(entityRaw, map[string]json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if card.ID != "4418763611" {
		t.Errorf("expected ID=4418763611, got %q", card.ID)
	}
	if card.Title != "Senior Software Engineer" {
		t.Errorf("expected title, got %q", card.Title)
	}
	if card.Location != "Cape Town, South Africa" {
		t.Errorf("expected location, got %q", card.Location)
	}
	if !card.EasyApply {
		t.Error("expected EasyApply=true when easyApplyUrl is set")
	}
}

func TestFormatPostedTime(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		epochMs  int64
		contains string
	}{
		{"zero", 0, ""},
		{"minutes ago", now.Add(-30 * time.Minute).UnixMilli(), "m ago"},
		{"hours ago", now.Add(-3 * time.Hour).UnixMilli(), "h ago"},
		{"days ago", now.Add(-5 * 24 * time.Hour).UnixMilli(), "d ago"},
		{"old", now.Add(-60 * 24 * time.Hour).UnixMilli(), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatPostedTime(tt.epochMs)
			if tt.epochMs == 0 {
				if result != "" {
					t.Errorf("expected empty string for zero epoch, got %q", result)
				}
				return
			}
			if tt.contains != "" && !containsStr(result, tt.contains) {
				t.Errorf("expected %q to contain %q", result, tt.contains)
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestGeoURNFromID(t *testing.T) {
	result := GeoURNFromID(104035573)
	expected := "urn:li:fsd_geo:104035573"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestParseJobCards_EmptyResponse(t *testing.T) {
	raw := json.RawMessage(`{"data":{},"included":[]}`)
	cards, total, err := parseJobCards(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
	if total != 0 {
		t.Errorf("expected total=0, got %d", total)
	}
}

func TestParseJobCards_WithIncluded(t *testing.T) {
	// Older response shape: elements contain URN references resolved via included[].
	raw := json.RawMessage(`{
		"data": {
			"jobsDashJobCardsByJobSearch": {
				"paging": {"total": 1},
				"elements": [
					{"*jobPosting": "urn:li:fsd_jobPosting:9999"}
				]
			}
		},
		"included": [
			{
				"entityUrn": "urn:li:fsd_jobPosting:9999",
				"title": "Go Developer",
				"formattedLocation": "Remote",
				"listedAt": 1717000000000
			}
		]
	}`)

	cards, total, err := parseJobCards(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].ID != "9999" {
		t.Errorf("expected ID=9999, got %q", cards[0].ID)
	}
	if cards[0].Title != "Go Developer" {
		t.Errorf("expected title 'Go Developer', got %q", cards[0].Title)
	}
}

func TestParseJobCards_VoyagerV2(t *testing.T) {
	// Current Voyager GraphQL v2 shape: elements contain jobCard.jobPostingCard objects.
	raw := json.RawMessage(`{
		"data": {
			"jobsDashJobCardsByJobSearch": {
				"paging": {"count": 1, "start": 0, "total": 507},
				"elements": [{
					"jobCard": {
						"jobPostingCard": {
							"jobPostingTitle": "Frontend Developer - Remote",
							"primaryDescription": {"text": "YO IT Consulting"},
							"secondaryDescription": {"text": "South Africa (Remote)"},
							"tertiaryDescription": {"text": "ZAR30K/yr - ZAR35K/yr"},
							"jobPosting": {
								"entityUrn": "urn:li:fsd_jobPosting:4414051567",
								"title": "Frontend Developer - Remote",
								"repostedJob": false
							},
							"footerItems": [
								{"type": "LISTED_DATE", "timeAt": 1779414259000},
								{"type": "EASY_APPLY_TEXT", "text": {"text": "Easy Apply"}}
							],
							"entityUrn": "urn:li:fsd_jobPostingCard:(4414051567,JOBS_SEARCH)"
						}
					}
				}]
			}
		},
		"included": []
	}`)

	cards, total, err := parseJobCards(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 507 {
		t.Errorf("expected total=507, got %d", total)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	card := cards[0]
	if card.ID != "4414051567" {
		t.Errorf("expected ID=4414051567, got %q", card.ID)
	}
	if card.Title != "Frontend Developer - Remote" {
		t.Errorf("expected title 'Frontend Developer - Remote', got %q", card.Title)
	}
	if card.Company != "YO IT Consulting" {
		t.Errorf("expected company 'YO IT Consulting', got %q", card.Company)
	}
	if card.Location != "South Africa (Remote)" {
		t.Errorf("expected location 'South Africa (Remote)', got %q", card.Location)
	}
	if !card.EasyApply {
		t.Error("expected EasyApply=true (EASY_APPLY_TEXT footer item present)")
	}
	if !card.Remote {
		t.Error("expected Remote=true (location contains 'Remote')")
	}
	if card.URN != "urn:li:fsd_jobPosting:4414051567" {
		t.Errorf("expected URN from jobPosting.entityUrn, got %q", card.URN)
	}
}

func TestExtractJobCardFromPostingCard(t *testing.T) {
	raw := json.RawMessage(`{
		"jobPostingTitle": "Staff Engineer",
		"primaryDescription": {"text": "BigCorp"},
		"secondaryDescription": {"text": "Cape Town, South Africa"},
		"jobPosting": {
			"entityUrn": "urn:li:fsd_jobPosting:123456"
		},
		"footerItems": [
			{"type": "LISTED_DATE", "timeAt": 1779000000000}
		],
		"entityUrn": "urn:li:fsd_jobPostingCard:(123456,JOBS_SEARCH)"
	}`)

	card, err := extractJobCardFromPostingCard(raw, map[string]json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if card.ID != "123456" {
		t.Errorf("expected ID=123456, got %q", card.ID)
	}
	if card.URN != "urn:li:fsd_jobPosting:123456" {
		t.Errorf("expected URN from jobPosting (not card), got %q", card.URN)
	}
	if card.Title != "Staff Engineer" {
		t.Errorf("expected title 'Staff Engineer', got %q", card.Title)
	}
	if card.Company != "BigCorp" {
		t.Errorf("expected company 'BigCorp', got %q", card.Company)
	}
	if card.Location != "Cape Town, South Africa" {
		t.Errorf("expected location, got %q", card.Location)
	}
	if card.EasyApply {
		t.Error("expected EasyApply=false (no EASY_APPLY_TEXT footer item)")
	}
	if card.Remote {
		t.Error("expected Remote=false (location does not contain 'remote')")
	}
}

func TestExtractCompanyName_ShapeResolutionResult(t *testing.T) {
	cd := json.RawMessage(`{"companyResolutionResult":{"name":"Acme Corp"}}`)
	name := extractCompanyName(cd, map[string]json.RawMessage{})
	if name != "Acme Corp" {
		t.Errorf("expected 'Acme Corp', got %q", name)
	}
}

func TestExtractCompanyName_ShapeDirect(t *testing.T) {
	cd := json.RawMessage(`{"name":"Globex"}`)
	name := extractCompanyName(cd, map[string]json.RawMessage{})
	if name != "Globex" {
		t.Errorf("expected 'Globex', got %q", name)
	}
}

func TestExtractCompanyName_ShapeNested(t *testing.T) {
	cd := json.RawMessage(`{"company":{"name":"Initech"}}`)
	name := extractCompanyName(cd, map[string]json.RawMessage{})
	if name != "Initech" {
		t.Errorf("expected 'Initech', got %q", name)
	}
}

func TestExtractCompanyName_URNResolution(t *testing.T) {
	cd := json.RawMessage(`{"*company":"urn:li:fsd_company:42"}`)
	entityMap := map[string]json.RawMessage{
		"urn:li:fsd_company:42": json.RawMessage(`{"name":"Umbrella Corp","entityUrn":"urn:li:fsd_company:42"}`),
	}
	name := extractCompanyName(cd, entityMap)
	if name != "Umbrella Corp" {
		t.Errorf("expected 'Umbrella Corp', got %q", name)
	}
}
