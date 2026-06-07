package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/yashiels/linkedin-cli/internal/types"
)

// PostJSON sends a JSON-encoded body to the given Voyager API path (relative
// to baseURL, e.g. "/voyager/api/voyagerJobsDashOnsiteApplyApplication")
// with optional query parameters appended to the URL.
//
// queryParams is a map of key→value pairs appended as "?key=value" after
// the path. Pass nil for no query params.
//
// Returns the raw JSON response body or an error.
func (c *Client) PostJSON(path string, queryParams map[string]string, body interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("api: cannot marshal request body: %w", err)
	}

	target := baseURL + path
	if len(queryParams) > 0 {
		first := true
		for k, v := range queryParams {
			if first {
				target += "?" + k + "=" + v
				first = false
			} else {
				target += "&" + k + "=" + v
			}
		}
	}

	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("api: cannot build POST request: %w", err)
	}

	c.injectHeaders(req)
	req.Header.Set("content-type", "application/json")

	if c.verbose || c.debug {
		c.logf("→ POST %s", target)
		if c.debug {
			c.logf("body: %s", string(data))
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, types.NetworkError("HTTP POST request failed", err)
	}
	defer resp.Body.Close()

	if c.verbose || c.debug {
		c.logf("← %d %s", resp.StatusCode, resp.Status)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, types.RateLimitError()
	}

	// 2xx is success; LinkedIn sometimes returns 201 or 204 for creates.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("api: unexpected status %d: %s", resp.StatusCode, string(errBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NetworkError("reading POST response body", err)
	}

	if c.debug && len(respBody) > 0 {
		c.logf("response body: %s", string(respBody))
	}

	if len(respBody) == 0 {
		return json.RawMessage("{}"), nil
	}

	return json.RawMessage(respBody), nil
}

// DeleteResource sends a DELETE request to the given Voyager API path.
// Returns an error on non-2xx responses.
func (c *Client) DeleteResource(path string) error {
	req, err := http.NewRequest(http.MethodDelete, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("api: cannot build DELETE request: %w", err)
	}

	c.injectHeaders(req)

	if c.verbose || c.debug {
		c.logf("→ DELETE %s", baseURL+path)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return types.NetworkError("HTTP DELETE request failed", err)
	}
	defer resp.Body.Close()

	if c.verbose || c.debug {
		c.logf("← %d %s", resp.StatusCode, resp.Status)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return types.RateLimitError()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("api: DELETE returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
