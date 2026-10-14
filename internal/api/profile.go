package api

import (
	"context"
	"encoding/json"
	"net/url"
)

const (
	dashProfilesPath = "/voyager/api/identity/dash/profiles"
	dashDecoration   = "com.linkedin.voyager.dash.deco.identity.profile.FullProfileWithEntities-109"
)

func (c *Client) GetProfileByIdentity(vanityName string) (json.RawMessage, error) {
	params := url.Values{
		"q":              {"memberIdentity"},
		"memberIdentity": {vanityName},
		"decorationId":   {dashDecoration},
	}
	return c.readJSON(context.Background(), readOperation, baseURL+dashProfilesPath+"?"+params.Encode())
}
