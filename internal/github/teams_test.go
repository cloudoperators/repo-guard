// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v88/github"
)

func TestTeamsProvider_AddUser(t *testing.T) {
	cases := []struct {
		name            string
		statusCode      int
		wantFound       bool
		wantErr         bool
		wantErrContains string
	}{
		{
			name:       "success — 200 returns found=true, no error",
			statusCode: http.StatusOK,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:       "success — 201 (membership created) returns found=true, no error",
			statusCode: http.StatusCreated,
			wantFound:  true,
			wantErr:    false,
		},
		{
			name:            "404 — user not found returns found=false",
			statusCode:      http.StatusNotFound,
			wantFound:       false,
			wantErr:         true,
			wantErrContains: "user not found",
		},
		{
			name:            "422 — suspended user returns found=false",
			statusCode:      http.StatusUnprocessableEntity,
			wantFound:       false,
			wantErr:         true,
			wantErrContains: "suspended",
		},
		{
			name:            "500 — server error returns found=true with error",
			statusCode:      http.StatusInternalServerError,
			wantFound:       true,
			wantErr:         true,
			wantErrContains: "500",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, mux := newTestTeamsProvider(t)

			mux.HandleFunc("/api/v3/orgs/test-org/teams/my-team/memberships/alice",
				func(w http.ResponseWriter, r *http.Request) {
					if tc.statusCode == http.StatusOK || tc.statusCode == http.StatusCreated {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(tc.statusCode)
						_ = json.NewEncoder(w).Encode(map[string]any{"state": "pending", "role": "member"})
					} else {
						http.Error(w, http.StatusText(tc.statusCode), tc.statusCode)
					}
				})

			found, err := provider.AddUser(t.Context(), "my-team", "alice")

			if found != tc.wantFound {
				t.Errorf("found: got %v, want %v", found, tc.wantFound)
			}
			if tc.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.wantErrContains != "" && err != nil {
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrContains)
				}
			}
		})
	}
}

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
		cache:        &etagCache{entries: make(map[string]etagEntry)},
	}
	return provider, mux
}

func TestTeamsProvider_List_EnterpriseFilter(t *testing.T) {
	cases := []struct {
		name      string
		fixtures  []map[string]any
		wantTeams []string
	}{
		{
			name: "enterprise team is excluded",
			fixtures: []map[string]any{
				{"id": 1, "name": "org-team", "slug": "org-team", "type": "organization"},
				{"id": 2, "name": "enterprise-team", "slug": "enterprise-team", "type": "enterprise"},
			},
			wantTeams: []string{"org-team"},
		},
		{
			name: "team without type field defaults to included",
			fixtures: []map[string]any{
				{"id": 1, "name": "regular-team", "slug": "regular-team"},
			},
			wantTeams: []string{"regular-team"},
		},
		{
			name: "multiple enterprise teams are all excluded",
			fixtures: []map[string]any{
				{"id": 1, "name": "org-team-a", "slug": "org-team-a", "type": "organization"},
				{"id": 2, "name": "ent-team-1", "slug": "ent-team-1", "type": "enterprise"},
				{"id": 3, "name": "org-team-b", "slug": "org-team-b", "type": "organization"},
				{"id": 4, "name": "ent-team-2", "slug": "ent-team-2", "type": "enterprise"},
			},
			wantTeams: []string{"org-team-a", "org-team-b"},
		},
		{
			name:      "empty team list",
			fixtures:  []map[string]any{},
			wantTeams: []string{},
		},
		{
			name: "all teams are enterprise — result is empty",
			fixtures: []map[string]any{
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
