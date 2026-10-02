package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type applyRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn applyRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEnsureApplicationCanProceedRejectsKnownClosed(t *testing.T) {
	detail := &types.JobDetail{
		Application: types.ApplicationAvailability{Status: types.ApplicationClosed},
	}
	if err := ensureApplicationCanProceed(detail); err == nil {
		t.Fatal("known closed job was allowed to proceed")
	}
}

func TestApplyExternalJSONIsCleanAndBackwardCompatible(t *testing.T) {
	applyURL := "https://jobs.lever.co/example/current/apply"
	detailBody := `{"data":{"jobsDashJobPostingDetailSectionsByCardSectionTypes":{"elements":[{"jobPostingDetailSection":[{"topCardV2":{"jobPostingCard":{"jobPostingTitle":"Example role","jobPosting":{"jobState":"LISTED"},"primaryActionV2":{"applyJobAction":{"applyJobActionResolutionResult":{"applicantTrackingSystemName":"Lever","onsiteApply":false,"applyCtaText":{"text":"Apply"},"companyApplyUrl":"` + applyURL + `"}}}}}}]}]}}}`
	value := runApplyJSONCommand(t, detailBody, `{"data":{"jobsDashOnsiteApplyApplicationByJobPosting":{"elements":[]}}}`)
	if value["externalUrl"] != applyURL || value["applyUrl"] != applyURL {
		t.Fatalf("URLs = %#v", value)
	}
	if value["listingUrl"] == applyURL {
		t.Fatal("listing URL was conflated with employer URL")
	}
}

func TestApplyDryRunJSONIsClean(t *testing.T) {
	detailBody := `{"data":{"jobsDashJobPostingDetailSectionsByCardSectionTypes":{"elements":[{"jobPostingDetailSection":[{"topCardV2":{"jobPostingCard":{"jobPostingTitle":"Example role","jobPosting":{"jobState":"LISTED"},"primaryActionV2":{"applyJobAction":{"applyJobActionResolutionResult":{"onsiteApply":true,"applyCtaText":{"text":"Easy Apply"}}}}}}}]}]}}}`
	applyBody := `{"data":{"jobsDashOnsiteApplyApplicationByJobPosting":{"elements":[{"jobSeekerApplicationDetail":{"onsiteApply":true,"applyCtaText":{"text":"Easy Apply"},"emailAddress":"candidate@example.com"}}]}}}`
	value := runApplyJSONCommand(t, detailBody, applyBody, "--dry-run")
	if value["dryRun"] != true || value["easyApply"] != true {
		t.Fatalf("result = %#v", value)
	}
}

func TestApplyUnavailableCheckJSONIsUnverified(t *testing.T) {
	detailBody := `{"data":{"jobsDashJobPostingDetailSectionsByCardSectionTypes":{"elements":[{"jobPostingDetailSection":[{"topCardV2":{"jobPostingCard":{"jobPostingTitle":"Example role","jobPosting":{"jobState":"LISTED"},"primaryActionV2":{"applyJobAction":{"applyJobActionResolutionResult":{"onsiteApply":true,"applyCtaText":{"text":"Easy Apply"}}}}}}}]}]}}}`
	value := runApplyJSONCommand(t, detailBody, `{"data":{"jobsDashOnsiteApplyApplicationByJobPosting":{"elements":[]}}}`)
	if value["applicationStatus"] != "unverified" || value["externalUrl"] != "" {
		t.Fatalf("result = %#v", value)
	}
}

func runApplyJSONCommand(t *testing.T, detailBody, applyBody string, flags ...string) map[string]interface{} {
	t.Helper()
	t.Setenv("LNK_LI_AT", "test-session")
	t.Setenv("LNK_CSRF_TOKEN", "test-csrf")
	originalFactory := newApplyClient
	newApplyClient = func(creds auth.Credentials, options ...api.Option) *api.Client {
		client := &http.Client{Transport: applyRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := applyBody
			if request.URL.Query().Get("queryName") == "JobPostingDetailSectionsByCardSectionTypesV2" {
				body = detailBody
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    request,
			}, nil
		})}
		return api.New(creds, append(options, api.WithHTTPClient(client))...)
	}
	t.Cleanup(func() { newApplyClient = originalFactory })

	jsonMode := true
	otherFlag := false
	command := NewApplyCmd(&otherFlag, &jsonMode, &otherFlag, &otherFlag, &otherFlag, &otherFlag, &otherFlag)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs(append([]string{"1234567890"}, flags...))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var value map[string]interface{}
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not clean JSON: %v; output=%q", err, output.String())
	}
	return value
}

func TestPrintNonEasyApplyDoesNotInventEmployerURL(t *testing.T) {
	tests := []struct {
		name      string
		applyURL  string
		want      string
		notWanted string
	}{
		{
			name:      "observed employer URL",
			applyURL:  "https://jobs.lever.co/example/id/apply",
			want:      "Observed employer application URL (unverified): https://jobs.lever.co/example/id/apply",
			notWanted: "Apply externally at",
		},
		{
			name:      "listing only",
			want:      "No employer application URL was observed.",
			notWanted: "Apply externally at",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			detail := &types.JobDetail{
				JobCard:     types.JobCard{ListingURL: "https://www.linkedin.com/jobs/view/1234567890"},
				Application: types.ApplicationAvailability{ApplyURL: test.applyURL},
			}
			var output bytes.Buffer
			printNonEasyApply(&output, detail)
			if !strings.Contains(output.String(), test.want) {
				t.Fatalf("output %q does not contain %q", output.String(), test.want)
			}
			if strings.Contains(output.String(), test.notWanted) {
				t.Fatalf("output %q contains misleading wording %q", output.String(), test.notWanted)
			}
		})
	}
}
