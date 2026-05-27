// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/palantir/go-githubapp/githubapp"

	"github.com/google/go-github/v85/github"
)

type OrganizationProvider interface {
	Owners(ctx context.Context) ([]string, error)
	OwnersExtended(ctx context.Context) ([]GithubMember, error)
	Members(ctx context.Context) ([]string, error)
	ExtendedMembers(ctx context.Context) ([]*github.User, []*github.User, error)
	ChangeToOwner(ctx context.Context, user string) error
	ChangeToMember(ctx context.Context, user string) error
	RemoveFromOrg(ctx context.Context, user string) error
}

type DefaultOrganizationProvider struct {
	organizationService github.OrganizationsService
	usersService        github.UsersService
	organization        string
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
	return &DefaultOrganizationProvider{organizationService: *client.Organizations, usersService: *client.Users, organization: organization}, nil
}

func (o *DefaultOrganizationProvider) members(ctx context.Context, role string) ([]string, error) {

	opt := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        role,
	}

	memberList := make([]string, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, opt)
		if err != nil {
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

	return memberList, nil
}

func (o *DefaultOrganizationProvider) membersExtended(ctx context.Context, role string) ([]GithubMember, error) {

	opt := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        role,
	}

	result := make([]GithubMember, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, opt)
		if err != nil {
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
	opt := &github.ListOptions{PerPage: 100}

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

func (o *DefaultOrganizationProvider) ExtendedMembers(ctx context.Context) ([]*github.User, []*github.User, error) {

	optMfa := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        "all",
		Filter:      "2fa_disabled",
	}
	mfadisabled := make([]*github.User, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, optMfa)
		if err != nil {
			return nil, nil, err
		}
		mfadisabled = append(mfadisabled, users...)
		if resp.NextPage == 0 {
			break
		}
		optMfa.Page = resp.NextPage
	}

	optAll := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        "all",
	}
	allMembers := make([]*github.User, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(ctx, o.organization, optAll)
		if err != nil {
			return nil, nil, err
		}
		allMembers = append(allMembers, users...)
		if resp.NextPage == 0 {
			break
		}
		optAll.Page = resp.NextPage
	}

	return allMembers, mfadisabled, nil
}

func (o *DefaultOrganizationProvider) Members(ctx context.Context) ([]string, error) {

	return o.members(ctx, "all")
}

func (o *DefaultOrganizationProvider) changeRole(ctx context.Context, user string, role string) error {

	membership := &github.Membership{}
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
