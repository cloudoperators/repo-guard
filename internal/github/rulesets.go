// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
	"github.com/palantir/go-githubapp/githubapp"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
)

// RulesetProvider manages organization-level rulesets.
type RulesetProvider interface {
	// ListAll returns all rulesets defined on the organization.
	ListAll(ctx context.Context) ([]*gogithub.RepositoryRuleset, error)
	// Get returns the ruleset with the given ID.
	Get(ctx context.Context, rulesetID int64) (*gogithub.RepositoryRuleset, error)
	// Create creates a new ruleset and returns the created object (including its ID).
	Create(ctx context.Context, rs gogithub.RepositoryRuleset) (*gogithub.RepositoryRuleset, error)
	// Update fully replaces the ruleset with the given ID.
	Update(ctx context.Context, rulesetID int64, rs gogithub.RepositoryRuleset) (*gogithub.RepositoryRuleset, error)
	// Delete removes the ruleset with the given ID.
	Delete(ctx context.Context, rulesetID int64) error
}

type defaultRulesetProvider struct {
	orgs         gogithub.OrganizationsService
	organization string
}

func NewRulesetProvider(cc githubapp.ClientCreator, installationID int64, organization string) (RulesetProvider, error) {
	client, err := cc.NewInstallationClient(installationID)
	if err != nil {
		return nil, fmt.Errorf("create installation client: %w", err)
	}
	if client.Organizations == nil {
		return nil, errors.New("empty organizations service")
	}
	if organization == "" {
		return nil, errors.New("organization name must not be empty")
	}
	return &defaultRulesetProvider{
		orgs:         *client.Organizations,
		organization: organization,
	}, nil
}

func (p *defaultRulesetProvider) ListAll(ctx context.Context) ([]*gogithub.RepositoryRuleset, error) {
	var result []*gogithub.RepositoryRuleset
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		page, resp, err := p.orgs.ListAllRepositoryRulesets(ctx, p.organization, opts)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return result, nil
}

func (p *defaultRulesetProvider) Get(ctx context.Context, rulesetID int64) (*gogithub.RepositoryRuleset, error) {
	rs, _, err := p.orgs.GetRepositoryRuleset(ctx, p.organization, rulesetID)
	return rs, err
}

func (p *defaultRulesetProvider) Create(ctx context.Context, rs gogithub.RepositoryRuleset) (*gogithub.RepositoryRuleset, error) {
	created, _, err := p.orgs.CreateRepositoryRuleset(ctx, p.organization, rs)
	return created, err
}

func (p *defaultRulesetProvider) Update(ctx context.Context, rulesetID int64, rs gogithub.RepositoryRuleset) (*gogithub.RepositoryRuleset, error) {
	updated, _, err := p.orgs.UpdateRepositoryRuleset(ctx, p.organization, rulesetID, rs)
	return updated, err
}

func (p *defaultRulesetProvider) Delete(ctx context.Context, rulesetID int64) error {
	_, err := p.orgs.DeleteRepositoryRuleset(ctx, p.organization, rulesetID)
	return err
}

// SpecToGitHub converts a GithubOrganizationRulesetSpec to a go-github RepositoryRuleset.
func SpecToGitHub(spec v1.GithubOrganizationRulesetSpec) gogithub.RepositoryRuleset {
	target := gogithub.RulesetTarget(spec.Target)
	enforcement := gogithub.RulesetEnforcement(spec.Enforcement)

	rs := gogithub.RepositoryRuleset{
		Name:        spec.Name,
		Target:      &target,
		Enforcement: enforcement,
	}

	// bypass actors
	for _, ba := range spec.BypassActors {
		mode := gogithub.BypassMode(ba.BypassMode)
		actor := &gogithub.BypassActor{
			ActorType:  (*gogithub.BypassActorType)(ptr(string(ba.ActorType))),
			BypassMode: &mode,
		}
		if ba.ActorID != 0 {
			actor.ActorID = &ba.ActorID
		}
		rs.BypassActors = append(rs.BypassActors, actor)
	}

	// conditions
	if c := spec.Conditions; c != nil {
		rs.Conditions = &gogithub.RepositoryRulesetConditions{}
		if c.RefName != nil {
			rs.Conditions.RefName = &gogithub.RepositoryRulesetRefConditionParameters{
				Include: c.RefName.Include,
				Exclude: c.RefName.Exclude,
			}
			if rs.Conditions.RefName.Include == nil {
				rs.Conditions.RefName.Include = []string{}
			}
			if rs.Conditions.RefName.Exclude == nil {
				rs.Conditions.RefName.Exclude = []string{}
			}
		}
		if c.RepositoryName != nil {
			rs.Conditions.RepositoryName = &gogithub.RepositoryRulesetRepositoryNamesConditionParameters{
				Include:   c.RepositoryName.Include,
				Exclude:   c.RepositoryName.Exclude,
				Protected: c.RepositoryName.Protected,
			}
		}
	}

	// rules
	if r := spec.Rules; r != nil {
		rs.Rules = &gogithub.RepositoryRulesetRules{}
		if r.Creation != nil && *r.Creation {
			e := gogithub.EmptyRuleParameters{}
			rs.Rules.Creation = &e
		}
		if r.Update != nil && *r.Update {
			rs.Rules.Update = &gogithub.UpdateRuleParameters{}
		}
		if r.Deletion != nil && *r.Deletion {
			e := gogithub.EmptyRuleParameters{}
			rs.Rules.Deletion = &e
		}
		if r.RequiredLinearHistory != nil && *r.RequiredLinearHistory {
			e := gogithub.EmptyRuleParameters{}
			rs.Rules.RequiredLinearHistory = &e
		}
		if r.RequiredSignatures != nil && *r.RequiredSignatures {
			e := gogithub.EmptyRuleParameters{}
			rs.Rules.RequiredSignatures = &e
		}
		if r.NonFastForward != nil && *r.NonFastForward {
			e := gogithub.EmptyRuleParameters{}
			rs.Rules.NonFastForward = &e
		}
		if pr := r.PullRequest; pr != nil {
			rs.Rules.PullRequest = &gogithub.PullRequestRuleParameters{
				DismissStaleReviewsOnPush:      pr.DismissStaleReviewsOnPush,
				RequireCodeOwnerReview:         pr.RequireCodeOwnerReview,
				RequireLastPushApproval:        pr.RequireLastPushApproval,
				RequiredApprovingReviewCount:   pr.RequiredApprovingReviewCount,
				RequiredReviewThreadResolution: pr.RequiredReviewThreadResolution,
			}
		}
	}

	return rs
}

func ptr[T any](v T) *T { return &v }
