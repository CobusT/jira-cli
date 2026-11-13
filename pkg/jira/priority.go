package jira

import (
	"context"
	"encoding/json"
	"net/http"
)

// IssuePriority is an issue priority.
type IssuePriority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Priorities fetches the issue priorities using the v2 version of the GET /priority endpoint.
func (c *Client) Priorities() ([]*IssuePriority, error) {
	res, err := c.GetV2(context.Background(), "/priority", nil)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, ErrEmptyResponse
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, formatUnexpectedResponse(res)
	}

	var out []*IssuePriority
	err = json.NewDecoder(res.Body).Decode(&out)
	return out, err
}
