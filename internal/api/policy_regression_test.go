package api

import (
	"errors"
	"github.com/yashiels/linkedin-cli/internal/types"
	"net/http"
	"testing"
)

func TestM2SingleShotRefusals(t *testing.T) {
	for _, tc := range requestWrappers() {
		for _, status := range []int{429, 401, 403, 999} {
			t.Run(tc.name+http.StatusText(status), func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				calls := 0
				c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(status, ""), nil }))
				err := tc.call(c)
				expected := types.ExitAuth
				if status == 429 {
					expected = types.ExitRateLimit
				}
				var coded *types.LnkError
				if !errors.As(err, &coded) || coded.Code != expected || calls != 1 {
					t.Fatalf("expected exit %d and one admission; error=%v calls=%d", expected, err, calls)
				}
			})
		}
	}
}
