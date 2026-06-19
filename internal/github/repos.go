// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/palantir/go-githubapp/githubapp"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/google/go-github/v85/github"
)

type RepositoryProvider interface {
	List(ctx context.Context) ([]string, []string, []string, error)
	ExtendedList(ctx context.Context) ([]repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, []repoguardsapv1.GithubRepository, error)
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
	return &DefaultRepositoryProvider{repositoryService: *client.Repositories, teamsService: *client.Teams, organization: organization}, nil
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
		default: // "public" and unknown values
			publicRepoList = append(publicRepoList, name)
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

	opt := &github.ListCollaboratorsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var allUsers []*github.User
	for {
		users, resp, err := t.repositoryService.ListCollaborators(ctx, t.organization, repo, opt)
		if err != nil {
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

	return collobarators, nil
}

func (t *DefaultRepositoryProvider) IsPrivate(ctx context.Context, repo string) (bool, error) {

	r, response, err := t.repositoryService.Get(ctx, t.organization, repo)

	if err != nil {
		return false, err
	}
	if response.StatusCode != 200 {
		return false, fmt.Errorf("getting repository response code: %d", response.StatusCode)
	}

	if r.GetVisibility() == "private" {
		return true, nil
	}

	return false, nil
}
