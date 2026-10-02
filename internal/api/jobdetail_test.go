package api

import (
	"encoding/json"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func TestParseJobDetailApplicationAvailability(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		description string
		resolution  map[string]interface{}
		status      types.ApplicationStatus
		expired     bool
		applyURL    string
	}{
		{
			name:  "explicit closure wins over enabled control",
			state: "CLOSED",
			resolution: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
			},
			status:  types.ApplicationClosed,
			expired: true,
		},
		{
			name:    "suspended listing is closed",
			state:   "SUSPENDED",
			status:  types.ApplicationClosed,
			expired: true,
		},
		{
			name:   "missing state and control is unverified",
			status: types.ApplicationUnverified,
		},
		{
			name:        "description apply text is not evidence",
			state:       "LISTED",
			description: "Apply today using the instructions below.",
			status:      types.ApplicationUnverified,
		},
		{
			name:  "disabled control is not accepting",
			state: "LISTED",
			resolution: map[string]interface{}{
				"onsiteApply":  false,
				"applyCtaText": map[string]interface{}{"text": "Apply"},
			},
			status: types.ApplicationUnverified,
		},
		{
			name:  "listed LinkedIn Easy Apply metadata is accepting",
			state: "LISTED",
			resolution: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
			},
			status: types.ApplicationAccepting,
		},
		{
			name: "unknown state with method metadata is unverified",
			resolution: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
			},
			status: types.ApplicationUnverified,
		},
		{
			name:  "negative CTA is unverified",
			state: "LISTED",
			resolution: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Apply unavailable"},
			},
			status: types.ApplicationUnverified,
		},
		{
			name:  "observed employer URL remains unverified",
			state: "LISTED",
			resolution: map[string]interface{}{
				"applicantTrackingSystemName": "Lever",
				"onsiteApply":                 false,
				"applyCtaText":                map[string]interface{}{"text": "Apply"},
				"companyApplyUrl":             "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
			},
			status:   types.ApplicationUnverified,
			applyURL: "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail, err := parseJobDetail(jobDetailFixture(t, test.state, test.description, test.resolution), "1234567890")
			if err != nil {
				t.Fatal(err)
			}
			if detail.Application.Status != test.status {
				t.Fatalf("status = %q, want %q", detail.Application.Status, test.status)
			}
			if detail.Expired != test.expired {
				t.Fatalf("expired = %t, want %t", detail.Expired, test.expired)
			}
			if detail.Application.ApplyURL != test.applyURL {
				t.Fatalf("apply URL = %q, want %q", detail.Application.ApplyURL, test.applyURL)
			}
			if detail.Application.ApplyURL != "" && detail.Application.ApplyURL == detail.ListingURL {
				t.Fatal("apply URL must remain distinct from listing URL")
			}
		})
	}
}

