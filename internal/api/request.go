package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/yashiels/linkedin-cli/internal/safety"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type operation uint8

const (
	readOperation operation = iota
	graphQLReadOperation
	normalizedReadOperation
	postOperation
	deleteOperation
)

func linkedinOrigin(u *url.URL) bool {
	return u.Scheme == "https" && u.Host == "www.linkedin.com" && u.User == nil && u.Opaque == ""
}

func safeDestination(target string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil || !linkedinOrigin(u) || u.Fragment != "" || u.RawFragment != "" || strings.Contains(target, "#") {
		return nil, types.NetworkError("api: rejected LinkedIn destination", nil)
	}
	p := u.EscapedPath()
	for range 5 {
		decoded, err := url.PathUnescape(p)
		if err != nil {
			return nil, types.NetworkError("api: rejected LinkedIn path", nil)
		}
		if strings.ContainsAny(decoded, "\\?#\r\n\t\x00") || strings.Contains(decoded, "//") || !strings.HasPrefix(decoded, "/voyager/api/") || path.Clean(decoded) != strings.TrimSuffix(decoded, "/") {
			return nil, types.NetworkError("api: rejected LinkedIn path", nil)
		}
		if decoded == p {
			return u, nil
		}
		if strings.Count(decoded, "/") != strings.Count(p, "/") {
			return nil, types.NetworkError("api: rejected encoded path separator", nil)
		}
		p = decoded
	}
	return nil, types.NetworkError("api: rejected LinkedIn path encoding", nil)
}

