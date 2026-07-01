// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gogithub "github.com/google/go-github/v88/github"
	githubv4 "github.com/shurcooL/githubv4"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
)

// graphqlResponse is the top-level structure returned by the mock GraphQL server.
type graphqlResponse struct {
	Data any `json:"data"`
}

// newTestRepositoryProviderWithGraphQL creates a DefaultRepositoryProvider backed
// by a fake HTTP server that serves both REST (under /api/v3/) and GraphQL (at /).
// The caller registers handlers on the returned mux.
func newTestRepositoryProviderWithGraphQL(t *testing.T, graphqlHandler http.HandlerFunc) *DefaultRepositoryProvider {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// REST client (go-github) hits /api/v3/
	restClient, err := gogithub.NewClient(
		gogithub.WithHTTPClient(srv.Client()),
		gogithub.WithAuthToken("test-token"),
		gogithub.WithEnterpriseURLs(srv.URL+"/api/v3/", srv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("create github client: %v", err)
	}

	// GraphQL client (shurcooL/githubv4) posts to / (the root path of the server).
	gqlClient := githubv4.NewEnterpriseClient(srv.URL+"/", srv.Client())

	if graphqlHandler != nil {
		mux.HandleFunc("/", graphqlHandler)
	}

	return &DefaultRepositoryProvider{
		repositoryService: *restClient.Repositories,
		teamsService:      *restClient.Teams,
		organization:      "test-org",
		graphqlClient:     gqlClient,
	}
}

// buildTeamsGraphQLResponse builds the JSON body for a teams-with-repos GraphQL query.
func buildTeamsGraphQLResponse(teams []map[string]any, hasNextPage bool, endCursor string) string {
	data := map[string]any{
		"organization": map[string]any{
			"teams": map[string]any{
				"pageInfo": map[string]any{
					"hasNextPage": hasNextPage,
					"endCursor":   endCursor,
				},
				"nodes": teams,
			},
		},
	}
	b, _ := json.Marshal(graphqlResponse{Data: data})
	return string(b)
}

// buildOrgReposGraphQLResponse builds the JSON response body for a repos-only
// GraphQL query (no teams field).
func buildOrgReposGraphQLResponse(nodes []map[string]any, hasNextPage bool, endCursor string) string {
	data := map[string]any{
		"organization": map[string]any{
			"repositories": map[string]any{
				"pageInfo": map[string]any{
					"hasNextPage": hasNextPage,
					"endCursor":   endCursor,
				},
				"nodes": nodes,
			},
		},
	}
	b, _ := json.Marshal(graphqlResponse{Data: data})
	return string(b)
}

// buildRepoMetaNode builds a repository metadata node (no teams) for fixtures.
func buildRepoMetaNode(name, visibility string, archived, disabled bool) map[string]any {
	return map[string]any{
		"name":       name,
		"visibility": visibility,
		"isArchived": archived,
		"isDisabled": disabled,
	}
}

// buildTeamNode builds a team node with repository edges for GraphQL fixtures.
// endCursor should be a non-empty string when reposHasNextPage is true so that
// fetchRemainingTeamRepos receives a real cursor and pagination is exercised correctly.
func buildTeamNode(slug string, repoEdges []map[string]any, reposHasNextPage bool, endCursor string) map[string]any {
	return map[string]any{
		"slug": slug,
		"repositories": map[string]any{
			"pageInfo": map[string]any{
				"hasNextPage": reposHasNextPage,
				"endCursor":   endCursor,
			},
			"edges": repoEdges,
		},
	}
}

// buildTeamRepoEdge builds a team→repo edge (permission + repo name) for GraphQL fixtures.
func buildTeamRepoEdge(repoName, permission string) map[string]any {
	return map[string]any{
		"permission": permission,
		"node":       map[string]any{"name": repoName},
	}
}

func TestGraphqlPermissionToTeamPermission(t *testing.T) {
	cases := []struct {
		input string
		want  repoguardsapv1.GithubTeamPermission
	}{
		{"ADMIN", "admin"},
		{"MAINTAIN", "maintain"},
		{"WRITE", "push"}, // critical mapping
		{"TRIAGE", "triage"},
		{"READ", "pull"},
		{"write", "push"},      // lowercase
		{"admin", "admin"},     // lowercase passthrough via default
		{"UNKNOWN", "unknown"}, // unknown values lower-cased
	}
	for _, tc := range cases {
		got := graphqlPermissionToTeamPermission(tc.input)
		if got != tc.want {
			t.Errorf("graphqlPermissionToTeamPermission(%q): got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtendedListGraphQL_Basic(t *testing.T) {
	// Teams query: devs→pub-repo(WRITE), ops→priv-repo(ADMIN), security→int-repo(READ).
	teamsBody := buildTeamsGraphQLResponse([]map[string]any{
		buildTeamNode("devs", []map[string]any{buildTeamRepoEdge("pub-repo", "WRITE")}, false, ""),
		buildTeamNode("ops", []map[string]any{buildTeamRepoEdge("priv-repo", "ADMIN")}, false, ""),
		buildTeamNode("security", []map[string]any{buildTeamRepoEdge("int-repo", "READ")}, false, ""),
	}, false, "")

	// Repos query: three repos across all visibility buckets.
	reposBody := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("pub-repo", "PUBLIC", false, false),
		buildRepoMetaNode("priv-repo", "PRIVATE", false, false),
		buildRepoMetaNode("int-repo", "INTERNAL", false, false),
	}, false, "")

	var callCount int32
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(teamsBody)) //nolint:errcheck
		} else {
			w.Write([]byte(reposBody)) //nolint:errcheck
		}
	})

	pub, priv, internal, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}

	if len(pub) != 1 || pub[0].Name != "pub-repo" {
		t.Errorf("public repos: got %v", pub)
	}
	if len(priv) != 1 || priv[0].Name != "priv-repo" {
		t.Errorf("private repos: got %v", priv)
	}
	if len(internal) != 1 || internal[0].Name != "int-repo" {
		t.Errorf("internal repos: got %v", internal)
	}

	// Verify permission mapping: WRITE → push
	if len(pub[0].Teams) != 1 || pub[0].Teams[0].Permission != "push" {
		t.Errorf("pub-repo teams: got %v, want push permission", pub[0].Teams)
	}
	if len(priv[0].Teams) != 1 || priv[0].Teams[0].Permission != "admin" {
		t.Errorf("priv-repo teams: got %v, want admin permission", priv[0].Teams)
	}
	if len(internal[0].Teams) != 1 || internal[0].Teams[0].Permission != "pull" {
		t.Errorf("int-repo teams: got %v, want pull permission", internal[0].Teams)
	}
}

