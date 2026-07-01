// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"strings"

	githubv4 "github.com/shurcooL/githubv4"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

// orgReposWithTeamsQuery is the GraphQL query struct for fetching all organisation
// repositories and their associated team permissions in a single paginated request.
type orgReposWithTeamsQuery struct {
	Organization struct {
		Repositories struct {
			PageInfo struct {
				HasNextPage githubv4.Boolean
				EndCursor   githubv4.String
			}
			Nodes []struct {
				Name       githubv4.String
				Visibility githubv4.String
				IsArchived githubv4.Boolean
				IsDisabled githubv4.Boolean
				Teams      struct {
					PageInfo struct {
						HasNextPage githubv4.Boolean
						EndCursor   githubv4.String
					}
					Edges []struct {
						Permission githubv4.String
						Node       struct{ Slug githubv4.String }
					}
				} `graphql:"teams(first: 100, after: $teamCursor)"`
			}
		} `graphql:"repositories(first: 100, after: $repoCursor, orderBy: {field: NAME, direction: ASC})"`
	} `graphql:"organization(login: $org)"`
}

// graphqlPermissionToTeamPermission converts a GraphQL RepositoryPermission enum
// value to the internal GithubTeamPermission type.
// GitHub GraphQL uses: ADMIN, MAINTAIN, WRITE, TRIAGE, READ.
// The REST API and our internal type use: admin, maintain, push, triage, pull.
func graphqlPermissionToTeamPermission(p string) repoguardsapv1.GithubTeamPermission {
	switch strings.ToUpper(p) {
	case "ADMIN":
		return "admin"
	case "MAINTAIN":
		return "maintain"
	case "WRITE":
		return "push" // GraphQL WRITE maps to REST push
	case "TRIAGE":
		return "triage"
	case "READ":
		return "pull"
	default:
		return repoguardsapv1.GithubTeamPermission(strings.ToLower(p))
	}
}

// ExtendedListGraphQL fetches all organisation repositories and their team permissions
// using a single paginated GraphQL query instead of N+1 REST calls.
// For 300 repositories this reduces ~303 REST calls to 3–4 GraphQL calls per reconcile.
//
// Repos that are archived or disabled are excluded (same semantics as List/ExtendedList).
// If a repository has more than 100 teams (extremely rare), the method falls back to a
// REST call for that specific repository only.
func (t *DefaultRepositoryProvider) ExtendedListGraphQL(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error) {
	publicRepos := make([]repoguardsapv1.GithubRepository, 0)
	privateRepos := make([]repoguardsapv1.GithubRepository, 0)
	internalRepos := make([]repoguardsapv1.GithubRepository, 0)

	var repoCursor *githubv4.String // nil = first page

	for {
		var query orgReposWithTeamsQuery
		vars := map[string]any{
			"org":        githubv4.String(t.organization),
			"repoCursor": repoCursor,
			"teamCursor": (*githubv4.String)(nil),
		}

		if err := t.graphqlClient.Query(ctx, &query, vars); err != nil {
			ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "error").Inc()
			return nil, nil, nil, err
		}
		ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "success").Inc()

		for _, node := range query.Organization.Repositories.Nodes {
			if bool(node.IsArchived) || bool(node.IsDisabled) {
				continue
			}
			name := string(node.Name)
			if name == "" {
				continue
			}

			var teams []repoguardsapv1.GithubTeamWithPermission
			if bool(node.Teams.PageInfo.HasNextPage) {
				// Rare edge case: repo has >100 teams. Fall back to REST for this repo.
				var err error
				teams, err = t.RepositoryTeams(ctx, name)
				if err != nil {
					return nil, nil, nil, err
				}
			} else {
				teams = make([]repoguardsapv1.GithubTeamWithPermission, 0, len(node.Teams.Edges))
				for _, edge := range node.Teams.Edges {
					slug := string(edge.Node.Slug)
					if slug == "" {
						continue
					}
					perm := graphqlPermissionToTeamPermission(string(edge.Permission))
					teams = append(teams, repoguardsapv1.GithubTeamWithPermission{
						Team:       slug,
						Permission: perm,
					})
				}
			}

			repo := repoguardsapv1.GithubRepository{Name: name, Teams: teams}
			switch strings.ToLower(string(node.Visibility)) {
			case "public":
				publicRepos = append(publicRepos, repo)
			case "private":
				privateRepos = append(privateRepos, repo)
			case "internal":
				internalRepos = append(internalRepos, repo)
			default:
				// Skip repos with unknown visibility (same policy as List()).
			}
		}

		if !bool(query.Organization.Repositories.PageInfo.HasNextPage) {
			break
		}
		cursor := query.Organization.Repositories.PageInfo.EndCursor
		repoCursor = &cursor
	}

	return publicRepos, privateRepos, internalRepos, nil
}
