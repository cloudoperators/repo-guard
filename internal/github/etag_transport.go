// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"net/http"
	"strings"
)

// etagTransport is an http.RoundTripper that injects If-None-Match headers on
// outbound GET requests and stores new ETags from 200 responses.
// The actual cached value (parsed response body) is stored by the provider layer.
type etagTransport struct {
	wrapped http.RoundTripper
	cache   *etagCache
	keyFn   func(*http.Request) string
}

// RoundTrip implements http.RoundTripper.
// For GET requests:
//   - If a cached ETag exists for the key derived from the request, adds If-None-Match.
//   - On 200 responses, stores the new ETag so the next request can use it.
//   - On 304 responses, passes through to the caller; the provider layer handles value retrieval.
func (t *etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return t.wrapped.RoundTrip(req)
	}
	key := t.keyFn(req)
	if etag, ok := t.cache.getEtag(key); ok {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusOK {
		if etag := resp.Header.Get("ETag"); etag != "" {
			// Update the ETag while preserving any previously cached parsed value.
			// The provider layer will overwrite the value on 200 via cache.set().
			t.cache.setEtagOnly(key, etag)
		}
	}
	return resp, nil
}

// urlCacheKey returns a stable cache key derived from the request URL path and raw query.
// It strips the GitHub Enterprise REST prefix ("/api/v3") so that cache keys are
// consistent with provider-side keys (e.g. "/orgs/...") regardless of whether the
// target is github.com or a GHE instance.
// This is used as the default keyFn for etagTransport.
func urlCacheKey(req *http.Request) string {
	path := req.URL.Path
	// Normalize GHE paths: strip leading "/api/v3" prefix so cache keys match
	// provider-constructed keys which always start with "/orgs/", "/repos/", etc.
	if after, ok := strings.CutPrefix(path, "/api/v3"); ok {
		path = after
	}
	if req.URL.RawQuery != "" {
		return path + "?" + req.URL.RawQuery
	}
	return path
}