func TestExtendedListGraphQL_Pagination(t *testing.T) {
	// Teams query returns single page (no teams for simplicity).
	teamsBody := buildTeamsGraphQLResponse(nil, false, "")

	// Repos query: two pages.
	page1Body := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("repo-a", "PUBLIC", false, false),
	}, true, "cursor-abc")
	page2Body := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("repo-b", "PRIVATE", false, false),
	}, false, "")

	var callCount int32
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1: // teams query
			w.Write([]byte(teamsBody)) //nolint:errcheck
		case 2: // repos page 1
			w.Write([]byte(page1Body)) //nolint:errcheck
		default: // repos page 2+
			w.Write([]byte(page2Body)) //nolint:errcheck
		}
	})

	pub, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected 3 GraphQL calls (1 teams + 2 repos pages), got %d", callCount)
	}
	if len(pub) != 1 || pub[0].Name != "repo-a" {
		t.Errorf("public repos: got %v", pub)
	}
	if len(priv) != 1 || priv[0].Name != "repo-b" {
		t.Errorf("private repos: got %v", priv)
	}
}

func TestExtendedListGraphQL_ArchivedFiltered(t *testing.T) {
	// Teams query: no teams.
	teamsBody := buildTeamsGraphQLResponse(nil, false, "")

	// Repos query: archived and disabled repos must be excluded.
	reposBody := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("active-repo", "PUBLIC", false, false),
		buildRepoMetaNode("archived-repo", "PUBLIC", true, false),
		buildRepoMetaNode("disabled-repo", "PRIVATE", false, true),
	}, false, "")

	var callCount int32
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			w.Write([]byte(teamsBody)) //nolint:errcheck
		} else {
			w.Write([]byte(reposBody)) //nolint:errcheck
		}
	})

	pub, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}
	if len(pub) != 1 || pub[0].Name != "active-repo" {
		t.Errorf("expected only active-repo in public, got %v", pub)
	}
	if len(priv) != 0 {
		t.Errorf("expected no private repos, got %v", priv)
	}
}

