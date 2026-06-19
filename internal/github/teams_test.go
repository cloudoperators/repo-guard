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

// newTestTeamsProvider creates a DefaultTeamsProvider backed by a fake HTTP server.
func newTestTeamsProvider(t *testing.T) (*DefaultTeamsProvider, *http.ServeMux) {
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

	provider := &DefaultTeamsProvider{
		service:      *client.Teams,
		organization: "test-org",
	}
	return provider, mux
}

func TestTeamsProvider_List_EnterpriseFilter(t *testing.T) {
	cases := []struct {
		name      string
		fixtures  []map[string]interface{}
		wantTeams []string
	}{
		{
			name: "enterprise team is excluded",
			fixtures: []map[string]interface{}{
				{"id": 1, "name": "org-team", "slug": "org-team", "type": "organization"},
				{"id": 2, "name": "enterprise-team", "slug": "enterprise-team", "type": "enterprise"},
			},
			wantTeams: []string{"org-team"},
		},
		{
			name: "team without type field defaults to included",
			fixtures: []map[string]interface{}{
				{"id": 1, "name": "regular-team", "slug": "regular-team"},
			},
			wantTeams: []string{"regular-team"},
		},
		{
			name: "multiple enterprise teams are all excluded",
			fixtures: []map[string]interface{}{
				{"id": 1, "name": "org-team-a", "slug": "org-team-a", "type": "organization"},
				{"id": 2, "name": "ent-team-1", "slug": "ent-team-1", "type": "enterprise"},
				{"id": 3, "name": "org-team-b", "slug": "org-team-b", "type": "organization"},
				{"id": 4, "name": "ent-team-2", "slug": "ent-team-2", "type": "enterprise"},
			},
			wantTeams: []string{"org-team-a", "org-team-b"},
		},
		{
			name:      "empty team list",
			fixtures:  []map[string]interface{}{},
			wantTeams: []string{},
		},
		{
			name: "all teams are enterprise — result is empty",
			fixtures: []map[string]interface{}{
				{"id": 1, "name": "ent-only", "slug": "ent-only", "type": "enterprise"},
			},
			wantTeams: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, mux := newTestTeamsProvider(t)

			mux.HandleFunc("/api/v3/orgs/test-org/teams", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(tc.fixtures); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			})

			got, err := provider.List(t.Context())
			if err != nil {
				t.Fatalf("List: unexpected error: %v", err)
			}

			if len(got) != len(tc.wantTeams) {
				t.Errorf("teams: got %v, want %v", got, tc.wantTeams)
				return
			}
			for i, want := range tc.wantTeams {
				if got[i] != want {
					t.Errorf("teams[%d]: got %q, want %q", i, got[i], want)
				}
			}
		})
	}
}
