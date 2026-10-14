package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/restli"
)

const (
	baseURL          = "https://www.linkedin.com"
	graphQLPath      = "/voyager/api/graphql"
	defaultTimeout   = 30 * time.Second
	maxRetries       = 4
	baseBackoff      = time.Second
	legacyAppVersion = "4.1.1209"
	legacyAppBuild   = "211700"
	userAgent        = "com.linkedin.android/" + legacyAppBuild + " (Linux; U; Android 16; en_ZA; " +
		"SM-S901E; Build/BP2A.250605.031.A3; Cronet/127.0.6533.65)"
)

type trackingHeader struct {
	OSName          string  `json:"osName"`
	OSVersion       string  `json:"osVersion"`
	ClientVersion   string  `json:"clientVersion"`
	Model           string  `json:"model"`
	DisplayDensity  float64 `json:"displayDensity"`
	DisplayWidth    int     `json:"displayWidth"`
	DisplayHeight   int     `json:"displayHeight"`
	TimezoneOffset  int     `json:"timezoneOffset"`
	InterfaceLocale string  `json:"interfaceLocale"`
}

type Client struct {
	http    *http.Client
	creds   auth.Credentials
	verbose bool
	debug   bool
	errOut  io.Writer
	timeout *time.Duration
}

type Option func(*Client)

func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = &d }
}

func WithVerbose(v bool) Option        { return func(c *Client) { c.verbose = v } }
func WithDebug(v bool) Option          { return func(c *Client) { c.debug = v } }
func WithErrWriter(w io.Writer) Option { return func(c *Client) { c.errOut = w } }

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			owned := *client
			c.http = &owned
		}
	}
}

func New(creds auth.Credentials, opts ...Option) *Client {
	c := &Client{http: &http.Client{Timeout: defaultTimeout}, creds: creds, errOut: io.Discard}
	for _, o := range opts {
		o(c)
	}
	if c.timeout != nil {
		c.http.Timeout = *c.timeout
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func (c *Client) QueryGraphQL(queryName, queryID string, variables interface{}) (json.RawMessage, error) {
	varStr, err := restli.Encode(variables)
	if err != nil {
		return nil, fmt.Errorf("api: cannot encode variables: %w", err)
	}
	query := "queryId=" + url.QueryEscape(queryID) + "&queryName=" + url.QueryEscape(queryName) + "&variables=" + varStr
	return c.readJSON(context.Background(), graphQLReadOperation, baseURL+graphQLPath+"?"+query)
}

func (c *Client) injectHeaders(req *http.Request) {
	csrf := c.creds.CSRFToken
	bcookie := c.creds.BCookie
	if bcookie == "" {
		bcookie = "v=2&00000000-0000-0000-0000-000000000000"
	}
	cookie := fmt.Sprintf("li_at=%s; JSESSIONID=ajax:%s; bcookie=%s; liap=true; lang=v=2&lang=en_US", c.creds.LiAt, csrf, bcookie)
	track, _ := json.Marshal(trackingHeader{
		OSName: "Android OS", OSVersion: "36", ClientVersion: legacyAppVersion,
		Model: "samsung_SM-S901E", DisplayDensity: 2.625, DisplayWidth: 1080,
		DisplayHeight: 2340, TimezoneOffset: 2, InterfaceLocale: "en_US",
	})
	req.Header.Set("cookie", cookie)
	req.Header.Set("csrf-token", "ajax:"+csrf)
	req.Header.Set("x-restli-protocol-version", "2.0.0")
	req.Header.Set("x-li-lang", "en_US")
	req.Header.Set("x-li-track", string(track))
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
}

func (c *Client) logf(format string, args ...interface{}) {
	fmt.Fprintf(c.errOut, "[lnk] "+format+"\n", args...)
}
