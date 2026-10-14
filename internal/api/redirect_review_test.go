package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/safety"
	"github.com/yashiels/linkedin-cli/internal/types"
)

func TestDeepAuthRedirectsPersistStop(t *testing.T) {
	for _, depth := range []int{6, 12, 48, 4096} {
		for _, authPath := range []string{"/%6cogin/step", "/%61uthwall", "/uas/%6cogin", "/voyager/api/../../checkpoint%2fchallenge/step", "/x/%2e%2e/%6cogin"} {
			target := authPath
			for i := 1; i < depth; i++ {
				target = strings.ReplaceAll(target, "%", "%25")
			}
			t.Run(fmt.Sprintf("%d/%s", depth, authPath), func(t *testing.T) {
				count := 0
				c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					count++
					return fixtureResponse(302, target+"?PRIVATE-REDIRECT-SENTINEL"), nil
				}))
				if _, err := c.Ping(); err == nil {
					t.Fatal("redirect accepted")
				}
				fresh := New(auth.Credentials{}, WithHTTPClient(c.http), func(next *Client) { next.policy = c.policy })
				_, err := fresh.Ping()
				if err == nil || count != 1 {
					t.Fatalf("deep auth redirect did not latch: depth=%d count=%d error=%v", depth, count, err)
				}
			})
		}
	}
}

func TestOrdinaryAndMalformedRedirectsRemainRejected(t *testing.T) {
	for _, target := range []string{"/ordinary?PRIVATE-SENTINEL", "/ordinary/%25zz", "/login-other/%25zz", "/authwall-other/%25zz", "/checkpoint/challenges/%25zz", "/other/login/%25zz", "/login/%25zz/../../ordinary", "/login/%zz", "https://external.invalid/login/%25zz", "http://www.linkedin.com/login/%25zz", "https://user@www.linkedin.com/login/%25zz", "https://www.linkedin.com:443/login/%25zz", "/%zz?PRIVATE-SENTINEL", "/%252?PRIVATE-SENTINEL", "https://external.invalid/%256cogin?PRIVATE-SENTINEL"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return fixtureResponse(302, target), nil
				}
				return fixtureResponse(200, ""), nil
			}))
			_, err := c.Ping()
			if err == nil || strings.Contains(err.Error(), "SENTINEL") {
				t.Fatalf("redirect accepted or leaked: %v", err)
			}
			if _, err = c.Ping(); err != nil || calls != 2 {
				t.Fatalf("ordinary redirect latched or followed: %v count=%d", err, calls)
			}
		})
	}
}

func TestLiteralPercentAuthRedirectsPersistAcrossPolicies(t *testing.T) {
	for _, depth := range []int{1, 6, 48, 4096} {
		for _, authPath := range []string{"/login/%25zz", "/uas/login/%25zz", "/authwall/%25zz", "/checkpoint/challenge/%25zz", "/%6cogin/%25zz", "/uas/%6cogin/%25zz", "/%61uthwall/%25zz", "/checkpoint%2fchallenge/%25zz", "/x/%2e%2e/%6cogin/%25zz"} {
			target := authPath
			for i := 1; i < depth; i++ {
				target = strings.ReplaceAll(target, "%", "%25")
			}
			t.Run(fmt.Sprintf("%d/%s", depth, authPath), func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(root, 0700); err != nil {
					t.Fatal(err)
				}
				now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
				cfg := safety.Config{Now: func() time.Time { return now }, Wait: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }}
				calls := 0
				transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(302, target), nil })
				newClient := func() *Client {
					policy, err := safety.New(root, cfg)
					if err != nil {
						t.Fatal(err)
					}
					return New(auth.Credentials{LiAt: "synthetic-percent", CSRFToken: "synthetic-csrf"}, WithHTTPClient(&http.Client{Transport: transport}), func(c *Client) { c.policy = policy })
				}
				_, err = newClient().Ping()
				var coded *types.LnkError
				if !errors.As(err, &coded) || coded.Status != 302 || coded.Code != types.ExitAuth || coded.Reason != types.ReasonRefusal || calls != 1 {
					t.Fatalf("redirect error=%v calls=%d", err, calls)
				}
				_, err = newClient().Ping()
				if !errors.As(err, &coded) || coded.Code != types.ExitAuth || coded.Status != 0 || calls != 1 {
					t.Fatalf("fresh policy failed to honor persisted stop: error=%v calls=%d", err, calls)
				}
				persisted, policyErr := safety.New(root, cfg)
				if policyErr != nil {
					t.Fatal(policyErr)
				}
				state, stateErr := persisted.Status(context.Background())
				if stateErr != nil || state.Block != "auth" || state.Admissions != 1 {
					t.Fatalf("persisted state=%+v error=%v", state, stateErr)
				}
			})
		}
	}
}
