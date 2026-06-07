// rest.go extends Client with generic REST GET/POST/DELETE methods for
// LinkedIn's non-GraphQL voyager endpoints.
package api

import (
	"bytes"
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

// Get performs a retried GET against a LinkedIn REST path (not GraphQL).
// path should start with "/" e.g. "/voyager/api/me".
// params are appended as query-string key/value pairs.
func (c *Client) Get(path string, params url.Values) (json.RawMessage, error) {
	u, _ := url.Parse(baseURL + path)
	if len(params) > 0 {
		u.RawQuery = params.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := time.Duration(math.Pow(2, float64(attempt-1))) * baseBackoff
			c.logf("rate limited, retrying in %s (attempt %d/%d)", wait, attempt, maxRetries)
			time.Sleep(wait)
		}

		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("api: cannot build request: %w", err)
		}
		c.injectHeaders(req)
		// Prefer structured JSON responses.
		req.Header.Set("accept", "application/vnd.linkedin.normalized+json+2.1")

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
					return nil, types.NetworkError("HTTP GET failed", err)
				}
			}
			lastErr = types.NetworkError("HTTP GET failed", err)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = types.NetworkError("reading response body", readErr)
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
			return nil, fmt.Errorf("api: unexpected status %d: %s",
				resp.StatusCode, truncateBody(body))
		}
	}
	return nil, lastErr
}

// Post performs a JSON POST to a LinkedIn REST path.
func (c *Client) Post(path string, payload interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("api: cannot marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("api: cannot build request: %w", err)
	}
	c.injectHeaders(req)
	req.Header.Set("content-type", "application/json")

	if c.verbose || c.debug {
		c.logf("→ POST %s", baseURL+path)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, types.NetworkError("HTTP POST failed", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if c.verbose || c.debug {
		c.logf("← %d %s", resp.StatusCode, resp.Status)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, types.RateLimitError()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("api: unexpected status %d: %s",
			resp.StatusCode, truncateBody(body))
	}
	if len(body) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.RawMessage(body), nil
}

// Delete performs a DELETE against a LinkedIn REST path.
func (c *Client) Delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("api: cannot build request: %w", err)
	}
	c.injectHeaders(req)

	if c.verbose || c.debug {
		c.logf("→ DELETE %s", baseURL+path)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return types.NetworkError("HTTP DELETE failed", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if c.verbose || c.debug {
		c.logf("← %d %s", resp.StatusCode, resp.Status)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return types.RateLimitError()
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("api: unexpected status %d: %s",
			resp.StatusCode, truncateBody(body))
	}
	return nil
}

// Ping makes a lightweight request to verify API connectivity.
// Returns the datacenter identifier if present in response headers.
func (c *Client) Ping() (string, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/voyager/api/me", nil)
	if err != nil {
		return "", fmt.Errorf("api: cannot build request: %w", err)
	}
	c.injectHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", types.NetworkError("connectivity check failed", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", types.AuthError("session expired or credentials invalid")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api: ping returned %d", resp.StatusCode)
	}
	// Try to extract datacenter from Via or X-LI-Pop headers.
	if pop := resp.Header.Get("x-li-pop"); pop != "" {
		return pop, nil
	}
	if via := resp.Header.Get("via"); via != "" {
		return via, nil
	}
	return "unknown", nil
}

func truncateBody(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}
