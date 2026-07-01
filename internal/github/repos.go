// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-github/v88/github"
	"github.com/palantir/go-githubapp/githubapp"
	githubv4 "github.com/shurcooL/githubv4"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

type RepositoryProvider interface {
	List(ctx context.Context) ([]string, []string, []string, error)
	ExtendedList(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error)
	ExtendedListGraphQL(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error)
	RepositoryTeams(ctx context.Context, repo string) ([]repoguardsapv1.GithubTeamWithPermission, error)
	RepositoryTeamAdd(ctx context.Context, repo, team string, permission repoguardsapv1.GithubTeamPermission) error
	RepositoryTeamRemove(ctx context.Context, repo, team string) error
	RepositoryCollobarators(ctx context.Context, repo string) ([]string, error)
	RepositoryCollobaratorRemove(ctx context.Context, repo string, user string) (bool, error)
	IsPrivate(ctx context.Context, repo string) (bool, error)
}

type DefaultRepositoryProvider struct {
	repositoryService github.RepositoriesService
	teamsService      github.TeamsService
	organization      string
	graphqlClient     *githubv4.Client
	cache             *etagCache
}

// installationID can be found at Organizations - Settings - Installed Github Apps and check the URL
func NewRepositoryProvider(cc githubapp.ClientCreator, organization string, installationID int64) (RepositoryProvider, error) {

	client, err := cc.NewInstallationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("create installation client: %w", err)
	}
	if client.Repositories == nil {
		return nil, errors.New("empty repositories service")
	}

	if client.Teams == nil {
		return nil, errors.New("empty teams service")
	}

	if organization == "" {
		return nil, errors.New("organization name should not be empty")
	}

	cache := getOrCreateOrgCache(organization)

	// Build a cloned go-github client whose transport injects If-None-Match headers
	// and captures ETag values from 200 responses for conditional GET caching.
	baseHTTP := client.Client()
	baseTransport := baseHTTP.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	etagHTTP := &http.Client{
		Transport:     &etagTransport{wrapped: baseTransport, cache: cache, keyFn: urlCacheKey},
		CheckRedirect: baseHTTP.CheckRedirect,
		Jar:           baseHTTP.Jar,
		Timeout:       baseHTTP.Timeout,
	}
	etagClient, err := client.Clone(github.WithHTTPClient(etagHTTP))
	if err != nil {
		return nil, fmt.Errorf("clone github client with etag transport: %w", err)
	}

	gqlClient, err := cc.NewInstallationV4Client(installationID)
	if err != nil {
		return nil, fmt.Errorf("create installation v4 client: %w", err)
	}

	return &DefaultRepositoryProvider{
		repositoryService: *etagClient.Repositories,
		teamsService:      *client.Teams, // mutations don't benefit from ETag
		organization:      organization,
		graphqlClient:     gqlClient,
		cache:             cache,
	}, nil
}

