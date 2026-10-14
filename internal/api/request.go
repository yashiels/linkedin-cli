package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

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
	p := u.EscapedPath()
	for range 5 {
		decoded, err := url.PathUnescape(p)
		if err != nil {
			return false
		}
		if decoded == p {
			break
		}
		p = decoded
	}
	p = path.Clean(p)
	for _, prefix := range []string{"/login", "/uas/login", "/authwall", "/checkpoint/challenge"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

type responseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *responseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
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
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
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
		cancel()
		var timed net.Error
		if errors.As(err, &timed) && timed.Timeout() {
			return nil, types.NetworkError("LinkedIn request timed out", context.DeadlineExceeded)
		}
		return nil, types.NetworkError("LinkedIn request failed", nil)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		cancel()
		if authRedirect(u, resp.Header.Get("Location")) {
			return nil, types.AuthError("session expired — run 'lnk auth login' to refresh")
		}
		return nil, types.NetworkError("LinkedIn redirect rejected", nil)
	}
	resp.Body = &responseBody{ReadCloser: resp.Body, cancel: cancel}
	if c.verbose || c.debug {
		c.logf("← %d LinkedIn API", resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) readJSON(ctx context.Context, op operation, target string) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			wait := baseBackoff * time.Duration(1<<(attempt-1))
			c.logf("retrying LinkedIn read in %s (attempt %d/%d)", wait, attempt, maxRetries)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, types.NetworkError("LinkedIn read canceled", nil)
			case <-timer.C:
			}
		}
		resp, err := c.send(ctx, op, target, nil)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				lastErr = err
				continue
			}
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = types.RateLimitError()
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("api: unexpected status %d", resp.StatusCode)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = types.NetworkError("reading LinkedIn response failed", nil)
			if op == graphQLReadOperation {
				return nil, lastErr
			}
			continue
		}
		return json.RawMessage(body), nil
	}
	return nil, lastErr
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
		return nil, fmt.Errorf("api: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NetworkError("reading LinkedIn response failed", nil)
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	return json.RawMessage(body), nil
}