func TestParseJobDetailClosureIsStickyAcrossTopCards(t *testing.T) {
	tests := []struct {
		name   string
		states []string
	}{
		{name: "closed then listed", states: []string{"CLOSED", "LISTED"}},
		{name: "listed then suspended", states: []string{"LISTED", "SUSPENDED"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sections := make([]interface{}, 0, len(test.states))
			for _, state := range test.states {
				sections = append(sections, map[string]interface{}{
					"topCardV2": map[string]interface{}{
						"jobPostingCard": map[string]interface{}{
							"jobPosting": map[string]interface{}{"jobState": state},
							"primaryActionV2": map[string]interface{}{
								"applyJobAction": map[string]interface{}{
									"applyJobActionResolutionResult": map[string]interface{}{
										"onsiteApply":  true,
										"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
									},
								},
							},
						},
					},
				})
			}
			raw, err := json.Marshal(map[string]interface{}{
				"data": map[string]interface{}{
					"jobsDashJobPostingDetailSectionsByCardSectionTypes": map[string]interface{}{
						"elements": []interface{}{map[string]interface{}{"jobPostingDetailSection": sections}},
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			detail, err := parseJobDetail(raw, "1234567890")
			if err != nil {
				t.Fatal(err)
			}
			if detail.Application.Status != types.ApplicationClosed || !detail.Expired || detail.EasyApply {
				t.Fatalf("detail = %#v", detail)
			}
		})
	}
}

func TestParseJobDetailContradictoryTopCardsAreUnverified(t *testing.T) {
	tests := []struct {
		name               string
		reversed           bool
		missingActiveState bool
		wantURL            string
	}{
		{name: "onsite then offsite", wantURL: "https://careers.example.com/jobs/current"},
		{name: "offsite then onsite", reversed: true, wantURL: "https://careers.example.com/jobs/current"},
		{name: "onsite then unknown state", missingActiveState: true},
		{name: "unknown state then onsite", reversed: true, missingActiveState: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			onsite := map[string]interface{}{
				"jobPosting": map[string]interface{}{"jobState": "LISTED"},
				"primaryActionV2": map[string]interface{}{
					"applyJobAction": map[string]interface{}{
						"applyJobActionResolutionResult": map[string]interface{}{
							"onsiteApply":  true,
							"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
						},
					},
				},
			}
			offsite := map[string]interface{}{
				"jobPosting": map[string]interface{}{"jobState": "LISTED"},
				"primaryActionV2": map[string]interface{}{
					"applyJobAction": map[string]interface{}{
						"applyJobActionResolutionResult": map[string]interface{}{
							"onsiteApply":                 false,
							"applyCtaText":                map[string]interface{}{"text": "Apply"},
							"applicantTrackingSystemName": "ExampleATS",
							"companyApplyUrl":             "https://careers.example.com/jobs/current",
						},
					},
				},
			}
			if test.missingActiveState {
				offsite = map[string]interface{}{
					"jobPosting": map[string]interface{}{},
					"primaryActionV2": map[string]interface{}{
						"applyJobAction": map[string]interface{}{
							"applyJobActionResolutionResult": map[string]interface{}{
								"onsiteApply":  true,
								"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
							},
						},
					},
				}
			}
			cards := []interface{}{onsite, offsite}
			if test.reversed {
				cards[0], cards[1] = cards[1], cards[0]
			}
			sections := make([]interface{}, 0, len(cards))
			for _, card := range cards {
				sections = append(sections, map[string]interface{}{"topCardV2": map[string]interface{}{"jobPostingCard": card}})
			}
			raw, err := json.Marshal(map[string]interface{}{
				"data": map[string]interface{}{
					"jobsDashJobPostingDetailSectionsByCardSectionTypes": map[string]interface{}{
						"elements": []interface{}{map[string]interface{}{"jobPostingDetailSection": sections}},
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			detail, err := parseJobDetail(raw, "1234567890")
			if err != nil {
				t.Fatal(err)
			}
			if detail.Application.Status != types.ApplicationUnverified || detail.EasyApply {
				t.Fatalf("detail = %#v", detail)
			}
			if detail.Application.ApplyURL != test.wantURL {
				t.Fatalf("apply URL = %q", detail.Application.ApplyURL)
			}
		})
	}
}

func TestParseJobDetailMissingOptionalControlDoesNotContradictPositiveEvidence(t *testing.T) {
	raw, err := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"jobsDashJobPostingDetailSectionsByCardSectionTypes": map[string]interface{}{
				"elements": []interface{}{map[string]interface{}{
					"jobPostingDetailSection": []interface{}{
						map[string]interface{}{"topCardV2": map[string]interface{}{"jobPostingCard": map[string]interface{}{
							"jobPosting": map[string]interface{}{"jobState": "LISTED"},
							"primaryActionV2": map[string]interface{}{"applyJobAction": map[string]interface{}{
								"applyJobActionResolutionResult": map[string]interface{}{
									"onsiteApply": true, "applyCtaText": map[string]interface{}{"text": "Easy Apply"},
								},
							}},
						}}},
						map[string]interface{}{"topCardV2": map[string]interface{}{"jobPostingCard": map[string]interface{}{
							"jobPosting": map[string]interface{}{"jobState": "LISTED"},
						}}},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := parseJobDetail(raw, "1234567890")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Application.Status != types.ApplicationAccepting || !detail.EasyApply {
		t.Fatalf("detail = %#v", detail)
	}
}

func TestParseEasyApplyCheckRequiresEnabledOnsiteControl(t *testing.T) {
	tests := []struct {
		name      string
		detail    map[string]interface{}
		available bool
	}{
		{
			name: "enabled onsite control",
			detail: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
			},
			available: true,
		},
		{
			name: "CTA without enabled control",
			detail: map[string]interface{}{
				"onsiteApply":  false,
				"applyCtaText": map[string]interface{}{"text": "Easy Apply"},
			},
		},
		{
			name: "negative CTA",
			detail: map[string]interface{}{
				"onsiteApply":  true,
				"applyCtaText": map[string]interface{}{"text": "Apply unavailable"},
			},
		},
		{
			name: "prefill data without enabled control",
			detail: map[string]interface{}{
				"emailAddress": "candidate@example.com",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]interface{}{
				"data": map[string]interface{}{
					"jobsDashOnsiteApplyApplicationByJobPosting": map[string]interface{}{
						"elements": []interface{}{map[string]interface{}{"jobSeekerApplicationDetail": test.detail}},
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			status, err := parseEasyApplyCheck(raw)
			if err != nil {
				t.Fatal(err)
			}
			if status.Available != test.available {
				t.Fatalf("available = %t, want %t", status.Available, test.available)
			}
		})
	}
}

func jobDetailFixture(t *testing.T, state, description string, resolution map[string]interface{}) json.RawMessage {
	t.Helper()
	jobPosting := map[string]interface{}{}
	if state != "" {
		jobPosting["jobState"] = state
	}
	card := map[string]interface{}{
		"jobPostingTitle": "Example role",
		"jobPosting":      jobPosting,
	}
	if resolution != nil {
		card["primaryActionV2"] = map[string]interface{}{
			"applyJobAction": map[string]interface{}{
				"applyJobActionResolutionResult": resolution,
			},
		}
	}
	sections := []interface{}{
		map[string]interface{}{"topCardV2": map[string]interface{}{"jobPostingCard": card}},
	}
	if description != "" {
		sections = append(sections, map[string]interface{}{
			"jobDescription": map[string]interface{}{
				"jobPosting": map[string]interface{}{
					"description": map[string]interface{}{"text": description},
				},
			},
		})
	}
	raw, err := json.Marshal(map[string]interface{}{
		"data": map[string]interface{}{
			"jobsDashJobPostingDetailSectionsByCardSectionTypes": map[string]interface{}{
				"elements": []interface{}{map[string]interface{}{"jobPostingDetailSection": sections}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