func TestExtendedListGraphQL_TeamsPagination(t *testing.T) {
	// Teams query: two pages of teams. Page 1 has team-a, page 2 has team-b.
	teamsPage1 := buildTeamsGraphQLResponse([]map[string]any{
		buildTeamNode("team-a", []map[string]any{buildTeamRepoEdge("repo-x", "WRITE")}, false, ""),
	}, true, "team-cursor-1")
	teamsPage2 := buildTeamsGraphQLResponse([]map[string]any{
		buildTeamNode("team-b", []map[string]any{buildTeamRepoEdge("repo-x", "READ")}, false, ""),
	}, false, "")

	// Repos query: single repo.
	reposBody := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("repo-x", "PRIVATE", false, false),
	}, false, "")

	var callCount int32
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1:
			w.Write([]byte(teamsPage1)) //nolint:errcheck
		case 2:
			w.Write([]byte(teamsPage2)) //nolint:errcheck
		default:
			w.Write([]byte(reposBody)) //nolint:errcheck
		}
	})

	_, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}
	if len(priv) != 1 {
		t.Fatalf("expected 1 private repo, got %v", priv)
	}
	// repo-x should have both team-a (push) and team-b (pull) permissions.
	if len(priv[0].Teams) != 2 {
		t.Errorf("expected 2 teams on repo-x, got %v", priv[0].Teams)
	}
	// Verify exact slug names and permission mappings (order-independent).
	bySlug := map[string]repoguardsapv1.GithubTeamPermission{}
	for _, tp := range priv[0].Teams {
		bySlug[tp.Team] = tp.Permission
	}
	if bySlug["team-a"] != "push" {
		t.Errorf("team-a: expected push, got %q", bySlug["team-a"])
	}
	if bySlug["team-b"] != "pull" {
		t.Errorf("team-b: expected pull, got %q", bySlug["team-b"])
	}
}

// buildTeamReposGraphQLResponse builds the JSON body for a single-team repos query
// (teamReposQuery struct) used in the >100-repos overflow fallback.
func buildTeamReposGraphQLResponse(repoEdges []map[string]any, hasNextPage bool, endCursor string) string {
	data := map[string]any{
		"organization": map[string]any{
			"team": map[string]any{
				"repositories": map[string]any{
					"pageInfo": map[string]any{
						"hasNextPage": hasNextPage,
						"endCursor":   endCursor,
					},
					"edges": repoEdges,
				},
			},
		},
	}
	b, _ := json.Marshal(graphqlResponse{Data: data})
	return string(b)
}

func TestExtendedListGraphQL_TeamRepoOverflow(t *testing.T) {
	// Team "big-team" has reposHasNextPage=true in the first teams query page,
	// triggering a dedicated teamReposQuery for the remaining repos.
	// A real endCursor is provided so fetchRemainingTeamRepos receives it correctly.
	teamsBody := buildTeamsGraphQLResponse([]map[string]any{
		buildTeamNode("big-team", []map[string]any{
			buildTeamRepoEdge("repo-1", "WRITE"),
		}, true, "repo-cursor-overflow"), // HasNextPage=true + real cursor triggers fetchRemainingTeamRepos
	}, false, "")

	// The overflow query returns the second page of repos for big-team.
	overflowBody := buildTeamReposGraphQLResponse([]map[string]any{
		buildTeamRepoEdge("repo-2", "ADMIN"),
	}, false, "")

	// Repos metadata query.
	reposBody := buildOrgReposGraphQLResponse([]map[string]any{
		buildRepoMetaNode("repo-1", "PRIVATE", false, false),
		buildRepoMetaNode("repo-2", "PRIVATE", false, false),
	}, false, "")

	var callCount int32
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		n := atomic.AddInt32(&callCount, 1)
		switch n {
		case 1: // teams query
			w.Write([]byte(teamsBody)) //nolint:errcheck
		case 2: // overflow single-team query
			w.Write([]byte(overflowBody)) //nolint:errcheck
		default: // repos metadata query
			w.Write([]byte(reposBody)) //nolint:errcheck
		}
	})

	_, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}
	if atomic.LoadInt32(&callCount) != 3 {
		t.Errorf("expected 3 GraphQL calls (teams + overflow + repos), got %d", callCount)
	}
	if len(priv) != 2 {
		t.Fatalf("expected 2 private repos, got %v", priv)
	}
	// Both repos should be associated with big-team.
	for _, repo := range priv {
		if len(repo.Teams) != 1 || repo.Teams[0].Team != "big-team" {
			t.Errorf("repo %q: expected big-team, got %v", repo.Name, repo.Teams)
		}
	}
	// Assert per-repo permissions: repo-1 from first page (WRITE→push), repo-2 from overflow (ADMIN→admin).
	wantPerm := map[string]repoguardsapv1.GithubTeamPermission{
		"repo-1": "push",
		"repo-2": "admin",
	}
	for _, repo := range priv {
		got := repo.Teams[0].Permission
		if want := wantPerm[repo.Name]; got != want {
			t.Errorf("repo %q: expected permission %q, got %q", repo.Name, want, got)
		}
	}
}
