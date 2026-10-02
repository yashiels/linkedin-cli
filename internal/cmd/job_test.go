package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func TestJobCommandHasCheckApplyFlag(t *testing.T) {
	value := false
	command := NewJobCmd(&value, &value, &value, &value, &value, &value, &value)
	flag := command.Flags().Lookup("check-apply")
	if flag == nil {
		t.Fatal("missing --check-apply flag")
	}
}

func TestJobDetailJSONPreservesFieldsAndIncludesUnverifiedApplication(t *testing.T) {
	detail := types.JobDetail{
		JobCard: types.JobCard{
			ID:         "1234567890",
			Title:      "Example role",
			ListingURL: "https://www.linkedin.com/jobs/view/1234567890",
		},
		Expired: false,
		Application: types.ApplicationAvailability{
			Status: types.ApplicationUnverified,
			Source: "linkedin-control",
			Reason: "LinkedIn returned no sufficient application-control evidence.",
		},
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]interface{}
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "title", "listingUrl", "expired", "application"} {
		if _, ok := value[field]; !ok {
			t.Fatalf("missing JSON field %q", field)
		}
	}
	application := value["application"].(map[string]interface{})
	if application["status"] != "unverified" {
		t.Fatalf("application status = %#v", application["status"])
	}
}

func TestPrintJobDetailHumanSeparatesListingAndApplyURLs(t *testing.T) {
	detail := &types.JobDetail{
		JobCard: types.JobCard{
			Title:      "Example role",
			ListingURL: "https://www.linkedin.com/jobs/view/1234567890",
		},
		Application: types.ApplicationAvailability{
			Status:   types.ApplicationUnverified,
			Reason:   "Employer form has not been verified.",
			ApplyURL: "https://jobs.lever.co/example/id/apply",
		},
	}
	var output bytes.Buffer
	printJobDetailHuman(&output, detail)
	text := output.String()
	for _, want := range []string{
		"Application: unverified",
		"Apply URL: https://jobs.lever.co/example/id/apply",
		"Listing URL: https://www.linkedin.com/jobs/view/1234567890",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q does not contain %q", text, want)
		}
	}
}
