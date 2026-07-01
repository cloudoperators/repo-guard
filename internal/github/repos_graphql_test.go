// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// buildOrgReposGraphQLResponse builds the JSON response body for a single-page
// GraphQL query returning the given repos.
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

// buildRepoNode builds a single repository node for a GraphQL fixture.
func buildRepoNode(name, visibility string, archived, disabled bool, teams []map[string]any, teamsHasNextPage bool) map[string]any {
	return map[string]any{
		"name":       name,
		"visibility": visibility,
		"isArchived": archived,
		"isDisabled": disabled,
		"teams": map[string]any{
			"pageInfo": map[string]any{
				"hasNextPage": teamsHasNextPage,
				"endCursor":   "",
			},
			"edges": teams,
		},
	}
}

// buildTeamEdge builds a single team edge (permission + slug) for a GraphQL fixture.
func buildTeamEdge(slug, permission string) map[string]any {
	return map[string]any{
		"permission": permission,
		"node":       map[string]any{"slug": slug},
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
	// One page, three repos across all visibility buckets.
	nodes := []map[string]any{
		buildRepoNode("pub-repo", "PUBLIC", false, false, []map[string]any{
			buildTeamEdge("devs", "WRITE"),
		}, false),
		buildRepoNode("priv-repo", "PRIVATE", false, false, []map[string]any{
			buildTeamEdge("ops", "ADMIN"),
		}, false),
		buildRepoNode("int-repo", "INTERNAL", false, false, []map[string]any{
			buildTeamEdge("security", "READ"),
		}, false),
	}
	respBody := buildOrgReposGraphQLResponse(nodes, false, "")

	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody)) //nolint:errcheck
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
	// Two pages: first returns has_next_page=true, second returns false.
	page1Nodes := []map[string]any{
		buildRepoNode("repo-a", "PUBLIC", false, false, nil, false),
	}
	page2Nodes := []map[string]any{
		buildRepoNode("repo-b", "PRIVATE", false, false, nil, false),
	}

	callCount := 0
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		var body string
		if callCount == 1 {
			body = buildOrgReposGraphQLResponse(page1Nodes, true, "cursor-abc")
		} else {
			body = buildOrgReposGraphQLResponse(page2Nodes, false, "")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body)) //nolint:errcheck
	})

	pub, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 GraphQL calls, got %d", callCount)
	}
	if len(pub) != 1 || pub[0].Name != "repo-a" {
		t.Errorf("public repos: got %v", pub)
	}
	if len(priv) != 1 || priv[0].Name != "repo-b" {
		t.Errorf("private repos: got %v", priv)
	}
}

func TestExtendedListGraphQL_ArchivedFiltered(t *testing.T) {
	// Archived and disabled repos must be excluded.
	nodes := []map[string]any{
		buildRepoNode("active-repo", "PUBLIC", false, false, nil, false),
		buildRepoNode("archived-repo", "PUBLIC", true, false, nil, false),
		buildRepoNode("disabled-repo", "PRIVATE", false, true, nil, false),
	}
	respBody := buildOrgReposGraphQLResponse(nodes, false, "")

	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody)) //nolint:errcheck
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

func TestExtendedListGraphQL_TeamOverflow(t *testing.T) {
	// A repo with teamsHasNextPage=true must fall back to the REST RepositoryTeams endpoint.
	nodes := []map[string]any{
		buildRepoNode("overflow-repo", "PRIVATE", false, false, []map[string]any{
			buildTeamEdge("team-gql", "READ"),
		}, true), // HasNextPage=true triggers REST fallback
	}
	respBody := buildOrgReposGraphQLResponse(nodes, false, "")

	restTeamsFixture := []map[string]any{
		{"id": 1, "slug": "team-rest", "name": "team-rest", "permission": "push"},
	}

	callCount := 0
	provider := newTestRepositoryProviderWithGraphQL(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(respBody)) //nolint:errcheck
	})

	// Register the REST fallback handler for RepositoryTeams.
	// We reconstruct the mux by creating a separate provider using the existing REST approach.
	// Since newTestRepositoryProviderWithGraphQL routes / to the graphqlHandler,
	// we need to also register REST handlers on the same mux.
	// Instead, create a separate REST server for the fallback.
	restMux := http.NewServeMux()
	restSrv := httptest.NewServer(restMux)
	t.Cleanup(restSrv.Close)

	restMux.HandleFunc("/api/v3/repos/test-org/overflow-repo/teams", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(restTeamsFixture) //nolint:errcheck
	})

	restClient, err := gogithub.NewClient(
		gogithub.WithHTTPClient(restSrv.Client()),
		gogithub.WithAuthToken("test-token"),
		gogithub.WithEnterpriseURLs(restSrv.URL+"/api/v3/", restSrv.URL+"/"),
	)
	if err != nil {
		t.Fatalf("create rest client: %v", err)
	}
	// Override the REST services on the provider.
	provider.repositoryService = *restClient.Repositories
	provider.teamsService = *restClient.Teams

	_, priv, _, err := provider.ExtendedListGraphQL(t.Context())
	if err != nil {
		t.Fatalf("ExtendedListGraphQL: unexpected error: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 REST fallback call, got %d", callCount)
	}
	if len(priv) != 1 {
		t.Fatalf("expected 1 private repo, got %v", priv)
	}
	// Teams should come from REST fallback, not GraphQL partial result.
	if len(priv[0].Teams) != 1 || priv[0].Teams[0].Team != "team-rest" {
		t.Errorf("expected REST-sourced team, got %v", priv[0].Teams)
	}
}
