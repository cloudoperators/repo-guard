// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gogithub "github.com/google/go-github/v88/github"
	"github.com/gosimple/slug"
	"github.com/palantir/go-githubapp/githubapp"

	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
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
	service      gogithub.TeamsService
	organization string
	githubName   string
	cache        *etagCache
}

// installationID can be found at Organizations - Settings - Installed Github Apps and check the URL
func NewTeamsProvider(cc githubapp.ClientCreator, githubName, organization string, installationID int64) (TeamsProvider, error) {

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

	cache := getOrCreateOrgCache(githubName, organization)

	// Clone the client with an ETag transport for conditional GET caching.
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
	etagClient, err := client.Clone(gogithub.WithHTTPClient(etagHTTP))
	if err != nil {
		return nil, fmt.Errorf("clone github client with etag transport: %w", err)
	}

	return &DefaultTeamsProvider{service: *etagClient.Teams, organization: organization, githubName: githubName, cache: cache}, nil
}

func (t *DefaultTeamsProvider) List(ctx context.Context) ([]string, error) {

	firstPageKey := fmt.Sprintf("/orgs/%s/teams?per_page=100", t.organization)

	opt := &gogithub.ListOptions{
		PerPage: 100,
	}

	teamList := make([]string, 0)
	for {
		teams, response, err := t.service.ListTeams(ctx, t.organization, opt)
		if err != nil {
			if response != nil && response.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(t.githubName, t.organization, "teams-list").Inc()
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
		for _, team := range teams {
			if team == nil {
				continue
			}
			// Skip enterprise-managed teams — they cannot be managed via the org API.
			// Attempting to add/remove them returns HTTP 422.
			if team.GetType() == "enterprise" {
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

	if etag, ok := t.cache.getEtag(firstPageKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(t.githubName, t.organization, "teams-list").Inc()
		t.cache.set(firstPageKey, etag, teamList)
	}

	return teamList, nil
}

func (t DefaultTeamsProvider) MembersExtended(ctx context.Context, team string) ([]GithubMember, error) {

	teamSlug := slug.Make(team)
	// etagKey matches the URL used by the transport for If-None-Match injection.
	// valueKey has a "#ext" suffix to avoid colliding with the Members() cache entry
	// for the same URL, which stores []string (different type).
	etagKey := fmt.Sprintf("/orgs/%s/teams/%s/members?per_page=100", t.organization, teamSlug)
	valueKey := etagKey + "#ext"

	opt := &gogithub.TeamListTeamMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}

	userList := make([]GithubMember, 0)
	for {
		users, resp, err := t.service.ListTeamMembersBySlug(ctx, t.organization, teamSlug, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(t.githubName, t.organization, "team-members-ext").Inc()
				if cached, ok := t.cache.getValue(valueKey); ok {
					if v, ok := cached.([]GithubMember); ok {
						return v, nil
					}
				}
				// Invalidate both the value key and the ETag key so the transport
				// stops injecting If-None-Match and forces a fresh 200 on the next call.
				t.cache.invalidate(valueKey)
				t.cache.invalidate(etagKey)
				return nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", valueKey)
			}
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

	if etag, ok := t.cache.getEtag(etagKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(t.githubName, t.organization, "team-members-ext").Inc()
		t.cache.set(valueKey, etag, userList)
	}

	return userList, nil
}

func (t DefaultTeamsProvider) Members(ctx context.Context, team string) ([]string, error) {

	teamSlug := slug.Make(team)
	firstPageKey := fmt.Sprintf("/orgs/%s/teams/%s/members?per_page=100", t.organization, teamSlug)

	opt := &gogithub.TeamListTeamMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
	}

	userList := make([]string, 0)
	for {
		users, resp, err := t.service.ListTeamMembersBySlug(ctx, t.organization, teamSlug, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(t.githubName, t.organization, "team-members").Inc()
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

	if etag, ok := t.cache.getEtag(firstPageKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(t.githubName, t.organization, "team-members").Inc()
		t.cache.set(firstPageKey, etag, userList)
	}

	return userList, nil
}

func (t DefaultTeamsProvider) AddTeam(ctx context.Context, team string) error {

	privacyLevel := "closed"
	description := "membership to this team is managed by github-guard"

	_, response, err := t.service.CreateTeam(ctx, t.organization, gogithub.NewTeam{Name: team, Privacy: &privacyLevel, Description: &description})
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
