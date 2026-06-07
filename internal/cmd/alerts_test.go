package cmd

import (
	"encoding/json"
	"testing"
)

func TestParseAlertsGraphQL(t *testing.T) {
	raw := json.RawMessage(`{
		"data": {
			"jobsDashJobAlertsByAll": {
				"elements": [
					{
						"entityUrn": "urn:li:jobAlert:111222333",
						"createdAt": 1700000000000,
						"alert": {
							"keywords": "software engineer",
							"location": "Cape Town",
							"frequency": "DAILY"
						}
					},
					{
						"entityUrn": "urn:li:jobAlert:444555666",
						"createdAt": 1710000000000,
						"alert": {
							"keywords": "product manager",
							"frequency": "WEEKLY"
						}
					}
				]
			}
		}
	}`)

	alerts, err := parseAlerts(raw)
	if err != nil {
		t.Fatalf("parseAlerts error: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}

	a0 := alerts[0]
	if a0.ID != "111222333" {
		t.Errorf("alerts[0].ID = %q; want 111222333", a0.ID)
	}
	if a0.Keywords != "software engineer" {
		t.Errorf("alerts[0].Keywords = %q", a0.Keywords)
	}
	if a0.Location != "Cape Town" {
		t.Errorf("alerts[0].Location = %q", a0.Location)
	}
	if a0.Frequency != "DAILY" {
		t.Errorf("alerts[0].Frequency = %q", a0.Frequency)
	}
	if a0.CreatedAt == "" {
		t.Errorf("alerts[0].CreatedAt should not be empty")
	}

	a1 := alerts[1]
	if a1.ID != "444555666" {
		t.Errorf("alerts[1].ID = %q; want 444555666", a1.ID)
	}
	if a1.Frequency != "WEEKLY" {
		t.Errorf("alerts[1].Frequency = %q", a1.Frequency)
	}
}

func TestParseAlertsEmpty(t *testing.T) {
	raw := json.RawMessage(`{"data": {"jobsDashJobAlertsByAll": {"elements": []}}}`)
	alerts, err := parseAlerts(raw)
	if err != nil {
		t.Fatalf("parseAlerts error: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestParseKeywordsArray(t *testing.T) {
	node := json.RawMessage(`{"keywords": ["go", "backend", "engineer"]}`)
	got := parseKeywords(node)
	if got != "go, backend, engineer" {
		t.Errorf("parseKeywords array = %q", got)
	}
}

func TestParseKeywordsString(t *testing.T) {
	node := json.RawMessage(`{"keywords": "software engineer"}`)
	got := parseKeywords(node)
	if got != "software engineer" {
		t.Errorf("parseKeywords string = %q", got)
	}
}

func TestParseAlertLocationUnion(t *testing.T) {
	node := json.RawMessage(`{"locationUnion": {"location": "Remote"}}`)
	got := parseAlertLocation(node)
	if got != "Remote" {
		t.Errorf("parseAlertLocation union.location = %q", got)
	}
}
