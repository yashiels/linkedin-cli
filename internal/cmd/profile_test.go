package cmd

import (
	"encoding/json"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func TestParseBasicProfile(t *testing.T) {
	raw := json.RawMessage(`{
		"firstName": "Yashiel",
		"lastName": "Sookdeo",
		"headline": "Software Engineer at Skyner",
		"summary": "Building things...",
		"locationName": "Cape Town Metropolitan Area",
		"publicIdentifier": "yashielsookdeo",
		"entityUrn": "urn:li:fs_profile:ACoAA"
	}`)

	p := &types.Profile{}
	parseBasicProfile(p, raw)

	if p.FirstName != "Yashiel" {
		t.Errorf("FirstName = %q; want %q", p.FirstName, "Yashiel")
	}
	if p.LastName != "Sookdeo" {
		t.Errorf("LastName = %q; want %q", p.LastName, "Sookdeo")
	}
	if p.Headline != "Software Engineer at Skyner" {
		t.Errorf("Headline = %q", p.Headline)
	}
	if p.About != "Building things..." {
		t.Errorf("About = %q", p.About)
	}
	if p.Location != "Cape Town Metropolitan Area" {
		t.Errorf("Location = %q", p.Location)
	}
	if p.VanityName != "yashielsookdeo" {
		t.Errorf("VanityName = %q", p.VanityName)
	}
}

func TestParseProfileFallbackLocation(t *testing.T) {
	raw := json.RawMessage(`{"firstName":"A","geoCountryName":"South Africa"}`)
	p := &types.Profile{}
	parseBasicProfile(p, raw)
	if p.Location != "South Africa" {
		t.Errorf("expected fallback location, got %q", p.Location)
	}
}

func TestParseProfileView(t *testing.T) {
	raw := json.RawMessage(`{
		"profile": {
			"firstName": "Test",
			"lastName": "User"
		},
		"positionView": {
			"elements": [
				{
					"title": "Software Engineer",
					"companyName": "Skyner",
					"isCurrent": true,
					"timePeriod": {
						"startDate": {"year": 2023, "month": 1}
					}
				},
				{
					"title": "Junior Dev",
					"companyName": "Acme",
					"isCurrent": false,
					"timePeriod": {
						"startDate": {"year": 2021},
						"endDate": {"year": 2023}
					}
				}
			]
		},
		"educationView": {
			"elements": [
				{
					"schoolName": "UCT",
					"degreeName": "BSc",
					"fieldOfStudy": "Computer Science",
					"timePeriod": {
						"startDate": {"year": 2018},
						"endDate": {"year": 2021}
					}
				}
			]
		}
	}`)

	p := &types.Profile{}
	parseProfileView(p, raw)

	if len(p.Experience) != 2 {
		t.Fatalf("expected 2 experience entries, got %d", len(p.Experience))
	}
	e0 := p.Experience[0]
	if e0.Title != "Software Engineer" {
		t.Errorf("exp[0].Title = %q", e0.Title)
	}
	if e0.Company != "Skyner" {
		t.Errorf("exp[0].Company = %q", e0.Company)
	}
	if e0.EndDate != "Present" {
		t.Errorf("exp[0].EndDate = %q; want Present", e0.EndDate)
	}
	if e0.StartDate != "2023" {
		t.Errorf("exp[0].StartDate = %q; want 2023", e0.StartDate)
	}

	e1 := p.Experience[1]
	if e1.EndDate != "2023" {
		t.Errorf("exp[1].EndDate = %q; want 2023", e1.EndDate)
	}

	if len(p.Education) != 1 {
		t.Fatalf("expected 1 education entry, got %d", len(p.Education))
	}
	edu := p.Education[0]
	if edu.School != "UCT" {
		t.Errorf("edu.School = %q", edu.School)
	}
	if edu.Degree != "BSc" {
		t.Errorf("edu.Degree = %q", edu.Degree)
	}
	if edu.StartDate != "2018" || edu.EndDate != "2021" {
		t.Errorf("edu dates: %q - %q", edu.StartDate, edu.EndDate)
	}
}

func TestParseNetworkInfo(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`{"connectionCount": 523}`, "500+ connections"},
		{`{"connectionCount": 42}`, "42 connections"},
		{`{"connectionCount": 0}`, ""},
		{`{}`, ""},
	}
	for _, tt := range tests {
		p := &types.Profile{}
		parseNetworkInfo(p, json.RawMessage(tt.raw))
		if p.Connections != tt.want {
			t.Errorf("parseNetworkInfo(%s) = %q; want %q", tt.raw, p.Connections, tt.want)
		}
	}
}

func TestFullName(t *testing.T) {
	p := &types.Profile{FirstName: "Yashiel", LastName: "Sookdeo"}
	if p.FullName() != "Yashiel Sookdeo" {
		t.Errorf("FullName = %q", p.FullName())
	}
	p2 := &types.Profile{FirstName: "Cher"}
	if p2.FullName() != "Cher" {
		t.Errorf("FullName single-name = %q", p2.FullName())
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("Building software at Skyner for fun and profit in Cape Town South Africa", 30)
	for _, l := range lines {
		if len(l) > 30 {
			t.Errorf("line exceeds 30 chars: %q", l)
		}
	}
}

func TestFormatConnections(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, ""},
		{1, "1 connections"},
		{499, "499 connections"},
		{500, "500+ connections"},
		{1000, "500+ connections"},
	}
	for _, tt := range tests {
		got := formatConnections(tt.in)
		if got != tt.want {
			t.Errorf("formatConnections(%d) = %q; want %q", tt.in, got, tt.want)
		}
	}
}
