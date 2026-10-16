// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// RulesetEnforcement represents the enforcement mode of a ruleset.
// +kubebuilder:validation:Enum=disabled;active;evaluate
type RulesetEnforcement string

const (
	RulesetEnforcementDisabled RulesetEnforcement = "disabled"
	RulesetEnforcementActive   RulesetEnforcement = "active"
	RulesetEnforcementEvaluate RulesetEnforcement = "evaluate"
)

// RulesetTarget is the target of a ruleset (branch, tag, push, repository).
// +kubebuilder:validation:Enum=branch;tag;push;repository
type RulesetTarget string

const (
	RulesetTargetBranch     RulesetTarget = "branch"
	RulesetTargetTag        RulesetTarget = "tag"
	RulesetTargetPush       RulesetTarget = "push"
	RulesetTargetRepository RulesetTarget = "repository"
)

// BypassActorType is the type of bypass actor.
// +kubebuilder:validation:Enum=Integration;OrganizationAdmin;RepositoryRole;Team;DeployKey
type BypassActorType string

const (
	BypassActorTypeIntegration       BypassActorType = "Integration"
	BypassActorTypeOrganizationAdmin BypassActorType = "OrganizationAdmin"
	BypassActorTypeRepositoryRole    BypassActorType = "RepositoryRole"
	BypassActorTypeTeam              BypassActorType = "Team"
	BypassActorTypeDeployKey         BypassActorType = "DeployKey"
)

// BypassMode is the mode of a bypass actor.
// +kubebuilder:validation:Enum=always;pull_request
type BypassMode string

const (
	BypassModeAlways      BypassMode = "always"
	BypassModePullRequest BypassMode = "pull_request"
)

// BypassActor defines an actor that can bypass a ruleset.
type BypassActor struct {
	// ActorID is the GitHub numeric ID of the team or app. Omit (or set 0) for DeployKey.
	// +optional
	ActorID int64 `json:"actorId,omitzero"`
	// ActorType is the type of the bypass actor.
	ActorType BypassActorType `json:"actorType"`
	// BypassMode defines when the bypass is permitted.
	BypassMode BypassMode `json:"bypassMode"`
}

// RulesetRefCondition filters which refs (branches/tags) the ruleset applies to.
type RulesetRefCondition struct {
	// Include is a list of ref patterns to include (e.g. "~DEFAULT_BRANCH", "refs/heads/main").
	// +listType=set
	// +optional
	Include []string `json:"include,omitempty"`
	// Exclude is a list of ref patterns to exclude.
	// +listType=set
	// +optional
	Exclude []string `json:"exclude,omitempty"`
}

// RulesetRepositoryNameCondition filters which repositories the ruleset applies to.
type RulesetRepositoryNameCondition struct {
	// Include is a list of repository name patterns to include. Use "~ALL" for all repositories.
	// +listType=set
	// +optional
	Include []string `json:"include,omitempty"`
	// Exclude is a list of repository name patterns to exclude.
	// +listType=set
	// +optional
	Exclude []string `json:"exclude,omitempty"`
	// Protected prevents matching repositories from being renamed (it does not restrict matching to already-protected repositories).
	// +optional
	Protected *bool `json:"protected,omitempty"`
}

// RulesetConditions defines which refs and repositories a ruleset applies to.
type RulesetConditions struct {
	// RefName filters which branches or tags the ruleset targets.
	// +optional
	RefName *RulesetRefCondition `json:"refName,omitempty"`
	// RepositoryName filters which repositories in the organization the ruleset applies to.
	// +optional
	RepositoryName *RulesetRepositoryNameCondition `json:"repositoryName,omitempty"`
}

// PullRequestRuleParameters defines required PR review parameters.
type PullRequestRuleParameters struct {
	// DismissStaleReviewsOnPush dismisses approvals when new commits are pushed.
	DismissStaleReviewsOnPush bool `json:"dismissStaleReviewsOnPush"`
	// RequireCodeOwnerReview requires a code owner to approve the PR.
	RequireCodeOwnerReview bool `json:"requireCodeOwnerReview"`
	// RequireLastPushApproval requires approval of the most recent push.
	RequireLastPushApproval bool `json:"requireLastPushApproval"`
	// RequiredApprovingReviewCount is the number of required approvals.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	RequiredApprovingReviewCount int `json:"requiredApprovingReviewCount"`
	// RequiredReviewThreadResolution requires all review threads to be resolved.
	RequiredReviewThreadResolution bool `json:"requiredReviewThreadResolution"`
}

