// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package generic_http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	externalprovider "github.com/cloudoperators/repo-guard/internal/external-provider"
)

type HTTPConfig struct {
	// If set, read users from this array field instead of expecting a []string response
	ResultsField string
	// If ResultsField is set and elements are objects, take this field as the user id
	IDField string
	// Enable pagination across pages 1..TotalPages
	Paginated bool
	// Name of the field that contains the total number of pages
	TotalPagesField string
	// Name of the query parameter carrying the page number
	PageParam string
	// URL to test connection with
	TestConnectionURL string
}

type HTTPClient struct {
	Endpoint   string
	Username   string
	Password   string
	Token      string
	Cfg        HTTPConfig
	HTTPClient *http.Client
}

func NewHTTPClient(endpoint, username, password, token string, cfg *HTTPConfig) externalprovider.ExternalProvider {
	c := HTTPConfig{}
	if cfg != nil {
		c = *cfg
	}
	return &HTTPClient{
		Endpoint: endpoint,
		Username: username,
		Password: password,
		Token:    token,
		Cfg:      c,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *HTTPClient) buildURL(group string) string {
	if strings.Contains(c.Endpoint, "{group}") {
		return strings.ReplaceAll(c.Endpoint, "{group}", group)
	}
	sep := "?"
	if strings.Contains(c.Endpoint, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%sgroup=%s", c.Endpoint, sep, group)
}

func (c *HTTPClient) Users(ctx context.Context, group string) ([]string, error) {
	if c.Cfg.Paginated {
		return c.usersPaginated(ctx, group)
	}
	url := c.buildURL(group)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("non-200 status code received: %d", resp.StatusCode)
	}
	if c.Cfg.ResultsField == "" {
		var users []string
		if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
			return nil, err
		}
		return users, nil
	}
	// structured response: extract from ResultsField
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	arrVal, ok := payload[c.Cfg.ResultsField]
	if !ok {
		// return empty rather than error to be tolerant
		return []string{}, nil
	}
	return extractIDsFromArray(arrVal, c.idField())
}

func (c *HTTPClient) idField() string {
	if c.Cfg.IDField != "" {
		return c.Cfg.IDField
	}
	return "id"
}

func extractIDsFromArray(arr any, idField string) ([]string, error) {
	ids := []string{}
	switch v := arr.(type) {
	case []any:
		for _, elem := range v {
			switch e := elem.(type) {
			case string:
				ids = append(ids, e)
			case map[string]any:
				if idRaw, ok := e[idField]; ok {
					if s, ok := idRaw.(string); ok {
						ids = append(ids, s)
					}
				}
			}
		}
	default:
		// not an array; return empty
	}
	return ids, nil
}

func (c *HTTPClient) usersPaginated(ctx context.Context, group string) ([]string, error) {
	pageParam := c.Cfg.PageParam
	if pageParam == "" {
		pageParam = "page"
	}
	totalPagesField := c.Cfg.TotalPagesField
	if totalPagesField == "" {
		totalPagesField = "total_pages"
	}
	users := []string{}
	// first page to get total pages
	page := 1
	for {
		url := c.buildURLWithPage(group, pageParam, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if c.Username != "" || c.Password != "" {
			req.SetBasicAuth(c.Username, c.Password)
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}
		// Avoid deferring Close() inside a loop to prevent accumulating open bodies
		if resp.StatusCode != http.StatusOK {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, fmt.Errorf("non-200 status code received: %d", resp.StatusCode)
		}
		var payload map[string]any
		decErr := json.NewDecoder(resp.Body).Decode(&payload)
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		if decErr != nil {
			return nil, decErr
		}
		arrVal := any(nil)
		if c.Cfg.ResultsField != "" {
			arrVal = payload[c.Cfg.ResultsField]
		} else {
			// if empty, try the whole payload as array
			if rawArr, ok := any(payload).([]any); ok {
				arrVal = rawArr
			}
		}
		pageUsers, err := extractIDsFromArray(arrVal, c.idField())
		if err != nil {
			return nil, err
		}
		users = append(users, pageUsers...)
		// figure total pages
		tpRaw, ok := payload[totalPagesField]
		if !ok {
			break // no pagination info, stop after first page
		}
		tp := 1
		switch n := tpRaw.(type) {
		case float64:
			tp = int(n)
		case int:
			tp = n
		}
		if page >= tp {
			break
		}
		page++
	}
	return users, nil
}

func (c *HTTPClient) buildURLWithPage(group, pageParam string, page int) string {
	url := c.buildURL(group)
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s%s=%d", url, sep, pageParam, page)
}

func (c *HTTPClient) TestConnection(ctx context.Context) error {
	if c.Cfg.TestConnectionURL != "" {
		// Perform a lightweight request to verify credentials without requiring a valid group.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Cfg.TestConnectionURL, nil)
		if err != nil {
			return err
		}
		if c.Username != "" || c.Password != "" {
			req.SetBasicAuth(c.Username, c.Password)
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer func() {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
		}()
		// Treat 401/403 as authentication/authorization failures. Any other status (e.g., 200 or 404)
		// is acceptable for connection validation because the dummy group may not exist.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("authentication failed: status %d", resp.StatusCode)
		}
		return nil

	}
	return nil
}
