package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEmployerApplicationVerification(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		ats      string
		status   int
		body     string
		want     types.ApplicationStatus
		evidence string
	}{
		{
			name:     "Lever live application form",
			url:      "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
			ats:      "Lever",
			status:   http.StatusOK,
			body:     `<form id="application-form" method="POST"><button id="hcaptchaSubmitBtn" type="submit" class="hidden"></button><button id="btn-submit" type="button" data-qa="btn-submit">Submit application</button></form>`,
			want:     types.ApplicationAccepting,
			evidence: "POST Lever application form",
		},
		{
			name:     "Workday explicit canApply",
			url:      "https://example.wd3.myworkdayjobs.com/en-US/example/job/example",
			ats:      "Workday",
			status:   http.StatusOK,
			body:     `{"jobPostingInfo":{"canApply":true}}`,
			want:     types.ApplicationAccepting,
			evidence: "canApply=true",
		},
		{
			name:   "Workday explicit closure",
			url:    "https://example.wd3.myworkdayjobs.com/en-US/example/job/example",
			ats:    "Workday",
			status: http.StatusOK,
			body:   `{"jobPostingInfo":{"canApply":false}}`,
			want:   types.ApplicationClosed,
		},
		{
			name:   "Workday landing page without proof",
			url:    "https://example.wd3.myworkdayjobs.com/en-US/example/job/example",
			ats:    "Workday",
			status: http.StatusOK,
			body:   `<html><body>Job details</body></html>`,
			want:   types.ApplicationUnverified,
		},
		{
			name:   "login wall",
			url:    "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
			ats:    "Lever",
			status: http.StatusOK,
			body:   `<html><title>Sign in</title><form id="application-form" method="POST"></form></html>`,
			want:   types.ApplicationUnverified,
		},
		{
			name:   "challenge wall",
			url:    "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
			ats:    "Lever",
			status: http.StatusForbidden,
			body:   `<html><title>Just a moment</title></html>`,
			want:   types.ApplicationUnverified,
		},
		{
			name:   "free-text deadline is not structured closure proof",
			url:    "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply",
			ats:    "Lever",
			status: http.StatusOK,
			body:   `<html>This job is no longer accepting applications.</html>`,
			want:   types.ApplicationUnverified,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker := testEmployerChecker(t, func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			})
			result := checker.Check(context.Background(), test.url, test.ats)
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q; reason = %q", result.Status, test.want, result.Reason)
			}
			if test.evidence != "" && !strings.Contains(result.Evidence, test.evidence) {
				t.Fatalf("evidence = %q, want substring %q", result.Evidence, test.evidence)
			}
			if result.CheckedAt == "" {
				t.Fatal("checkedAt must be set after an employer request")
			}
		})
	}
}

func TestLeverApplicationVerificationIgnoresFreeTextClosureMarkers(t *testing.T) {
	tests := []struct {
		name string
		body string
		want types.ApplicationStatus
	}{
		{
			name: "comment on live form",
			body: `<!-- no longer accepting applications --><form id="application-form" method="POST"><button type="submit"></button></form>`,
			want: types.ApplicationAccepting,
		},
		{
			name: "script on live form",
			body: `<script>const message = "position has been filled";</script><form id="application-form" method="POST"><button type="submit"></button></form>`,
			want: types.ApplicationAccepting,
		},
		{
			name: "description without form",
			body: `<main><p>The application deadline has passed.</p></main>`,
			want: types.ApplicationUnverified,
		},
	}
	base := types.ApplicationAvailability{Status: types.ApplicationUnverified, ApplyURL: "https://jobs.lever.co/example/current/apply"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateEmployerResponse(base, "lever", http.StatusOK, []byte(test.body))
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q", result.Status, test.want)
			}
		})
	}
}

