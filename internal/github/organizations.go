// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gogithub "github.com/google/go-github/v88/github"
	"github.com/palantir/go-githubapp/githubapp"

	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
)

type OrganizationProvider interface {
	Owners(ctx context.Context) ([]string, error)
	OwnersExtended(ctx context.Context) ([]GithubMember, error)
	Members(ctx context.Context) ([]string, error)
	ExtendedMembers(ctx context.Context) ([]*gogithub.User, []*gogithub.User, error)
	ChangeToOwner(ctx context.Context, user string) error
	ChangeToMember(ctx context.Context, user string) error
	RemoveFromOrg(ctx context.Context, user string) error
}

type DefaultOrganizationProvider struct {
	organizationService gogithub.OrganizationsService
	usersService        gogithub.UsersService
	organization        string
	cache               *etagCache
}

// installationID can be found at Organizations - Settings - Installed Github Apps and check the URL
func NewOrganizationProvider(cc githubapp.ClientCreator, organization string, installationID int64) (OrganizationProvider, error) {

	client, err := cc.NewInstallationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("create installation client: %w", err)
	}
	if client.Organizations == nil {
		return nil, errors.New("empty organizations service")
	}
	if client.Users == nil {
		return nil, errors.New("empty users service")
	}

	if organization == "" {
		return nil, errors.New("organization name should not be empty")
	}

	cache := getOrCreateOrgCache(organization)

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

	return &DefaultOrganizationProvider{
		organizationService: *etagClient.Organizations,
		usersService:        *client.Users, // user lookups use the original client
		organization:        organization,
		cache:               cache,
	}, nil
}

func (o *DefaultOrganizationProvider) members(ctx context.Context, role string) ([]string, error) {

	firstPageKey := fmt.Sprintf("/orgs/%s/members?per_page=100&role=%s", o.organization, role)

	opt := &gogithub.ListMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
		Role:        role,
	}

	memberList := make([]string, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(o.organization, "org-members").Inc()
				if cached, ok := o.cache.getValue(firstPageKey); ok {
					if v, ok := cached.([]string); ok {
						return v, nil
					}
				}
				o.cache.invalidate(firstPageKey)
				return nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", firstPageKey)
			}
			return nil, err
		}
		for _, member := range users {
			if member == nil {
				continue
			}
			login := member.GetLogin()
			if login == "" {
				continue
			}
			memberList = append(memberList, login)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	if etag, ok := o.cache.getEtag(firstPageKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(o.organization, "org-members").Inc()
		o.cache.set(firstPageKey, etag, memberList)
	}

	return memberList, nil
}

func (o *DefaultOrganizationProvider) membersExtended(ctx context.Context, role string) ([]GithubMember, error) {

	firstPageKey := fmt.Sprintf("/orgs/%s/members?per_page=100&role=%s", o.organization, role)

	opt := &gogithub.ListMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
		Role:        role,
	}

	result := make([]GithubMember, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, opt)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(o.organization, "org-members-ext").Inc()
				if cached, ok := o.cache.getValue(firstPageKey); ok {
					if v, ok := cached.([]GithubMember); ok {
						return v, nil
					}
				}
				o.cache.invalidate(firstPageKey)
				return nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", firstPageKey)
			}
			return nil, err
		}
		for _, m := range users {
			if m == nil {
				continue
			}
			result = append(result, GithubMember{Login: m.GetLogin(), UID: m.GetID()})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	if etag, ok := o.cache.getEtag(firstPageKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(o.organization, "org-members-ext").Inc()
		o.cache.set(firstPageKey, etag, result)
	}

	return result, nil
}

func (o *DefaultOrganizationProvider) Owners(ctx context.Context) ([]string, error) {

	return o.members(ctx, "admin")
}

func (o *DefaultOrganizationProvider) OwnersExtended(ctx context.Context) ([]GithubMember, error) {
	active, err := o.membersExtended(ctx, "admin")
	if err != nil {
		return nil, err
	}
	pending, err := o.pendingAdminMembers(ctx)
	if err != nil {
		return nil, err
	}
	return append(active, pending...), nil
}

