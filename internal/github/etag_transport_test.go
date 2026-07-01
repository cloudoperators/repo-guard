// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEtagTransport_InjectsIfNoneMatch(t *testing.T) {
	cache := &etagCache{entries: make(map[string]etagEntry)}
	cache.set("/repos", `"etag-v1"`, nil)

	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &etagTransport{
		wrapped: http.DefaultTransport,
		cache:   cache,
		keyFn:   urlCacheKey,
	}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/repos", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() }) //nolint:errcheck
	_, _ = io.Copy(io.Discard, resp.Body)

	if receivedHeader != `"etag-v1"` {
		t.Errorf("If-None-Match: got %q, want %q", receivedHeader, `"etag-v1"`)
	}
}

func TestEtagTransport_Stores200Etag(t *testing.T) {
	cache := &etagCache{entries: make(map[string]etagEntry)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"new-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := &etagTransport{
		wrapped: http.DefaultTransport,
		cache:   cache,
		keyFn:   urlCacheKey,
	}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/resource", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() }) //nolint:errcheck
	_, _ = io.Copy(io.Discard, resp.Body)

	etag, ok := cache.getEtag("/resource")
	if !ok {
		t.Fatal("expected ETag to be stored after 200 response")
	}
	if etag != `"new-etag"` {
		t.Errorf("stored etag: got %q, want %q", etag, `"new-etag"`)
	}
}

func TestEtagTransport_SkipsNonGet(t *testing.T) {
	cache := &etagCache{entries: make(map[string]etagEntry)}
	cache.set("/resource", `"should-not-inject"`, nil)

	var receivedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("If-None-Match")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	transport := &etagTransport{
		wrapped: http.DefaultTransport,
		cache:   cache,
		keyFn:   urlCacheKey,
	}
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/resource", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() }) //nolint:errcheck

	if receivedHeader != "" {
		t.Errorf("expected no If-None-Match for POST, got %q", receivedHeader)
	}
}

func TestUrlCacheKey(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"http://example.com/orgs/myorg/members", "/orgs/myorg/members"},
		{"http://example.com/orgs/myorg/members?role=admin&per_page=100", "/orgs/myorg/members?role=admin&per_page=100"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
		got := urlCacheKey(req)
		if got != tc.want {
			t.Errorf("urlCacheKey(%q): got %q, want %q", tc.url, got, tc.want)
		}
	}
}
