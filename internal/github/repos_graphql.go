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

// orgTeamsWithReposQuery is the GraphQL query struct for fetching all organisation
// teams and their associated repository permissions. GitHub's GraphQL API does not
// expose a `teams` field on the Repository type; the traversal must go the other
// way: organisation → teams → repositories.
type orgTeamsWithReposQuery struct {
	Organization struct {
		Teams struct {
			PageInfo struct {
				HasNextPage githubv4.Boolean
				EndCursor   githubv4.String
			}
			Nodes []struct {
				Slug         githubv4.String
				Repositories struct {
					PageInfo struct {
						HasNextPage githubv4.Boolean
						EndCursor   githubv4.String
					}
					Edges []struct {
						Permission githubv4.String
						Node       struct{ Name githubv4.String }
					}
				} `graphql:"repositories(first: 100, after: $repoCursor, orderBy: {field: NAME, direction: ASC})"`
			}
		} `graphql:"teams(first: 100, after: $teamCursor)"`
	} `graphql:"organization(login: $org)"`
}

// teamReposQuery is the GraphQL query struct for fetching repositories of a
// single team. Used as a fallback when a team has more than 100 repositories.
// Team is a pointer because organization.team(slug:) is a nullable GraphQL
// field — the team may have been renamed or deleted between the initial
// orgTeamsWithReposQuery page and this overflow call.
type teamReposQuery struct {
	Organization struct {
		Team *struct {
			Repositories struct {
				PageInfo struct {
					HasNextPage githubv4.Boolean
					EndCursor   githubv4.String
				}
				Edges []struct {
					Permission githubv4.String
					Node       struct{ Name githubv4.String }
				}
			} `graphql:"repositories(first: 100, after: $repoCursor, orderBy: {field: NAME, direction: ASC})"`
		} `graphql:"team(slug: $teamSlug)"`
	} `graphql:"organization(login: $org)"`
}

// orgReposQuery is the GraphQL query struct for fetching all organisation
// repositories with their metadata (name, visibility, archived, disabled).
type orgReposQuery struct {
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

// buildRepoTeamsMap fetches all organisation teams and their repositories via
// GraphQL and returns a map of repository name → team permissions.
// Teams with more than 100 repositories fall back to a dedicated single-team
// query so that the repoCursor is scoped to that team only.
func (t *DefaultRepositoryProvider) buildRepoTeamsMap(ctx context.Context) (map[string][]repoguardsapv1.GithubTeamWithPermission, error) {
	repoTeams := make(map[string][]repoguardsapv1.GithubTeamWithPermission)

	var teamCursor *githubv4.String
	for {
		var query orgTeamsWithReposQuery
		vars := map[string]any{
			"org":        githubv4.String(t.organization),
			"teamCursor": teamCursor,
			"repoCursor": (*githubv4.String)(nil),
		}

		if err := t.graphqlClient.Query(ctx, &query, vars); err != nil {
			ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "error").Inc()
			return nil, err
		}
		ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "success").Inc()

		for _, team := range query.Organization.Teams.Nodes {
			slug := string(team.Slug)
			if slug == "" {
				continue
			}

			for _, edge := range team.Repositories.Edges {
				repoName := string(edge.Node.Name)
				if repoName == "" {
					continue
				}
				perm := graphqlPermissionToTeamPermission(string(edge.Permission))
				repoTeams[repoName] = append(repoTeams[repoName], repoguardsapv1.GithubTeamWithPermission{
					Team:       slug,
					Permission: perm,
				})
			}

			// Rare edge case: team has >100 repos. Use a dedicated single-team
			// query so the repoCursor applies only to this team.
			// Guard against an empty EndCursor: if HasNextPage is true but
			// EndCursor is empty the API response is malformed; skip the
			// overflow query rather than looping on after:"".
			if bool(team.Repositories.PageInfo.HasNextPage) && team.Repositories.PageInfo.EndCursor != "" {
				if err := t.fetchRemainingTeamRepos(ctx, slug, team.Repositories.PageInfo.EndCursor, repoTeams); err != nil {
					return nil, err
				}
			}
		}

		if !bool(query.Organization.Teams.PageInfo.HasNextPage) {
			break
		}
		cursor := query.Organization.Teams.PageInfo.EndCursor
		teamCursor = &cursor
	}

	return repoTeams, nil
}

// fetchRemainingTeamRepos paginates through the remaining repositories for a
// single team (slug) starting from afterCursor, appending results to repoTeams.
func (t *DefaultRepositoryProvider) fetchRemainingTeamRepos(ctx context.Context, slug string, afterCursor githubv4.String, repoTeams map[string][]repoguardsapv1.GithubTeamWithPermission) error {
	cursor := afterCursor
	for {
		var query teamReposQuery
		vars := map[string]any{
			"org":        githubv4.String(t.organization),
			"teamSlug":   githubv4.String(slug),
			"repoCursor": &cursor,
		}
		if err := t.graphqlClient.Query(ctx, &query, vars); err != nil {
			ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "error").Inc()
			return err
		}
		ghmetrics.GraphQLCallsTotal.WithLabelValues(t.githubName, t.organization, "success").Inc()

		// Team is nullable: the team may have been deleted/renamed between the
		// initial teams query and this overflow call. Treat nil as an empty result.
		if query.Organization.Team == nil {
			break
		}

		for _, edge := range query.Organization.Team.Repositories.Edges {
			repoName := string(edge.Node.Name)
			if repoName == "" {
				continue
			}
			perm := graphqlPermissionToTeamPermission(string(edge.Permission))
			repoTeams[repoName] = append(repoTeams[repoName], repoguardsapv1.GithubTeamWithPermission{
				Team:       slug,
				Permission: perm,
			})
		}

		if !bool(query.Organization.Team.Repositories.PageInfo.HasNextPage) {
			break
		}
		cursor = query.Organization.Team.Repositories.PageInfo.EndCursor
	}
	return nil
}

// ExtendedListGraphQL fetches all organisation repositories and their team permissions
// using GraphQL queries instead of N+1 REST calls.
// For 300 repositories this significantly reduces REST API consumption per reconcile.
//
// Repos that are archived or disabled are excluded (same semantics as List/ExtendedList).
// Team permissions are gathered by traversing organisation → teams → repositories, since
// GitHub's GraphQL API does not expose a `teams` field on the Repository type.
func (t *DefaultRepositoryProvider) ExtendedListGraphQL(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error) {
	// Step 1: build a map of repo name → teams from the teams side.
	repoTeams, err := t.buildRepoTeamsMap(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	publicRepos := make([]repoguardsapv1.GithubRepository, 0)
	privateRepos := make([]repoguardsapv1.GithubRepository, 0)
	internalRepos := make([]repoguardsapv1.GithubRepository, 0)

	// Step 2: list all repositories with their metadata.
	var repoCursor *githubv4.String
	for {
		var query orgReposQuery
		vars := map[string]any{
			"org":        githubv4.String(t.organization),
			"repoCursor": repoCursor,
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

			teams := repoTeams[name] // nil → empty slice is fine
			if teams == nil {
				teams = []repoguardsapv1.GithubTeamWithPermission{}
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