func authRedirect(origin *url.URL, location string) bool {
	u, err := origin.Parse(location)
	if err != nil || !linkedinOrigin(u) {
		return false
	}
	p, ok := decodeRedirectPath(u.EscapedPath())
	if !ok {
		return false
	}
	p = path.Clean(p)
	for _, prefix := range []string{"/login", "/uas/login", "/authwall", "/checkpoint/challenge"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func decodeRedirectPath(p string) (string, bool) {
	decoded := make([]byte, 0, len(p))
	remaining := len(p) / 2
	for i := range len(p) {
		decoded = append(decoded, p[i])
		for len(decoded) >= 3 && decoded[len(decoded)-3] == '%' {
			high, highOK := hexDigit(decoded[len(decoded)-2])
			low, lowOK := hexDigit(decoded[len(decoded)-1])
			if !highOK || !lowOK {
				break
			}
			if remaining == 0 {
				return "", false
			}
			remaining--
			decoded[len(decoded)-3] = high<<4 | low
			decoded = decoded[:len(decoded)-2]
		}
	}
	if bytes.IndexByte(decoded, '%') >= 0 {
		return "", false
	}
	return string(decoded), true
}

func hexDigit(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func (op operation) name() string {
	switch op {
	case readOperation:
		return "read"
	case graphQLReadOperation:
		return "graphql"
	case normalizedReadOperation:
		return "normalized_read"
	case postOperation:
		return "post"
	case deleteOperation:
		return "delete"
	default:
		return ""
	}
}
func (op operation) method() string {
	switch op {
	case postOperation:
		return http.MethodPost
	case deleteOperation:
		return http.MethodDelete
	default:
		return http.MethodGet
	}
}
func requestFailure(op operation, status, code int, reason types.DiagnosticReason) error {
	return types.WithRequestDetails(types.NewError(code, "LinkedIn request failed"), op.method(), op.name(), status, reason)
}

func (c *Client) send(ctx context.Context, op operation, target string, payload []byte) (*http.Response, error) {
	u, err := safeDestination(target)
	if err != nil {
		return nil, err
	}
	method := http.MethodGet
	switch op {
	case readOperation, graphQLReadOperation, normalizedReadOperation:
	case postOperation:
		method = http.MethodPost
	case deleteOperation:
		method = http.MethodDelete
	default:
		return nil, types.NetworkError("api: invalid operation", nil)
	}
	timeout := c.http.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	policy, err := c.requestPolicy()
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout+30*time.Second+policy.Delay())
	defer cancel()
	lease, err := policy.Admit(requestCtx)
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	req, err := http.NewRequestWithContext(requestCtx, method, u.String(), bytes.NewReader(payload))
	if err != nil {
		cancel()
		return nil, types.NetworkError("api: cannot build LinkedIn request", nil)
	}
	c.injectHeaders(req)
	if op == normalizedReadOperation {
		req.Header.Set("Accept", "application/vnd.linkedin.normalized+json+2.1")
	}
	if op == postOperation {
		req.Header.Set("Content-Type", "application/json")
	}
	if op == postOperation || op == deleteOperation {
		req.GetBody = nil
	}
	if c.verbose || c.debug {
		c.logf("→ %s LinkedIn API", method)
	}
	resp, err := c.http.Do(req)

	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return nil, requestFailure(op, 0, types.ExitNetwork, types.ReasonTransport)
	}
	refusal := lease.Refusal(resp.StatusCode, resp.StatusCode >= 300 && resp.StatusCode < 400 && authRedirect(u, resp.Header.Get("Location")), resp.Header.Get("Retry-After"))
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, safety.MaxBody+1))
	closeErr := resp.Body.Close()
	if c.verbose || c.debug {
		c.logf("← %d LinkedIn API", resp.StatusCode)
	}
	if refusal != nil {
		return nil, types.WithRequestDetails(refusal, method, op.name(), resp.StatusCode, types.ReasonRefusal)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, requestFailure(op, resp.StatusCode, types.ExitNetwork, types.ReasonRedirect)
	}
	if readErr != nil || closeErr != nil || requestCtx.Err() != nil {
		return nil, requestFailure(op, resp.StatusCode, types.ExitNetwork, types.ReasonResponseRead)
	}
	if len(body) > safety.MaxBody {
		return nil, requestFailure(op, resp.StatusCode, types.ExitNetwork, types.ReasonResponseSize)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

func (c *Client) readJSON(ctx context.Context, op operation, target string) (json.RawMessage, error) {

	resp, err := c.send(ctx, op, target, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, requestFailure(op, resp.StatusCode, types.ExitGeneral, types.ReasonHTTPStatus)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, requestFailure(op, resp.StatusCode, types.ExitNetwork, types.ReasonResponseRead)
	}
	if err = validateJSON(body); err != nil {
		return nil, types.WithRequestDetails(err, op.method(), op.name(), resp.StatusCode, types.ReasonUnspecified)
	}
	return json.RawMessage(body), nil
}

func validateJSON(body []byte) error {
	if !json.Valid(body) {
		return &types.LnkError{Code: types.ExitGeneral, Message: "api: invalid JSON response", Reason: types.ReasonJSON}
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil || envelope == nil {
		return &types.LnkError{Code: types.ExitGeneral, Message: "api: invalid JSON response envelope", Reason: types.ReasonJSON}
	}
	if raw, exists := envelope["errors"]; exists && string(raw) != "null" {
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) != nil || len(entries) > 0 {
			return &types.LnkError{Code: types.ExitGeneral, Message: "api: GraphQL response contains errors", Reason: types.ReasonGraphQL}
		}
	}
	return nil
}

func (c *Client) mutateJSON(ctx context.Context, op operation, target string, payload interface{}, strictDelete bool) (json.RawMessage, error) {
	var data []byte
	if op == postOperation {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return nil, errors.New("api: cannot marshal payload")
		}
	}
	resp, err := c.send(ctx, op, target, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, types.RateLimitError()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (strictDelete && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent) {
		return nil, requestFailure(op, resp.StatusCode, types.ExitGeneral, types.ReasonHTTPStatus)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, requestFailure(op, resp.StatusCode, types.ExitNetwork, types.ReasonResponseRead)
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	if err := validateJSON(body); err != nil {
		return nil, types.WithRequestDetails(err, op.method(), op.name(), resp.StatusCode, types.ReasonUnspecified)
	}
	return json.RawMessage(body), nil
}
