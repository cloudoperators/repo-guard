// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/palantir/go-githubapp/githubapp"

	"github.com/gosimple/slug"

	"github.com/google/go-github/v85/github"
)

type TeamsProvider interface {
	List(ctx context.Context) ([]string, error)
	Members(ctx context.Context, team string) ([]string, error)
	MembersExtended(ctx context.Context, team string) ([]GithubMember, error)
	AddTeam(ctx context.Context, team string) error
	RemoveTeam(ctx context.Context, team string) error
	AddUser(ctx context.Context, team, user string) (bool, error)
	RemoveUser(ctx context.Context, team, user string) error
}

type GithubMember struct {
	Login string
	UID   int64
}

type DefaultTeamsProvider struct {
	service      github.TeamsService
	organization string
}

// installationID can be found at Organizations - Settings - Installed Github Apps and check the URL
func NewTeamsProvider(cc githubapp.ClientCreator, organization string, installationID int64) (TeamsProvider, error) {

	client, err := cc.NewInstallationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("create installation client: %w", err)
	}
	if client.Teams == nil {
		return nil, errors.New("empty teams service")
	}
	if organization == "" {
		return nil, errors.New("organization name should not be empty")
	}

	return &DefaultTeamsProvider{service: *client.Teams, organization: organization}, nil
}

func (t *DefaultTeamsProvider) List(ctx context.Context) ([]string, error) {

	opt := &github.ListOptions{
		PerPage: 100,
	}

	teamList := make([]string, 0)
	for {
		teams, response, err := t.service.ListTeams(ctx, t.organization, opt)
		if err != nil {
			return nil, err
		}
		for _, team := range teams {
			if team == nil {
				continue
			}
			name := team.GetName()
			if name == "" {
				continue
			}
			teamList = append(teamList, name)
		}
		if response.NextPage == 0 {
			break
		}
		opt.Page = response.NextPage
	}

	return teamList, nil

}

func (t DefaultTeamsProvider) MembersExtended(ctx context.Context, team string) ([]GithubMember, error) {

	opt := &github.TeamListTeamMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	userList := make([]GithubMember, 0)
	for {
		users, resp, err := t.service.ListTeamMembersBySlug(ctx, t.organization, slug.Make(team), opt)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if user == nil {
				continue
			}
			userList = append(userList, GithubMember{Login: user.GetLogin(), UID: user.GetID()})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return userList, nil
}

func (t DefaultTeamsProvider) Members(ctx context.Context, team string) ([]string, error) {

	opt := &github.TeamListTeamMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	userList := make([]string, 0)
	for {
		users, resp, err := t.service.ListTeamMembersBySlug(ctx, t.organization, slug.Make(team), opt)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if user == nil {
				continue
			}
			login := user.GetLogin()
			if login == "" {
				continue
			}
			userList = append(userList, login)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return userList, nil
}

func (t DefaultTeamsProvider) AddTeam(ctx context.Context, team string) error {

	privacyLevel := "closed"
	description := "membership to this team is managed by github-guard"

	_, response, err := t.service.CreateTeam(ctx, t.organization, github.NewTeam{Name: team, Privacy: &privacyLevel, Description: &description})
	if err != nil {
		// Treat 422 "Name must be unique for this org" as success (team already exists)
		if response != nil && response.StatusCode == 422 {
			return nil
		}
		return err
	}

	if response.StatusCode != 201 {
		return fmt.Errorf("creating team response code: %d", response.StatusCode)
	}

	return nil
}
func (t DefaultTeamsProvider) RemoveTeam(ctx context.Context, team string) error {

	response, err := t.service.DeleteTeamBySlug(ctx, t.organization, slug.Make(team))
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			return nil
		}
		return err
	}

	if response.StatusCode != 204 {
		return fmt.Errorf("deleting team response code: %d", response.StatusCode)
	}

	return nil
}

func (t DefaultTeamsProvider) AddUser(ctx context.Context, team, user string) (bool, error) {

	_, response, err := t.service.AddTeamMembershipBySlug(ctx, t.organization, slug.Make(team), user, nil)
	if err != nil {
		if response != nil {
			if response.StatusCode == 404 {
				return false, fmt.Errorf("user not found in github")
			}
			if response.StatusCode != 200 {
				return true, fmt.Errorf("adding user to team response code: %d", response.StatusCode)
			}
		}
		return false, err
	}

	return true, nil

}
func (t DefaultTeamsProvider) RemoveUser(ctx context.Context, team, user string) error {

	response, err := t.service.RemoveTeamMembershipBySlug(ctx, t.organization, slug.Make(team), user)
	if err != nil {
		if response != nil && response.StatusCode == 404 {
			return nil
		}
		return err
	}

	if response.StatusCode != 204 {
		return fmt.Errorf("removing user from team response code: %d", response.StatusCode)
	}

	return nil
}
