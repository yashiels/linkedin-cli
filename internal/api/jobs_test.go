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

	filters, ok := q["selectedFilters"].(map[string]interface{})
	if !ok {
		t.Fatalf("selectedFilters should be a map")
	}
	rt, ok := filters["resultType"].([]string)
	if !ok || len(rt) != 1 || rt[0] != "JOBS" {
		t.Errorf("expected resultType=[JOBS], got %v", filters["resultType"])
	}

	// No location should mean no locationUnion.
	if _, hasLoc := q["locationUnion"]; hasLoc {
		t.Error("expected no locationUnion when GeoURN is empty")
	}
}

func TestBuildSearchVars_WithAllFilters(t *testing.T) {
	p := JobSearchParams{
		Keywords:    "backend developer",
		GeoURN:      "urn:li:fsd_geo:105013608",
		JobTypes:    []string{"F", "C"},
		Experience:  []string{"2", "3"},
		Sort:        "R",
		PostedRange: "r86400",
		EasyApply:   true,
		Count:       25,
		Start:       10,
	}
	vars := buildSearchVars(p)

	q, _ := vars["query"].(map[string]interface{})
	filters, _ := q["selectedFilters"].(map[string]interface{})

	if _, hasEasy := filters["applyWithLinkedin"]; !hasEasy {
		t.Error("expected applyWithLinkedin filter when EasyApply is true")
	}
	if _, hasType := filters["jobType"]; !hasType {
		t.Error("expected jobType filter")
	}
	if _, hasExp := filters["experience"]; !hasExp {
		t.Error("expected experience filter")
	}
	if _, hasPosted := filters["timePostedRange"]; !hasPosted {
		t.Error("expected timePostedRange filter")
	}
	if vars["count"] != 25 {
		t.Errorf("expected count=25, got %v", vars["count"])
	}
	if vars["start"] != 10 {
		t.Errorf("expected start=10, got %v", vars["start"])
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
	raw := json.RawMessage(`{
		"data": {
			"jobCardsByJobSearch": {
				"paging": {"count": 10, "start": 0, "total": 42},
				"elements": [{"title": "Engineer"}, {"title": "Developer"}]
			}
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
	raw := json.RawMessage(`{
		"data": {
			"data": {
				"jobCardsByJobSearch": {
					"paging": {"total": 1},
					"elements": [
						{"*jobPosting": "urn:li:fsd_jobPosting:9999"}
					]
				}
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
