package api

import (
	"fmt"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"net/http"
	"strings"
	"testing"
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
	for _, target := range []string{"/ordinary?PRIVATE-SENTINEL", "/%zz?PRIVATE-SENTINEL", "/%252?PRIVATE-SENTINEL", "https://external.invalid/%256cogin?PRIVATE-SENTINEL"} {
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