func TestLeverApplicationVerificationRejectsFalsePositiveMarkup(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "disabled submit",
			body: `<form id="application-form" method="POST"><button type="submit" disabled data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "aria disabled submit",
			body: `<form id="application-form" method="POST"><button type="submit" aria-disabled="true" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "aria hidden submit",
			body: `<form id="application-form" method="POST"><button type="submit" aria-hidden="true" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "disabled fieldset",
			body: `<form id="application-form" method="POST"><fieldset disabled><button type="submit" data-qa="btn-submit">Submit application</button></fieldset></form>`,
		},
		{
			name: "non-submit button",
			body: `<form id="application-form" method="POST"><button type="button" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "non-POST form",
			body: `<form id="application-form" method="GET"><button type="submit" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "markers split across forms",
			body: `<form id="application-form" method="POST"></form><form><button type="submit" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "markers in comment",
			body: `<!-- <form id="application-form" method="POST"><button type="submit" data-qa="btn-submit">Submit application</button></form> -->`,
		},
		{
			name: "markers in script",
			body: `<script>const markup = '<form id="application-form" method="POST"><button type="submit" data-qa="btn-submit"></button></form>';</script>`,
		},
		{
			name: "disabled proxy",
			body: `<form id="application-form" method="POST"><button type="submit" class="hidden" disabled></button><button type="button" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "different posting action",
			body: `<form id="application-form" method="POST" action="https://jobs.lever.co/example/different/apply"><button type="submit" data-qa="btn-submit">Submit application</button></form>`,
		},
		{
			name: "raw script false close delimiter",
			body: `<script>x</scriptx><form id="application-form" method="POST"><button type="submit"></button></form></script>`,
		},
		{
			name: "duplicate form attributes",
			body: `<form id="other" id="application-form" method="GET" method="POST"><button type="submit"></button></form>`,
		},
		{
			name: "control owned by other form",
			body: `<form id="application-form" method="POST"><button type="submit" form="other"></button></form>`,
		},
		{
			name: "control overrides method",
			body: `<form id="application-form" method="POST"><button type="submit" formmethod="GET"></button></form>`,
		},
		{
			name: "control overrides posting target",
			body: `<form id="application-form" method="POST"><button type="submit" formaction="https://jobs.lever.co/example/different/apply"></button></form>`,
		},
	}
	base := types.ApplicationAvailability{Status: types.ApplicationUnverified, ApplyURL: "https://jobs.lever.co/example/current/apply"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateEmployerResponse(base, "lever", http.StatusOK, []byte(test.body))
			if result.Status != types.ApplicationUnverified {
				t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
			}
		})
	}
}

func TestWorkdayApplicationVerificationRequiresExactJSONPath(t *testing.T) {
	tests := []struct {
		name string
		body string
		want types.ApplicationStatus
	}{
		{name: "HTML script token", body: `<html><script>window.data={"canApply":true}</script></html>`, want: types.ApplicationUnverified},
		{name: "unrelated JSON token", body: `{"other":{"canApply":true}}`, want: types.ApplicationUnverified},
		{name: "closure wins duplicate flag", body: `{"jobPostingInfo":{"canApply":true,"canApply":false}}`, want: types.ApplicationClosed},
		{name: "different posting identity", body: `{"jobPostingInfo":{"canApply":true,"externalPath":"/en-US/example/job/different"}}`, want: types.ApplicationUnverified},
	}
	base := types.ApplicationAvailability{Status: types.ApplicationUnverified, ApplyURL: "https://example.wd3.myworkdayjobs.com/en-US/example/job/current"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateEmployerResponse(base, "workday", http.StatusOK, []byte(test.body))
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q", result.Status, test.want)
			}
		})
	}
}

func TestEmployerApplicationVerificationNetworkErrorIsUnverified(t *testing.T) {
	checker := &employerApplicationChecker{
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		})},
		lookupIP: publicTestLookup,
		now:      fixedTestTime,
	}
	result := checker.Check(context.Background(), "https://jobs.lever.co/example/00000000-0000-0000-0000-000000000000/apply", "Lever")
	if result.Status != types.ApplicationUnverified {
		t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
	}
}

func TestEmployerApplicationVerificationRejectsUnsafeDestinations(t *testing.T) {
	tests := []struct {
		name string
		url  string
		ips  []net.IP
	}{
		{name: "HTTP", url: "http://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("203.0.113.10")}},
		{name: "loopback", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("127.0.0.1")}},
		{name: "private", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("10.0.0.1")}},
		{name: "link local", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("169.254.1.1")}},
		{name: "shared address space", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("100.64.1.1")}},
		{name: "documentation range", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("203.0.113.10")}},
		{name: "benchmark range", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("198.18.0.1")}},
		{name: "this network", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("0.1.2.3")}},
		{name: "NAT64", url: "https://jobs.lever.co/example/id/apply", ips: []net.IP{net.ParseIP("64:ff9b::0808:0808")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called := false
			checker := &employerApplicationChecker{
				http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					called = true
					return nil, errors.New("must not request unsafe URL")
				})},
				lookupIP: publicTestLookup,
				now:      fixedTestTime,
			}
			checker.lookupIP = func(context.Context, string) ([]net.IP, error) { return test.ips, nil }
			result := checker.Check(context.Background(), test.url, "Lever")
			if result.Status != types.ApplicationUnverified {
				t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
			}
			if called {
				t.Fatal("unsafe destination was requested")
			}
		})
	}
}

func TestIPv6SiteLocalBoundary(t *testing.T) {
	siteLocal := netip.MustParsePrefix("fec0::/10")
	membership := []struct {
		address string
		inside  bool
	}{
		{address: "febf::", inside: false},
		{address: "fec0::", inside: true},
		{address: "feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", inside: true},
	}
	for _, test := range membership {
		address := netip.MustParseAddr(test.address)
		if siteLocal.Contains(address) != test.inside {
			t.Fatalf("fec0::/10 membership for %s = %t, want %t", address, siteLocal.Contains(address), test.inside)
		}
	}

	publicity := []struct {
		address string
		public  bool
	}{
		{address: "febf::", public: false},
		{address: "fec0::", public: false},
		{address: "feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", public: false},
		{address: "2001:4860:4860::8888", public: true},
	}
	for _, test := range publicity {
		got := isPublicApplicationIP(net.ParseIP(test.address))
		if got != test.public {
			t.Fatalf("public status for %s = %t, want %t", test.address, got, test.public)
		}
	}
}

func TestLeverApplicationVerificationRespectsAncestorVisibility(t *testing.T) {
	tests := []struct {
		name string
		body string
		want types.ApplicationStatus
	}{
		{
			name: "hidden form",
			body: `<form id="application-form" method="POST" hidden><button type="submit"></button></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "aria hidden form",
			body: `<form id="application-form" method="POST" aria-hidden="true"><button type="submit"></button></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "display none form",
			body: `<form id="application-form" method="POST" style="DISPLAY : none"><button type="submit"></button></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "hidden wrapper",
			body: `<form id="application-form" method="POST"><div hidden><button type="submit"></button></div></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "hidden wrapper outside form",
			body: `<div hidden><form id="application-form" method="POST"><button type="submit"></button></form></div>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "aria hidden wrapper",
			body: `<form id="application-form" method="POST"><div aria-hidden="true"><button type="submit"></button></div></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "visibility hidden wrapper",
			body: "<form id=\"application-form\" method=\"POST\"><div style=\"visibility:\n hidden\"><button type=\"submit\"></button></div></form>",
			want: types.ApplicationUnverified,
		},
		{
			name: "hidden class wrapper",
			body: `<form id="application-form" method="POST"><div class="section hidden"><button type="submit"></button></div></form>`,
			want: types.ApplicationUnverified,
		},
		{
			name: "visible form and wrapper",
			body: `<form id="application-form" method="POST"><div><button type="submit"></button></div></form>`,
			want: types.ApplicationAccepting,
		},
	}
	base := types.ApplicationAvailability{Status: types.ApplicationUnverified, ApplyURL: "https://jobs.lever.co/example/current/apply"}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := evaluateEmployerResponse(base, "lever", http.StatusOK, []byte(test.body))
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q", result.Status, test.want)
			}
		})
	}
}

func TestEmployerApplicationVerificationRejectsDifferentPostingRedirect(t *testing.T) {
	checker := testEmployerChecker(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/example/current/apply" {
			writer.Header().Set("Location", "https://jobs.lever.co/example/different/apply")
			writer.WriteHeader(http.StatusFound)
			return
		}
		_, _ = writer.Write([]byte(`<form id="application-form" method="POST"><button type="submit" data-qa="btn-submit">Submit application</button></form>`))
	})
	result := checker.Check(context.Background(), "https://jobs.lever.co/example/current/apply", "Lever")
	if result.Status != types.ApplicationUnverified {
		t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
	}
}

func TestEmployerApplicationVerificationRejectsDifferentWorkdayHost(t *testing.T) {
	checker := testEmployerChecker(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", "https://other.wd3.myworkdayjobs.com/en-US/example/job/current")
		writer.WriteHeader(http.StatusFound)
	})
	result := checker.Check(context.Background(), "https://example.wd3.myworkdayjobs.com/en-US/example/job/current", "Workday")
	if result.Status != types.ApplicationUnverified {
		t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
	}
}

type closeTrackingTransport struct {
	closed bool
}

func (transport *closeTrackingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	_, _ = recorder.WriteString(`<form id="application-form" method="POST"><button type="submit" data-qa="btn-submit">Submit application</button></form>`)
	return recorder.Result(), nil
}

func (transport *closeTrackingTransport) CloseIdleConnections() {
	transport.closed = true
}

func TestEmployerApplicationVerificationClosesIdleConnections(t *testing.T) {
	transport := &closeTrackingTransport{}
	checker := &employerApplicationChecker{
		http:     &http.Client{Transport: transport},
		lookupIP: publicTestLookup,
		now:      fixedTestTime,
	}
	checker.Check(context.Background(), "https://jobs.lever.co/example/current/apply", "Lever")
	if !transport.closed {
		t.Fatal("idle connections were not closed")
	}
}

func TestEmployerApplicationVerificationChecksRedirectAndLeaksNoCredentials(t *testing.T) {
	var mu sync.Mutex
	var requests []*http.Request
	checker := testEmployerChecker(t, func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Clone(request.Context()))
		mu.Unlock()
		if request.URL.Query().Get("redirected") == "" {
			writer.Header().Set("Location", "https://jobs.lever.co/example/id/apply?redirected=1")
			writer.WriteHeader(http.StatusFound)
			return
		}
		_, _ = writer.Write([]byte(`<form id="application-form" method="POST"><button data-qa="btn-submit">Submit application</button></form>`))
	})
	result := checker.Check(context.Background(), "https://jobs.lever.co/example/id/apply", "Lever")
	if result.Status != types.ApplicationAccepting {
		t.Fatalf("safe redirect status = %q, want %q", result.Status, types.ApplicationAccepting)
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	for _, request := range requests {
		for _, header := range []string{"Authorization", "Cookie", "Csrf-Token"} {
			if request.Header.Get(header) != "" {
				t.Fatalf("%s leaked to employer request", header)
			}
		}
	}

	checker = testEmployerChecker(t, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", "https://127.0.0.1/form")
		writer.WriteHeader(http.StatusFound)
	})
	checker.lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		if host == "127.0.0.1" {
			return []net.IP{net.ParseIP(host)}, nil
		}
		return publicTestLookup(ctx, host)
	}
	result = checker.Check(context.Background(), "https://jobs.lever.co/example/id/apply", "Lever")
	if result.Status != types.ApplicationUnverified || !strings.Contains(result.Reason, "redirect") {
		t.Fatalf("private redirect result = %#v", result)
	}
}

func TestEmployerApplicationVerificationDoesNotRequestUnsupportedATS(t *testing.T) {
	called := false
	checker := &employerApplicationChecker{
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected request")
		})},
		lookupIP: publicTestLookup,
		now:      fixedTestTime,
	}
	result := checker.Check(context.Background(), "https://careers.example.com/jobs/123", "ExampleATS")
	if result.Status != types.ApplicationUnverified {
		t.Fatalf("status = %q, want %q", result.Status, types.ApplicationUnverified)
	}
	if called {
		t.Fatal("unsupported ATS was requested")
	}
}

func testEmployerChecker(t *testing.T, handler http.HandlerFunc) *employerApplicationChecker {
	t.Helper()
	return &employerApplicationChecker{
		http: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			return recorder.Result(), nil
		})},
		lookupIP: publicTestLookup,
		now:      fixedTestTime,
	}
}

func publicTestLookup(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("8.8.8.8")}, nil
}

func fixedTestTime() time.Time {
	return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
}
