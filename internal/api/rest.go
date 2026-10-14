package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func (c *Client) Get(path string, params url.Values) (json.RawMessage, error) {
	u, err := safeDestination(baseURL + path)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		u.RawQuery = params.Encode()
	}
	return c.readJSON(context.Background(), normalizedReadOperation, u.String())
}

func (c *Client) Post(path string, payload interface{}) (json.RawMessage, error) {
	return c.mutateJSON(context.Background(), postOperation, baseURL+path, payload, false)
}

func (c *Client) Delete(path string) error {
	_, err := c.mutateJSON(context.Background(), deleteOperation, baseURL+path, nil, true)
	return err
}

func (c *Client) Ping() (string, error) {
	resp, err := c.send(context.Background(), readOperation, baseURL+"/voyager/api/me", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", types.NetworkError("reading LinkedIn ping failed", nil)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", types.AuthError("session expired or credentials invalid")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("api: ping returned %d", resp.StatusCode)
	}
	if pop := resp.Header.Get("x-li-pop"); pop != "" {
		return pop, nil
	}
	if via := resp.Header.Get("via"); via != "" {
		return via, nil
	}
	return "unknown", nil
}
