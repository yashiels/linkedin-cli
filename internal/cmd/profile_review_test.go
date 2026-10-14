package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/safety"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type profileReviewReply struct {
	status  int
	body    string
	err     error
	readErr bool
}

type profileReviewTransport func(*http.Request) (*http.Response, error)

func (f profileReviewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type profileReviewBrokenBody struct{}

func (profileReviewBrokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (profileReviewBrokenBody) Close() error             { return nil }

func runProfileReview(t *testing.T, replies []profileReviewReply, mini json.RawMessage, wantCount, wantStatus int, wantReason types.DiagnosticReason, wantName string) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", root)
		t.Setenv("LNK_CONFIG", filepath.Join(root, "config.toml"))
		t.Setenv("LNK_LI_AT", "")
		t.Setenv("LNK_CSRF_TOKEN", "")
		var paths []string
		var times []time.Time
		client := api.New(auth.Credentials{LiAt: "synthetic-profile", CSRFToken: "synthetic-csrf"}, api.WithHTTPClient(&http.Client{Transport: profileReviewTransport(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Path)
			times = append(times, time.Now())
			i := len(paths) - 1
			if i >= len(replies) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}
			reply := replies[i]
			if reply.err != nil {
				return nil, reply.err
			}
			var body io.ReadCloser = io.NopCloser(strings.NewReader(reply.body))
			if reply.readErr {
				body = profileReviewBrokenBody{}
			}
			return &http.Response{StatusCode: reply.status, Header: make(http.Header), Body: body}, nil
		})}))
		profile, err := fetchProfile(client, "fixture", "urn:li:fsd_profile:fixture", mini)
		if len(paths) != wantCount {
			t.Errorf("exchanges=%d want=%d paths=%v error=%v", len(paths), wantCount, paths, err)
		}
		if wantReason != types.ReasonUnspecified {
			var coded *types.LnkError
			if !errors.As(err, &coded) || coded.Status != wantStatus || coded.Reason != wantReason {
				t.Errorf("error=%+v want status=%d reason=%d", err, wantStatus, wantReason)
			}
		} else if err != nil || profile == nil || profile.FirstName != wantName {
			t.Errorf("profile=%+v error=%v want name=%q", profile, err, wantName)
		}
		policy, policyErr := safety.Default(safety.Config{})
		if policyErr != nil {
			t.Fatal(policyErr)
		}
		state, stateErr := policy.Status(context.Background())
		if stateErr != nil || state.Admissions != len(paths) {
			t.Errorf("admissions=%d exchanges=%d error=%v", state.Admissions, len(paths), stateErr)
		}
		for i := 1; i < len(times); i++ {
			if times[i].Sub(times[i-1]) < 5*time.Second {
				t.Errorf("unpaced exchange %d: %v", i, times[i].Sub(times[i-1]))
			}
		}
	})
}

func TestProfileHTTPFailuresStopExchanges(t *testing.T) {
	empty := profileReviewReply{status: 200, body: `{}`}
	basic := profileReviewReply{status: 200, body: `{"firstName":"Fixture"}`}
	gone := profileReviewReply{status: 410, body: `{}`}
	stages := []struct {
		name   string
		before []profileReviewReply
	}{
		{"dash", nil}, {"basic", []profileReviewReply{empty}},
		{"graphql_urn", []profileReviewReply{empty, gone}},
		{"graphql_identity", []profileReviewReply{empty, gone, empty}},
		{"view", []profileReviewReply{empty, basic}},
		{"network", []profileReviewReply{empty, basic, empty}},
	}
	for _, stage := range stages {
		for _, status := range []int{400, 404, 410, 500, 502} {
			if stage.name == "basic" && status == 410 {
				continue
			}
			t.Run(fmt.Sprintf("%s/%d", stage.name, status), func(t *testing.T) {
				replies := append(append([]profileReviewReply{}, stage.before...), profileReviewReply{status: status, body: `{}`})
				runProfileReview(t, replies, nil, len(replies), status, types.ReasonHTTPStatus, "")
			})
		}
	}
}

func TestProfileCompatibilityAndEmptyData(t *testing.T) {
	empty := profileReviewReply{status: 200, body: `{}`}
	basic := profileReviewReply{status: 200, body: `{"firstName":"Fixture"}`}
	gone := profileReviewReply{status: 410, body: `{}`}
	graph := profileReviewReply{status: 200, body: `{"data":{"profile":{"elements":[{"firstName":"Fixture"}]}}}`}
	for _, tc := range []struct {
		name    string
		replies []profileReviewReply
		mini    json.RawMessage
	}{
		{"dash", []profileReviewReply{{status: 200, body: `{"elements":[{"firstName":"Fixture"}]}`}}, nil},
		{"legacy", []profileReviewReply{empty, basic, empty, empty}, nil},
		{"gone_graphql", []profileReviewReply{empty, gone, graph, empty, empty}, nil},
		{"empty_graphql_urn", []profileReviewReply{empty, gone, empty, graph, empty, empty}, nil},
		{"empty_seeded", []profileReviewReply{empty, gone, empty, empty, empty, empty}, json.RawMessage(`{"firstName":"Fixture"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runProfileReview(t, tc.replies, tc.mini, len(tc.replies), 0, types.ReasonUnspecified, "Fixture")
		})
	}
}

func TestProfileLaterHardStops(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  profileReviewReply
		status int
		reason types.DiagnosticReason
	}{
		{"auth", profileReviewReply{status: 401, body: `{}`}, 401, types.ReasonRefusal},
		{"forbidden", profileReviewReply{status: 403, body: `{}`}, 403, types.ReasonRefusal},
		{"restricted", profileReviewReply{status: 999, body: `{}`}, 999, types.ReasonRefusal},
		{"body_size", profileReviewReply{status: 200, body: strings.Repeat("x", safety.MaxBody+1)}, 200, types.ReasonResponseSize},
		{"rate", profileReviewReply{status: 429, body: `{}`}, 429, types.ReasonRefusal},
		{"transport", profileReviewReply{err: io.ErrUnexpectedEOF}, 0, types.ReasonTransport},
		{"body_read", profileReviewReply{status: 200, readErr: true}, 200, types.ReasonResponseRead},
		{"json", profileReviewReply{status: 200, body: `invalid`}, 200, types.ReasonJSON},
		{"graphql", profileReviewReply{status: 200, body: `{"errors":[{"message":"fixture"}]}`}, 200, types.ReasonGraphQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runProfileReview(t, []profileReviewReply{{status: 200, body: `{}`}, {status: 410, body: `{}`}, tc.reply}, nil, 3, tc.status, tc.reason, "")
		})
	}
}