func (t *DefaultRepositoryProvider) ExtendedList(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error) {
	publicRepos := make([]repoguardsapv1.GithubRepository, 0)
	privateRepos := make([]repoguardsapv1.GithubRepository, 0)
	internalRepos := make([]repoguardsapv1.GithubRepository, 0)

	publicRepoList, privateRepoList, internalRepoList, err := t.List(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, repo := range publicRepoList {

		teams, err := t.RepositoryTeams(ctx, repo)
		if err != nil {
			return nil, nil, nil, err
		}

		publicRepos = append(publicRepos, repoguardsapv1.GithubRepository{Name: repo, Teams: teams})
	}

	for _, repo := range privateRepoList {

		teams, err := t.RepositoryTeams(ctx, repo)
		if err != nil {
			return nil, nil, nil, err
		}

		privateRepos = append(privateRepos, repoguardsapv1.GithubRepository{Name: repo, Teams: teams})
	}

	for _, repo := range internalRepoList {

		teams, err := t.RepositoryTeams(ctx, repo)
		if err != nil {
			return nil, nil, nil, err
		}

		internalRepos = append(internalRepos, repoguardsapv1.GithubRepository{Name: repo, Teams: teams})
	}

	return publicRepos, privateRepos, internalRepos, nil

}

func (t *DefaultRepositoryProvider) List(ctx context.Context) ([]string, []string, []string, error) {

	opt := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allRepos []*github.Repository
	for {
		repos, resp, err := t.repositoryService.ListByOrg(ctx, t.organization, opt)
		if err != nil {
			return nil, nil, nil, err
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	publicRepoList := make([]string, 0)
	privateRepoList := make([]string, 0)
	internalRepoList := make([]string, 0)
	for _, repo := range allRepos {
		if repo == nil {
			continue
		}
		// Skip archived or disabled repositories — GitHub rejects API mutations
		// on these repos with HTTP 422.
		if repo.GetArchived() || repo.GetDisabled() {
			continue
		}
		name := repo.GetName()
		if name == "" {
			continue
		}
		vis := repo.GetVisibility()
		if vis == "" {
			// Fall back to the private bool for older API responses.
			if repo.GetPrivate() {
				vis = "private"
			} else {
				vis = "public"
			}
		}
		switch vis {
		case "private":
			privateRepoList = append(privateRepoList, name)
		case "internal":
			internalRepoList = append(internalRepoList, name)
		case "public":
			publicRepoList = append(publicRepoList, name)
		default:
			// Skip repos with unknown visibility values rather than
			// silently promoting them to a bucket with incorrect semantics.
			continue
		}
	}

	return publicRepoList, privateRepoList, internalRepoList, nil
}

type CollobaratorWithPermission struct {
	Collobarator string
	Permission   string
}

func (t *DefaultRepositoryProvider) RepositoryTeams(ctx context.Context, repo string) ([]repoguardsapv1.GithubTeamWithPermission, error) {

	opt := &github.ListOptions{PerPage: 100}

	var allTeams []*github.Team
	for {
		teams, resp, err := t.repositoryService.ListTeams(ctx, t.organization, repo, opt)
		if err != nil {
			return nil, err
		}
		allTeams = append(allTeams, teams...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	teamWithPermissions := make([]repoguardsapv1.GithubTeamWithPermission, 0)
	for _, t := range allTeams {
		if t == nil {
			continue
		}
		slug := t.GetSlug()
		if slug == "" {
			continue
		}
		perm := ""
		if t.Permission != nil {
			perm = *t.Permission
		}
		teamWithPermissions = append(teamWithPermissions, repoguardsapv1.GithubTeamWithPermission{Team: slug, Permission: repoguardsapv1.GithubTeamPermission(perm)})
	}

	return teamWithPermissions, nil
}
func (t *DefaultRepositoryProvider) RepositoryTeamAdd(ctx context.Context, repo, team string, permission repoguardsapv1.GithubTeamPermission) error {

	opts := github.TeamAddTeamRepoOptions{Permission: string(permission)}
	response, err := t.teamsService.AddTeamRepoBySlug(ctx, t.organization, team, t.organization, repo, &opts)
	if err != nil {
		return err
	}
	if response.StatusCode != 204 {
		return fmt.Errorf("adding team to repository response code: %d", response.StatusCode)
	}
	return nil
}

func (t *DefaultRepositoryProvider) RepositoryCollobaratorRemove(ctx context.Context, repo string, user string) (bool, error) {

	response, err := t.repositoryService.RemoveCollaborator(ctx, t.organization, repo, user)
	if err != nil {
		if response != nil {
			if response.StatusCode == 404 {
				return false, fmt.Errorf("user not found in github")
			}
			if response.StatusCode != 204 {
				return true, fmt.Errorf("remove collaborator response code: %d", response.StatusCode)
			}
		}
		return false, err
	}

	return true, nil

}

func (t *DefaultRepositoryProvider) RepositoryTeamRemove(ctx context.Context, repo, team string) error {

	response, err := t.teamsService.RemoveTeamRepoBySlug(ctx, t.organization, team, t.organization, repo)
	if err != nil {
		return err
	}
	if response.StatusCode != 204 {
		return fmt.Errorf("removing team from repository response code: %d", response.StatusCode)
	}
	return nil
}

func (t *DefaultRepositoryProvider) RepositoryCollobarators(ctx context.Context, repo string) ([]string, error) {

	// The ETag transport (injected at construction via NewRepositoryProvider) automatically
	// injects If-None-Match on GETs and stores new ETags from 200 responses.
	// On 304, go-github returns a non-nil *ErrorResponse; we retrieve the cached value.
	firstPageKey := fmt.Sprintf("/repos/%s/%s/collaborators?per_page=100", t.organization, repo)

	opt := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allUsers []*github.User
	for {
		users, resp, err := t.repositoryService.ListCollaborators(ctx, t.organization, repo, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(t.organization, "repo-collaborators").Inc()
				if cached, ok := t.cache.getValue(firstPageKey); ok {
					if v, ok := cached.([]string); ok {
						return v, nil
					}
				}
				t.cache.invalidate(firstPageKey)
				return nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", firstPageKey)
			}
			return nil, err
		}
		allUsers = append(allUsers, users...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	collobarators := make([]string, 0)

	for _, c := range allUsers {
		if c == nil {
			continue
		}
		login := c.GetLogin()
		if login == "" {
			continue
		}
		collobarators = append(collobarators, login)
	}

	// Store the parsed result under the first-page URL key so a future 304 can return it.
	if etag, ok := t.cache.getEtag(firstPageKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(t.organization, "repo-collaborators").Inc()
		t.cache.set(firstPageKey, etag, collobarators)
	}

	return collobarators, nil
}

func (t *DefaultRepositoryProvider) IsPrivate(ctx context.Context, repo string) (bool, error) {

	repoKey := fmt.Sprintf("/repos/%s/%s", t.organization, repo)

	r, response, err := t.repositoryService.Get(ctx, t.organization, repo)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotModified {
			ghmetrics.EtagCacheHitsTotal.WithLabelValues(t.organization, "repo-visibility").Inc()
			if cached, ok := t.cache.getValue(repoKey); ok {
				if v, ok := cached.(bool); ok {
					return v, nil
				}
			}
			t.cache.invalidate(repoKey)
			return false, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", repoKey)
		}
		return false, err
	}
	if response.StatusCode != 200 {
		return false, fmt.Errorf("getting repository response code: %d", response.StatusCode)
	}

	vis := r.GetVisibility()
	isPrivate := vis == "private" || vis == "internal"

	if etag, ok := t.cache.getEtag(repoKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(t.organization, "repo-visibility").Inc()
		t.cache.set(repoKey, etag, isPrivate)
	}

	return isPrivate, nil
}
