package api

import (
	"context"
	"encoding/json"
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", types.NetworkError("reading LinkedIn ping failed", nil)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", types.AuthError("session expired or credentials invalid")
	}
	if resp.StatusCode != http.StatusOK {
		return "", requestFailure(readOperation, resp.StatusCode, types.ExitGeneral, types.ReasonHTTPStatus)
	}
	if err := validateJSON(body); err != nil {
		return "", types.WithRequestDetails(err, http.MethodGet, readOperation.name(), resp.StatusCode, types.ReasonUnspecified)
	}
	return "connected", nil
}
