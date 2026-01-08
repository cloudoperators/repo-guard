// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

/*
Copyright 2023 cc.
*/

package v1

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GithubTeamSpec defines the desired state of GithubTeam
type GithubTeamSpec struct {
	Github                 string                        `json:"github,omitempty"`
	Organization           string                        `json:"organization,omitempty"`
	Team                   string                        `json:"team,omitempty"`
	GreenhouseTeam         string                        `json:"greenhouseTeam,omitempty"`
	ExternalMemberProvider *ExternalMemberProviderConfig `json:"externalMemberProvider,omitempty"`
}

type ExternalMemberProviderConfig struct {
	LDAP                 *GenericProvider `json:"ldap,omitempty"`
	LDAPGroupDepreceated *LDAPGroup       `json:"ldapGroup,omitempty"` // For backwards compatibility
	GenericHTTP          *GenericProvider `json:"genericHTTP,omitempty"`
	Static               *GenericProvider `json:"static,omitempty"`
}

type GenericProvider struct {
	ExternalMemberProvider string `json:"provider,omitempty"`
	Group                  string `json:"group,omitempty"`
}

type LDAPGroup struct {
	LDAPGroupProvider string `json:"ldapGroupProvider,omitempty"`
	Group             string `json:"group,omitempty"`
}

type Member struct {
	GreenhouseID   string `json:"id,omitempty"`
	GithubUsername string `json:"githubUsername,omitempty"`
}

// GithubTeamStatus defines the observed state of GithubTeam
type GithubTeamStatus struct {
	TeamStatus          GithubTeamState       `json:"teamStatus,omitempty"`
	TeamStatusError     string                `json:"error,omitempty"`
	TeamStatusTimestamp metav1.Time           `json:"timestamp,omitempty"`
	Operations          []GithubUserOperation `json:"operations,omitempty"`

	Members []Member `json:"members,omitempty"`
}

type GithubTeamState string

const (
	GithubTeamStatePendingOperations = "pending"
	GithubTeamStateFailed            = "failed"
	GithubTeamStateComplete          = "complete"
	GithubTeamStateDryRun            = "dry-run"
	GithubTeamStateRateLimited       = "ratelimited"
)

type GithubUserOperation struct {
	Operation GithubUserOperationType  `json:"operation,omitempty"`
	User      string                   `json:"user,omitempty"`
	State     GithubUserOperationState `json:"state,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Timestamp metav1.Time              `json:"timestamp,omitempty"`
}

type GithubUserOperationType string

const (
	GithubUserOperationTypeAdd    GithubUserOperationType = "add"
	GithubUserOperationTypeRemove GithubUserOperationType = "remove"
)

type GithubUserOperationState string

const (
	GithubUserOperationStatePending  = "pending"
	GithubUserOperationStateComplete = "complete"
	GithubUserOperationStateSkipped  = "skipped"
	GithubUserOperationStateFailed   = "failed"
	GithubUserOperationStateNotFound = "notfound"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Github",type="string",JSONPath=".spec.github"
//+kubebuilder:printcolumn:name="Organization",type="string",JSONPath=".spec.organization"
//+kubebuilder:printcolumn:name="Team",type="string",JSONPath=".spec.team"
//+kubebuilder:printcolumn:name="Team Status",type="string",JSONPath=".status.teamStatus"
//+kubebuilder:printcolumn:name="Last Change",type="date",JSONPath=".status.timestamp"

// GithubTeam is the Schema for the githubteams API
type GithubTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubTeamSpec   `json:"spec,omitempty"`
	Status GithubTeamStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GithubTeamList contains a list of GithubTeam
type GithubTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubTeam `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GithubTeam{}, &GithubTeamList{})
}

// Helper function to build a map of users with their operation states
func buildUserOperationStateMap(operations []GithubUserOperation) map[string]GithubUserOperationState {
	userOpStateMap := make(map[string]GithubUserOperationState)
	for _, op := range operations {
		userOpStateMap[strings.ToLower(op.User)] = op.State
	}
	return userOpStateMap
}

func (github GithubTeam) ChangeCalculator(desiredMembers []Member) (bool, *GithubTeamStatus) {
	newStatus := github.Status.DeepCopy()
	changed := false

	// Build a map of current members for quick lookup
	currentMembersMap := make(map[string]Member)
	for _, m := range github.Status.Members {
		currentMembersMap[strings.ToLower(m.GithubUsername)] = m
	}

	// Build a map of users who are in NotFound state
	userOpStateMap := buildUserOperationStateMap(newStatus.Operations)

	// Process desired members to identify additions
	for _, desiredMember := range desiredMembers {
		lowerGithubUsername := strings.ToLower(desiredMember.GithubUsername)
		// Skip users marked as NotFound
		if userOpStateMap[lowerGithubUsername] == GithubUserOperationStateNotFound {
			continue
		}

		if _, exists := currentMembersMap[lowerGithubUsername]; !exists {
			// Check if there's already a pending or completed add operation
			operationExists := false
			for _, op := range newStatus.Operations {
				if strings.EqualFold(op.User, desiredMember.GithubUsername) &&
					op.Operation == GithubUserOperationTypeAdd &&
					(op.State == GithubUserOperationStatePending ||
						op.State == GithubUserOperationStateComplete ||
						op.State == GithubUserOperationStateSkipped ||
						op.State == GithubUserOperationStateNotFound) {
					operationExists = true
					break
				}
			}
			if !operationExists {
				op := GithubUserOperation{
					Operation: GithubUserOperationTypeAdd,
					User:      desiredMember.GithubUsername,
					State:     GithubUserOperationStatePending,
					Timestamp: metav1.Now(),
				}
				newStatus.Operations = append(newStatus.Operations, op)
				changed = true
			}
		}
	}

	// Process current members to identify removals
	desiredMembersMap := make(map[string]Member)
	for _, m := range desiredMembers {
		desiredMembersMap[strings.ToLower(m.GithubUsername)] = m
	}

	for _, currentMember := range github.Status.Members {
		lowerGithubUsername := strings.ToLower(currentMember.GithubUsername)
		if _, exists := desiredMembersMap[lowerGithubUsername]; !exists {
			// Check if there's already a pending or completed remove operation
			operationExists := false
			for _, op := range newStatus.Operations {
				if strings.EqualFold(op.User, currentMember.GithubUsername) &&
					op.Operation == GithubUserOperationTypeRemove &&
					(op.State == GithubUserOperationStatePending ||
						op.State == GithubUserOperationStateComplete ||
						op.State == GithubUserOperationStateSkipped) {
					operationExists = true
					break
				}
			}
			if !operationExists {
				op := GithubUserOperation{
					Operation: GithubUserOperationTypeRemove,
					User:      currentMember.GithubUsername,
					State:     GithubUserOperationStatePending,
					Timestamp: metav1.Now(),
				}
				newStatus.Operations = append(newStatus.Operations, op)
				changed = true
			}
		}
	}

	if changed {
		newStatus.TeamStatus = GithubTeamStatePendingOperations
		newStatus.TeamStatusTimestamp = metav1.Now()
	}

	return changed, newStatus
}

func (g GithubTeam) PendingOperationsFound() bool {

	if g.Status.Operations != nil {
		for _, op := range g.Status.Operations {
			if op.State == GithubUserOperationStatePending {
				return true
			}
		}
	}
	return false
}

func (g GithubTeam) FailedOperationsFound() bool {

	if g.Status.Operations != nil {
		for _, op := range g.Status.Operations {
			if op.State == GithubUserOperationStateFailed {
				return true
			}
		}
	}
	return false
}
