// Package api provides an HTTP client for LinkedIn's internal GraphQL API.
//
// LinkedIn's Android app communicates via HTTPS to voyager.linkedin.com using
// a combination of standard HTTP headers and RestLi-encoded query variables.
// This client replicates the required headers so the server treats requests as
// coming from the official Android client.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/restli"
	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	baseURL     = "https://www.linkedin.com"
	graphQLPath = "/voyager/api/graphql"

	defaultTimeout = 30 * time.Second
	maxRetries     = 4
	baseBackoff    = 1 * time.Second

	// Static Android client fingerprint mirroring decompiled APK values.
	trackHeader = `{"osName":"Android OS","osVersion":"36","clientVersion":"4.1.1209",` +
		`"model":"samsung_SM-S901E","displayDensity":2.625,"displayWidth":1080,` +
		`"displayHeight":2340,"osVersion":"BP2A.250605.031.A3","timezoneOffset":2,` +
		`"interfaceLocale":"en_US"}`

	userAgent = "com.linkedin.android/211700 (Linux; U; Android 16; en_ZA; " +
		"SM-S901E; Build/BP2A.250605.031.A3; Cronet/127.0.6533.65)"
)

// Client is a configured LinkedIn API client.
type Client struct {
	http    *http.Client
	creds   auth.Credentials
	verbose bool
	debug   bool
	errOut  io.Writer
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout sets the HTTP timeout (default 30 s).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// WithVerbose enables request/response logging to errOut.
func WithVerbose(v bool) Option { return func(c *Client) { c.verbose = v } }

// WithDebug enables full body logging to errOut.
func WithDebug(v bool) Option { return func(c *Client) { c.debug = v } }

// WithErrWriter sets the writer used for diagnostic output (default os.Stderr).
func WithErrWriter(w io.Writer) Option { return func(c *Client) { c.errOut = w } }

// New creates a Client using the provided credentials.
func New(creds auth.Credentials, opts ...Option) *Client {
	c := &Client{
		http:   &http.Client{Timeout: defaultTimeout},
		creds:  creds,
		errOut: io.Discard,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// QueryGraphQL executes a LinkedIn GraphQL query by name and ID with the given
// variables. variables must be encodable by the restli package.
//
// It returns the raw JSON response body on success. On rate-limit it retries
// with exponential back-off up to maxRetries times.
func (c *Client) QueryGraphQL(queryName, queryID string, variables interface{}) (json.RawMessage, error) {
	varStr, err := restli.Encode(variables)
	if err != nil {
		return nil, fmt.Errorf("api: cannot encode variables: %w", err)
	}

	// Build URL.
	u, _ := url.Parse(baseURL + graphQLPath)
	// Build raw query manually to avoid double-encoding the RestLi variables.
	// The RestLi encoder already URL-encodes special characters within values.
	u.RawQuery = "queryId=" + url.QueryEscape(queryID) +
		"&queryName=" + url.QueryEscape(queryName) +
		"&variables=" + varStr

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

		if c.verbose || c.debug {
			c.logf("→ GET %s", u.String())
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = types.NetworkError("HTTP request failed", err)
			continue
		}
		defer resp.Body.Close()

		if c.verbose || c.debug {
			c.logf("← %d %s", resp.StatusCode, resp.Status)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = types.RateLimitError()
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return nil, fmt.Errorf("api: unexpected status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, types.NetworkError("reading response body", err)
		}

		if c.debug {
			c.logf("body: %s", string(body))
		}

		return json.RawMessage(body), nil
	}

	return nil, lastErr
}

// injectHeaders attaches all required LinkedIn API headers to req.
func (c *Client) injectHeaders(req *http.Request) {
	csrf := c.creds.CSRFToken
	bcookie := c.creds.BCookie
	if bcookie == "" {
		bcookie = "v=2&00000000-0000-0000-0000-000000000000"
	}

	cookie := fmt.Sprintf(
		"li_at=%s; JSESSIONID=ajax:%s; bcookie=%s; liap=true; lang=v=2&lang=en_US",
		c.creds.LiAt, csrf, bcookie,
	)

	req.Header.Set("cookie", cookie)
	req.Header.Set("csrf-token", "ajax:"+csrf)
	req.Header.Set("x-restli-protocol-version", "2.0.0")
	req.Header.Set("x-li-lang", "en_US")
	req.Header.Set("x-li-track", trackHeader)
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
}

// logf writes a diagnostic message to errOut when verbose/debug is on.
func (c *Client) logf(format string, args ...interface{}) {
	fmt.Fprintf(c.errOut, "[lnk] "+format+"\n", args...)
}
