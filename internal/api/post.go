package api

import (
	"context"
	"encoding/json"
)

func (c *Client) PostJSON(path string, queryParams map[string]string, body interface{}) (json.RawMessage, error) {
	u, err := safeDestination(baseURL + path)
	if err != nil {
		return nil, err
	}
	if len(queryParams) > 0 {
		params := u.Query()
		for k, v := range queryParams {
			params.Set(k, v)
		}
		u.RawQuery = params.Encode()
	}
	return c.mutateJSON(context.Background(), postOperation, u.String(), body, false)
}

func (c *Client) DeleteResource(path string) error {
	_, err := c.mutateJSON(context.Background(), deleteOperation, baseURL+path, nil, false)
	return err
}
