// Package api — profile.go provides the dash profiles REST endpoint client.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	dashProfilesPath = "/voyager/api/identity/dash/profiles"
	dashDecoration   = "com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-109"
)

// GetProfileByIdentity fetches a complete LinkedIn profile using the dash profiles
// REST endpoint. This endpoint is the current working replacement for the
// deprecated /voyager/api/identity/profiles/<vanityName> endpoint and returns
// full profile data including education, experience, skills, certifications,
// and honors in a single request.
//
// Endpoint:
//
//	GET /voyager/api/identity/dash/profiles
//	  ?q=memberIdentity
//	  &memberIdentity=<vanityName>
//	  &decorationId=com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-109
//
// Response: {"elements": [{...full profile object...}]}
func (c *Client) GetProfileByIdentity(vanityName string) (json.RawMessage, error) {
	params := url.Values{
		"q":              {"memberIdentity"},
		"memberIdentity": {vanityName},
		"decorationId":   {dashDecoration},
	}
	u, _ := url.Parse(baseURL + dashProfilesPath)
	u.RawQuery = params.Encode()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt-1))) * baseBackoff
			c.logf("profile: rate limited, retrying in %s (attempt %d/%d)", wait, attempt, maxRetries)
			time.Sleep(wait)
		}

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("api: cannot build dash profile request: %w", err)
		}
		// injectHeaders sets accept: application/json; do NOT override to normalized JSON
		// because the dash endpoint uses standard JSON format with elements[].
		c.injectHeaders(req)

		if c.verbose || c.debug {
			c.logf("→ GET %s", u.String())
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// Unwrap url.Error to check for auth errors from CheckRedirect.
			// Auth errors (session expiry) must not be retried.
			var urlErr *url.Error
			if errors.As(err, &urlErr) {
				var authErr *types.LnkError
				if errors.As(urlErr.Err, &authErr) && authErr.Code == types.ExitAuth {
					return nil, authErr
				}
				if !urlErr.Timeout() {
					return nil, types.NetworkError("HTTP GET dash profile failed", err)
				}
			}
			lastErr = types.NetworkError("HTTP GET dash profile failed", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = types.NetworkError("reading dash profile response body", readErr)
			continue
		}

		if c.verbose || c.debug {
			c.logf("← %d %s", resp.StatusCode, resp.Status)
		}
		if c.debug {
			c.logf("body: %s", string(body))
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return json.RawMessage(body), nil
		case http.StatusTooManyRequests:
			lastErr = types.RateLimitError()
			continue
		default:
			return nil, fmt.Errorf("api: dash profile returned %d: %s",
				resp.StatusCode, truncateBody(body))
		}
	}
	return nil, lastErr
}