// RulesetRules defines the set of rules enforced by this ruleset.
type RulesetRules struct {
	// Creation prevents creating matching refs.
	// +optional
	Creation *bool `json:"creation,omitempty"`
	// Update prevents updating matching refs.
	// +optional
	Update *bool `json:"update,omitempty"`
	// Deletion prevents deleting matching refs.
	// +optional
	Deletion *bool `json:"deletion,omitempty"`
	// RequiredLinearHistory prevents merge commits on matching refs.
	// +optional
	RequiredLinearHistory *bool `json:"requiredLinearHistory,omitempty"`
	// RequiredSignatures requires signed commits on matching refs.
	// +optional
	RequiredSignatures *bool `json:"requiredSignatures,omitempty"`
	// NonFastForward prevents force pushes to matching refs.
	// +optional
	NonFastForward *bool `json:"nonFastForward,omitempty"`
	// PullRequest enables pull request rules with the given parameters.
	// +optional
	PullRequest *PullRequestRuleParameters `json:"pullRequest,omitempty"`
}

// GithubOrganizationRulesetSpec defines the desired state of GithubOrganizationRuleset.
// +kubebuilder:validation:XValidation:rule="self.github == oldSelf.github",message="github is immutable"
// +kubebuilder:validation:XValidation:rule="self.organization == oldSelf.organization",message="organization is immutable"
type GithubOrganizationRulesetSpec struct {
	// Github references the Github CR that holds the GitHub App credentials.
	Github string `json:"github"`
	// Organization is the GitHub organization to manage the ruleset in.
	Organization string `json:"organization"`
	// Name is the human-readable name of the ruleset in GitHub.
	// If a ruleset with this name already exists in GitHub it will be adopted.
	Name string `json:"name"`
	// Target is the kind of resource the ruleset applies to.
	// +kubebuilder:default=branch
	// +optional
	Target RulesetTarget `json:"target,omitempty"`
	// Enforcement controls whether the ruleset is active, in evaluate mode, or disabled.
	// +kubebuilder:default=active
	Enforcement RulesetEnforcement `json:"enforcement"`
	// BypassActors is the list of actors that may bypass this ruleset.
	// +listType=atomic
	// +optional
	BypassActors []BypassActor `json:"bypassActors,omitempty"`
	// Conditions narrows which refs and repositories this ruleset applies to.
	// +optional
	Conditions *RulesetConditions `json:"conditions,omitempty"`
	// Rules defines the actual enforcement rules.
	// +optional
	Rules *RulesetRules `json:"rules,omitempty"`
}

// GithubOrganizationRulesetState is the reconciliation state of the ruleset.
type GithubOrganizationRulesetState string

const (
	GithubOrganizationRulesetStatePending GithubOrganizationRulesetState = "Pending"
	GithubOrganizationRulesetStateReady   GithubOrganizationRulesetState = "Ready"
	GithubOrganizationRulesetStateFailed  GithubOrganizationRulesetState = "Failed"
)

// GithubOrganizationRulesetStatus defines the observed state of GithubOrganizationRuleset.
type GithubOrganizationRulesetStatus struct {
	// RulesetID is the numeric GitHub ID of the managed ruleset, set once created or adopted.
	// +optional
	RulesetID *int64 `json:"rulesetId,omitempty"`
	// InstallationID is the GitHub App installation ID, persisted so the deletion
	// path does not depend on the GithubOrganization CR still being present.
	// +optional
	InstallationID *int64 `json:"installationId,omitempty"`
	// ObservedGeneration is the .metadata.generation of the spec that was last successfully reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// State is the current reconciliation state.
	// +optional
	State GithubOrganizationRulesetState `json:"state,omitempty"`
	// Message holds a human-readable description of the last reconciliation result.
	// +optional
	Message string `json:"message,omitempty"`
	// Conditions holds standard Kubernetes condition objects.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Github",type="string",JSONPath=".spec.github"
// +kubebuilder:printcolumn:name="Organization",type="string",JSONPath=".spec.organization"
// +kubebuilder:printcolumn:name="Name",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="Enforcement",type="string",JSONPath=".spec.enforcement"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="RulesetID",type="integer",JSONPath=".status.rulesetId"

// GithubOrganizationRuleset is the Schema for the githuborganizationrulesets API.
type GithubOrganizationRuleset struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   GithubOrganizationRulesetSpec   `json:"spec,omitempty"`
	Status GithubOrganizationRulesetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GithubOrganizationRulesetList contains a list of GithubOrganizationRuleset.
type GithubOrganizationRulesetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubOrganizationRuleset `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(scheme *runtime.Scheme) error {
		scheme.AddKnownTypes(GroupVersion, &GithubOrganizationRuleset{}, &GithubOrganizationRulesetList{})
		return nil
	})
}
