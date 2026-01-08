// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"

	"github.com/palantir/go-githubapp/githubapp"

	"github.com/google/go-github/v81/github"
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

func (o *DefaultOrganizationProvider) githubMembers(role string, filter string) ([]*github.User, error) {

	opt := &github.ListMembersOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Role:        role,
		Filter:      filter,
	}

	var allMembers []*github.User
	for {
		users, resp, err := o.organizationService.ListMembers(context.Background(), o.organization, opt)
		if err != nil {
			return nil, err
		}
		allMembers = append(allMembers, users...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	return allMembers, nil

}
func (o *DefaultOrganizationProvider) members(role string) ([]string, error) {

	allMembers, err := o.githubMembers(role, "")
	if err != nil {
		return nil, err
	}

	ownerList := make([]string, 0)
	for _, owner := range allMembers {
		if owner == nil {
			continue
		}
		login := owner.GetLogin()
		if login == "" {
			continue
		}
		ownerList = append(ownerList, login)
	}

	return ownerList, nil
}

func (o *DefaultOrganizationProvider) membersExtended(role string) ([]GithubMember, error) {

	allMembers, err := o.githubMembers(role, "")
	if err != nil {
		return nil, err
	}

	result := make([]GithubMember, 0)
	for _, m := range allMembers {
		result = append(result, GithubMember{Login: m.GetLogin(), UID: m.GetID()})
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

	mfadisabled, err := o.githubMembers("all", "2fa_disabled")
	if err != nil {
		return nil, nil, err
	}

	allMembers, err := o.githubMembers("all", "")
	if err != nil {
		return nil, nil, err
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