// pendingAdminMembers returns users with pending admin invitations to the org.
// ListMembers only returns active members, so without this, users whose invite is still pending
// are invisible to OwnersExtended and get re-invited on every reconcile.
func (o *DefaultOrganizationProvider) pendingAdminMembers(ctx context.Context) ([]GithubMember, error) {
	opt := &gogithub.ListOptions{PerPage: 100}

	result := make([]GithubMember, 0)
	for {
		invitations, resp, err := o.organizationService.ListPendingOrgInvitations(ctx, o.organization, opt)
		if err != nil {
			// Some GitHub Enterprise instances do not expose the invitations endpoint; treat
			// 404 as "no pending invitations" so reconciliation is not blocked.
			if resp != nil && resp.StatusCode == 404 {
				return result, nil
			}
			return nil, err
		}
		for _, inv := range invitations {
			if inv == nil {
				continue
			}
			if inv.GetRole() != "admin" {
				continue
			}
			login := inv.GetLogin()
			if login == "" {
				continue
			}
			user, _, err := o.usersService.Get(ctx, login)
			if err != nil {
				return nil, fmt.Errorf("get user ID for pending invitee %q: %w", login, err)
			}
			result = append(result, GithubMember{Login: login, UID: user.GetID()})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return result, nil
}

func (o *DefaultOrganizationProvider) ExtendedMembers(ctx context.Context) ([]*gogithub.User, []*gogithub.User, error) {

	mfaKey := fmt.Sprintf("/orgs/%s/members?filter=2fa_disabled&per_page=100&role=all", o.organization)
	allKey := fmt.Sprintf("/orgs/%s/members?per_page=100&role=all", o.organization)

	optMfa := &gogithub.ListMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
		Role:        "all",
		Filter:      "2fa_disabled",
	}
	mfadisabled := make([]*gogithub.User, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, optMfa)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(o.organization, "org-members-2fa").Inc()
				if cached, ok := o.cache.getValue(mfaKey); ok {
					if v, ok := cached.([]*gogithub.User); ok {
						mfadisabled = v
						break
					}
				}
				o.cache.invalidate(mfaKey)
				return nil, nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", mfaKey)
			}
			return nil, nil, err
		}
		mfadisabled = append(mfadisabled, users...)
		if resp.NextPage == 0 {
			break
		}
		optMfa.Page = resp.NextPage
	}
	if etag, ok := o.cache.getEtag(mfaKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(o.organization, "org-members-2fa").Inc()
		o.cache.set(mfaKey, etag, mfadisabled)
	}

	optAll := &gogithub.ListMembersOptions{
		ListOptions: gogithub.ListOptions{PerPage: 100},
		Role:        "all",
	}
	allMembers := make([]*gogithub.User, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, optAll)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotModified {
				ghmetrics.EtagCacheHitsTotal.WithLabelValues(o.organization, "org-members-all").Inc()
				if cached, ok := o.cache.getValue(allKey); ok {
					if v, ok := cached.([]*gogithub.User); ok {
						allMembers = v
						break
					}
				}
				o.cache.invalidate(allKey)
				return nil, nil, fmt.Errorf("etag cache inconsistency for %s: 304 received but no valid cached value", allKey)
			}
			return nil, nil, err
		}
		allMembers = append(allMembers, users...)
		if resp.NextPage == 0 {
			break
		}
		optAll.Page = resp.NextPage
	}
	if etag, ok := o.cache.getEtag(allKey); ok && etag != "" {
		ghmetrics.EtagCacheMissesTotal.WithLabelValues(o.organization, "org-members-all").Inc()
		o.cache.set(allKey, etag, allMembers)
	}

	return allMembers, mfadisabled, nil
}

func (o *DefaultOrganizationProvider) Members(ctx context.Context) ([]string, error) {

	return o.members(ctx, "all")
}

func (o *DefaultOrganizationProvider) changeRole(ctx context.Context, user string, role string) error {

	membership := &gogithub.Membership{}
	membership.Role = &role

	_, response, err := o.organizationService.EditOrgMembership(ctx, user, o.organization, membership)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("changing user to organization owner response code: %d", response.StatusCode)
	}
	return nil
}

func (o *DefaultOrganizationProvider) ChangeToOwner(ctx context.Context, user string) error {
	return o.changeRole(ctx, user, "admin")
}

func (o *DefaultOrganizationProvider) ChangeToMember(ctx context.Context, user string) error {
	return o.changeRole(ctx, user, "member")
}

func (o *DefaultOrganizationProvider) RemoveFromOrg(ctx context.Context, user string) error {
	response, err := o.organizationService.RemoveMember(ctx, o.organization, user)
	if err != nil {
		return err
	}

	if response.StatusCode != 204 {
		return fmt.Errorf("removing user from organization response code: %d", response.StatusCode)
	}
	return nil
}
