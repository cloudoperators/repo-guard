// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gogithub "github.com/google/go-github/v88/github"
)

// newTestRepositoryProvider creates a DefaultRepositoryProvider backed by a
// fake HTTP server.  The caller receives both the provider and the mux to
// register route handlers before calling provider methods.
func newTestRepositoryProvider(t *testing.T) (*DefaultRepositoryProvider, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := gogithub.NewClient(
		gogithub.WithHTTPClient(srv.Client()),
		gogithub.WithAuthToken("test-token"),
		gogithub.WithEnterpriseURLs(srv.URL+"/api/v3/", srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("create github client: %v", err)
	}

	provider := &DefaultRepositoryProvider{
		repositoryService: *client.Repositories,
		teamsService:      *client.Teams,
		organization:      "test-org",
		cache:             &etagCache{entries: make(map[string]etagEntry)},
	}
	return provider, mux
}

func TestRepositoryProvider_List_VisibilityBuckets(t *testing.T) {
	cases := []struct {
		name         string
		fixtures     []map[string]any
		wantPublic   []string
		wantPrivate  []string
		wantInternal []string
	}{
		{
			name: "three-way split by visibility field",
			fixtures: []map[string]any{
				{"name": "pub-repo", "private": false, "visibility": "public"},
				{"name": "priv-repo", "private": true, "visibility": "private"},
				{"name": "int-repo", "private": true, "visibility": "internal"},
			},
			wantPublic:   []string{"pub-repo"},
			wantPrivate:  []string{"priv-repo"},
			wantInternal: []string{"int-repo"},
		},
		{
			name: "empty visibility falls back to private bool — private=true",
			fixtures: []map[string]any{
				{"name": "legacy-private", "private": true, "visibility": ""},
			},
			wantPublic:   []string{},
			wantPrivate:  []string{"legacy-private"},
			wantInternal: []string{},
		},
		{
			name: "empty visibility falls back to private bool — private=false",
			fixtures: []map[string]any{
				{"name": "legacy-public", "private": false, "visibility": ""},
			},
			wantPublic:   []string{"legacy-public"},
			wantPrivate:  []string{},
			wantInternal: []string{},
		},
		{
			name: "unknown visibility skipped — not promoted to any bucket",
			fixtures: []map[string]any{
				{"name": "mystery-repo", "private": false, "visibility": "unknown-future-value"},
			},
			wantPublic:   []string{},
			wantPrivate:  []string{},
			wantInternal: []string{},
		},
		{
			name: "archived repo is excluded from all buckets",
			fixtures: []map[string]any{
				{"name": "pub-repo", "private": false, "visibility": "public"},
				{"name": "archived-repo", "private": false, "visibility": "public", "archived": true},
			},
			wantPublic:   []string{"pub-repo"},
			wantPrivate:  []string{},
			wantInternal: []string{},
		},
		{
			name: "disabled repo is excluded from all buckets",
			fixtures: []map[string]any{
				{"name": "priv-repo", "private": true, "visibility": "private"},
				{"name": "disabled-repo", "private": true, "visibility": "private", "disabled": true},
			},
			wantPublic:   []string{},
			wantPrivate:  []string{"priv-repo"},
			wantInternal: []string{},
		},
		{
			name:         "empty repo list",
			fixtures:     []map[string]any{},
			wantPublic:   []string{},
			wantPrivate:  []string{},
			wantInternal: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, mux := newTestRepositoryProvider(t)

			mux.HandleFunc("/api/v3/orgs/test-org/repos", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tc.fixtures); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			})

			pub, priv, intern, err := provider.List(t.Context())
			if err != nil {
				t.Fatalf("List: unexpected error: %v", err)
			}

			assertStringSlice(t, "public", pub, tc.wantPublic)
			assertStringSlice(t, "private", priv, tc.wantPrivate)
			assertStringSlice(t, "internal", intern, tc.wantInternal)
		})
	}
}

func assertStringSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s repos: got %v, want %v", label, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s repos[%d]: got %q, want %q", label, i, got[i], want[i])
		}
	}
}
