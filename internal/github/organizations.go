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
	Owners() ([]string, error)
	OwnersExtended() ([]GithubMember, error)
	Members() ([]string, error)
	ExtendedMembers() ([]*github.User, []*github.User, error)
	ChangeToOwner(user string) error
	ChangeToMember(user string) error
	RemoveFromOrg(user string) error
}

type DefaultOrganizationProvider struct {
	organizationService github.OrganizationsService
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

	if organization == "" {
		return nil, errors.New("organization name should not be empty")
	}
	return &DefaultOrganizationProvider{organizationService: *client.Organizations, organization: organization}, nil
}

func (o *DefaultOrganizationProvider) members(role string) ([]string, error) {

	opt := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        role,
	}

	memberList := make([]string, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(context.Background(), o.organization, opt)
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

func (o *DefaultOrganizationProvider) membersExtended(role string) ([]GithubMember, error) {

	opt := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        role,
	}

	result := make([]GithubMember, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(context.Background(), o.organization, opt)
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

func (o *DefaultOrganizationProvider) Owners() ([]string, error) {

	return o.members("admin")
}

func (o *DefaultOrganizationProvider) OwnersExtended() ([]GithubMember, error) {
	return o.membersExtended("admin")
}

func (o *DefaultOrganizationProvider) ExtendedMembers() ([]*github.User, []*github.User, error) {

	optMfa := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        "all",
		Filter:      "2fa_disabled",
	}
	mfadisabled := make([]*github.User, 0)
	for {
		users, resp, err := o.organizationService.ListMembers(context.Background(), o.organization, optMfa)
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
		users, resp, err := o.organizationService.ListMembers(context.Background(), o.organization, optAll)
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

func (o *DefaultOrganizationProvider) Members() ([]string, error) {

	return o.members("all")
}

func (o *DefaultOrganizationProvider) changeRole(user string, role string) error {

	membership := &github.Membership{}
	membership.Role = &role

	_, response, err := o.organizationService.EditOrgMembership(context.Background(), user, o.organization, membership)
	if err != nil {
		return err
	}

	if response.StatusCode != 200 {
		return fmt.Errorf("changing user to organization owner response code: %d", response.StatusCode)
	}
	return nil
}

func (o *DefaultOrganizationProvider) ChangeToOwner(user string) error {
	return o.changeRole(user, "admin")
}

func (o *DefaultOrganizationProvider) ChangeToMember(user string) error {
	return o.changeRole(user, "member")
}

func (o *DefaultOrganizationProvider) RemoveFromOrg(user string) error {
	response, err := o.organizationService.RemoveMember(context.Background(), o.organization, user)
	if err != nil {
		return err
	}

	if response.StatusCode != 204 {
		return fmt.Errorf("removing user from organization response code: %d", response.StatusCode)
	}
	return nil
}
