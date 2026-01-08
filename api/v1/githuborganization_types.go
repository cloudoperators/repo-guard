/*
Copyright 2023 cc.
*/

package v1

import (
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GithubOrganizationSpec defines the desired state of GithubOrganization
type GithubOrganizationSpec struct {
	Github       string `json:"github,omitempty"`
	Organization string `json:"organization,omitempty"`

	OrganizationOwnerTeams        []string                   `json:"organizationOwnerTeams,omitempty"`
	DefaultPublicRepositoryTeams  []GithubTeamWithPermission `json:"defaultPublicRepositoryTeams,omitempty"`
	DefaultPrivateRepositoryTeams []GithubTeamWithPermission `json:"defaultPrivateRepositoryTeams,omitempty"`

	InstallationID int64 `json:"installationID,omitempty"` // TODO(onur) get installation ID from webhook

}

func GithubRepositoryListEquals(github, kubernetes []GithubRepository) bool {

	if len(github) != len(kubernetes) {
		return false
	}
	for _, g := range github {

		found := false
		for _, k := range kubernetes {

			if g.Name == k.Name {

				if !GithubTeamWithPermissionListContentSame(g.Teams, k.Teams) {
					return false
				}
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func GithubTeamWithPermissionListContentSame(github, kubernetes []GithubTeamWithPermission) bool {

	if len(github) != len(kubernetes) {
		return false
	}
	for _, g := range github {

		found := false
		for _, k := range kubernetes {

			if g.Team == k.Team {
				if g.Permission != k.Permission {
					return false
				}
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type GithubRepository struct {
	Name  string                     `json:"name,omitempty"`
	Teams []GithubTeamWithPermission `json:"teams,omitempty"`
}
type GithubTeamWithPermission struct {
	Team       string               `json:"team,omitempty"`
	Permission GithubTeamPermission `json:"permission,omitempty"`
}

type GithubTeamPermission string

const (
	GithubTeamPermissionAdmin = "admin"
	GithubTeamPermissionPush  = "push"
	GithubTeamPermissionPull  = "pull"
)

// GithubOrganizationStatus defines the observed state of GithubOrganization
type GithubOrganizationStatus struct {
	Teams               []string           `json:"teams,omitempty"`
	PublicRepositories  []GithubRepository `json:"publicRepositories,omitempty"`
	PrivateRepositories []GithubRepository `json:"privateRepositories,omitempty"`
	OrganizationOwners  []Member           `json:"organizationOwners,omitempty"`

	// OutOfPolicyRepositories is a compact representation used to reduce
	// the size of the status payload. The controller stores only repositories
	// that are out-of-policy (i.e., require changes) here, instead of persisting
	// full repository lists. This helps to avoid exceeding the etcd object size limit
	// when organizations have a very large number of repositories.
	OutOfPolicyRepositories []string `json:"outOfPolicyRepositories,omitempty"`

	OrganizationStatus          GithubOrganizationState `json:"orgStatus,omitempty"`
	OrganizationStatusError     string                  `json:"error,omitempty"`
	OrganizationStatusTimestamp metav1.Time             `json:"timestamp,omitempty"`

	Operations GithubOrganizationStatusOperations `json:"operations,omitempty"`
}

type GithubOrganizationStatusOperations struct {
	OrganizationOwnerOperations []GithubUserOperation     `json:"organizationOwnerOperations,omitempty"`
	GithubTeamOperations        []GithubTeamOperation     `json:"teamOperations,omitempty"`
	RepositoryTeamOperations    []GithubRepoTeamOperation `json:"repoOperations,omitempty"`
}
type GithubOrganizationState string

const (
	GithubOrganizationStatePendingOperations = "pending"
	GithubOrganizationStateFailed            = "failed"
	GithubOrganizationStateComplete          = "complete"
	GithubOrganizationStateDryRun            = "dry-run"
	GithubOrganizationStateRateLimited       = "ratelimited"
)

type GithubRepoTeamOperation struct {
	Operation  GithubRepoTeamOperationType  `json:"operation,omitempty"`
	Repo       string                       `json:"repo,omitempty"`
	Team       string                       `json:"team,omitempty"`
	Permission GithubTeamPermission         `json:"permission,omitempty"`
	State      GithubRepoTeamOperationState `json:"state,omitempty"`
	Error      string                       `json:"error,omitempty"`
	Timestamp  metav1.Time                  `json:"timestamp,omitempty"`
}

type GithubRepoTeamOperationType string

const (
	GithubRepoTeamOperationTypeAdd    = "add"
	GithubRepoTeamOperationTypeRemove = "remove"
)

type GithubRepoTeamOperationState string

const (
	GithubRepoTeamOperationStatePending  = "pending"
	GithubRepoTeamOperationStateComplete = "complete"
	GithubRepoTeamOperationStateFailed   = "failed"
	GithubRepoTeamOperationStateSkipped  = "skipped"
)

type GithubTeamOperation struct {
	Operation GithubTeamOperationType  `json:"operation,omitempty"`
	Team      string                   `json:"team,omitempty"`
	State     GithubUserOperationState `json:"state,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Timestamp metav1.Time              `json:"timestamp,omitempty"`
}

type GithubTeamOperationType string

const (
	GithubTeamOperationTypeAdd    = "add"
	GithubTeamOperationTypeRemove = "remove"
)

type GithubTeamOperationTypeState string

const (
	GithubTeamOperationStatePending  = "pending"
	GithubTeamOperationStateComplete = "complete"
	GithubTeamOperationStateSkipped  = "skipped"
	GithubTeamOperationStateFailed   = "failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// +kubebuilder:printcolumn:name="Github",type="string",JSONPath=".spec.github"
// +kubebuilder:printcolumn:name="Organization",type="string",JSONPath=".spec.organization"
// +kubebuilder:printcolumn:name="Org Status",type="string",JSONPath=".status.orgStatus"
// +kubebuilder:printcolumn:name="Last Change",type="date",JSONPath=".status.timestamp"
type GithubOrganization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GithubOrganizationSpec   `json:"spec,omitempty"`
	Status GithubOrganizationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GithubOrganizationList contains a list of GithubOrganization
type GithubOrganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GithubOrganization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GithubOrganization{}, &GithubOrganizationList{})
}

func (g GithubOrganization) OwnerChangeCalculator(ownersFromKubernetes []Member) (bool, *GithubOrganizationStatus) {

	newStatus := g.Status.DeepCopy()
	changed := false

	for _, kubernetesOwner := range ownersFromKubernetes {

		githubOwnerFound := false
		for _, githubOwner := range g.Status.OrganizationOwners {
			// Compare by GithubUsername to avoid mismatches between different GreenhouseID mappings
			if strings.EqualFold(githubOwner.GithubUsername, kubernetesOwner.GithubUsername) {
				githubOwnerFound = true
				break
			}
		}

		// kubernetes owner is not found in github list
		if !githubOwnerFound {
			// action: add the owner to github

			// check if there is a waiting task
			ownerOperationFound := false
			for _, ownerOpeation := range newStatus.Operations.OrganizationOwnerOperations {
				if strings.EqualFold(ownerOpeation.User, kubernetesOwner.GithubUsername) && ownerOpeation.Operation == GithubUserOperationTypeAdd && ownerOpeation.State != GithubUserOperationStateComplete {
					ownerOperationFound = true
					break
				}
			}

			if !ownerOperationFound {
				op := GithubUserOperation{
					Operation: GithubUserOperationTypeAdd,
					User:      kubernetesOwner.GithubUsername,
					State:     GithubUserOperationStatePending,
					Timestamp: metav1.Now(),
				}
				newStatus.Operations.OrganizationOwnerOperations = append(newStatus.Operations.OrganizationOwnerOperations, op)
				changed = true
			}

		}
	}

	for _, githubOwner := range g.Status.OrganizationOwners {

		kubernetesOwnerFound := false
		for _, kubernetesOwner := range ownersFromKubernetes {
			// Compare by GithubUsername to avoid mismatches between different GreenhouseID mappings
			if strings.EqualFold(kubernetesOwner.GithubUsername, githubOwner.GithubUsername) {
				kubernetesOwnerFound = true
				break
			}
		}

		// github owner is not found in kubernetes list
		if !kubernetesOwnerFound {
			// action: remove the owner from github

			// check if there is a waiting task
			ownerOperationFound := false
			for _, ownerOpeation := range newStatus.Operations.OrganizationOwnerOperations {
				if strings.EqualFold(ownerOpeation.User, githubOwner.GithubUsername) && ownerOpeation.Operation == GithubUserOperationTypeRemove && ownerOpeation.State != GithubUserOperationStateComplete {
					ownerOperationFound = true
					break
				}
			}

			if !ownerOperationFound {
				op := GithubUserOperation{
					Operation: GithubUserOperationTypeRemove,
					User:      githubOwner.GithubUsername,
					State:     GithubUserOperationStatePending,
					Timestamp: metav1.Now(),
				}
				newStatus.Operations.OrganizationOwnerOperations = append(newStatus.Operations.OrganizationOwnerOperations, op)
				changed = true
			}

		}
	}

	if changed {
		newStatus.OrganizationStatus = GithubOrganizationStatePendingOperations
		newStatus.OrganizationStatusError = ""
		newStatus.OrganizationStatusTimestamp = metav1.Now()
	}

	return changed, newStatus

}

func (g GithubOrganization) TeamChangeCalculator(teamsFromKubernetes []string) (bool, *GithubOrganizationStatus) {

	newStatus := g.Status.DeepCopy()
	changed := false

	for _, kubernetesTeam := range teamsFromKubernetes {

		kubernetesTeamFound := false
		for _, k := range g.Status.Teams {
			if strings.EqualFold(k, kubernetesTeam) {
				kubernetesTeamFound = true
				break
			}
		}

		// kubernetes team is not found in github list
		if !kubernetesTeamFound {
			// action: add the owner to github

			// check if there is a waiting task
			teamOperationFound := false
			for _, teamOpeation := range newStatus.Operations.GithubTeamOperations {
				if strings.EqualFold(teamOpeation.Team, kubernetesTeam) && teamOpeation.Operation == GithubTeamOperationTypeAdd && teamOpeation.State != GithubTeamOperationStateComplete {
					teamOperationFound = true
					break
				}
			}

			if !teamOperationFound {
				op := GithubTeamOperation{
					Operation: GithubTeamOperationTypeAdd,
					Team:      kubernetesTeam,
					State:     GithubTeamOperationStatePending,
					Timestamp: metav1.Now(),
				}
				newStatus.Operations.GithubTeamOperations = append(newStatus.Operations.GithubTeamOperations, op)
				changed = true
			}

		}
	}

	for _, githubTeam := range g.Status.Teams {

		kubernetesTeamFound := false
		for _, kubernetesTeam := range teamsFromKubernetes {
			if strings.EqualFold(kubernetesTeam, githubTeam) {
				kubernetesTeamFound = true
				break
			}
		}

		// team is not found in kubernetes list
		if !kubernetesTeamFound {
			// action: remove the team from github

			// check if there is a waiting task
			teamOperationFound := false
			for _, teamOpeation := range newStatus.Operations.GithubTeamOperations {
				if strings.EqualFold(teamOpeation.Team, githubTeam) && teamOpeation.Operation == GithubTeamOperationTypeRemove && teamOpeation.State != GithubTeamOperationStateComplete {
					teamOperationFound = true
					break
				}
			}

			if !teamOperationFound {
				op := GithubTeamOperation{
					Operation: GithubTeamOperationTypeRemove,
					Team:      githubTeam,
					State:     GithubTeamOperationStatePending,
					Timestamp: metav1.Now(),
				}

				newStatus.Operations.GithubTeamOperations = append(newStatus.Operations.GithubTeamOperations, op)
				changed = true
			}

		}
	}

	if changed {
		newStatus.OrganizationStatus = GithubOrganizationStatePendingOperations
		newStatus.OrganizationStatusError = ""
		newStatus.OrganizationStatusTimestamp = metav1.Now()
	}

	return changed, newStatus

}

func repoChangeCalculator(defaultConfig []GithubTeamWithPermission, actual []GithubRepository, exceptions []GithubTeamRepository, skipDefaultRepositoryTeams []string, operations []GithubRepoTeamOperation) []GithubRepoTeamOperation {
	newOperations := []GithubRepoTeamOperation{}

	// iterate over each repo in Github
	for _, repo := range actual {

		configExtendedWithExceptions := []GithubTeamWithPermission(defaultConfig)
		// check if the repo is in the skip list
		if slices.Contains(skipDefaultRepositoryTeams, repo.Name) {
			configExtendedWithExceptions = make([]GithubTeamWithPermission, 0)
		}

		for _, exception := range exceptions {
			if slices.Contains(exception.Spec.Repository, repo.Name) {
				configExtendedWithExceptions = append(configExtendedWithExceptions, GithubTeamWithPermission{Team: exception.Spec.Team, Permission: exception.Spec.Permission})
			}
		}

		// ensure that default teams are assigned
		for _, configTeam := range configExtendedWithExceptions {
			configTeamFound := false

			for _, team := range repo.Teams {
				if team.Team == configTeam.Team {
					configTeamFound = true
					if team.Permission != configTeam.Permission {
						// remove the team and add it with the config permission

						// check if there is a waiting task
						repoTeamOperationRemoveFound := false
						for _, op := range operations {
							if strings.EqualFold(op.Team, team.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeRemove && op.State != GithubTeamOperationStateComplete {
								repoTeamOperationRemoveFound = true
								break
							}
						}
						// if there is no waiting task, add new task
						if !repoTeamOperationRemoveFound {
							op := GithubRepoTeamOperation{
								Operation: GithubRepoTeamOperationTypeRemove,
								Team:      team.Team,
								Repo:      repo.Name,
								State:     GithubRepoTeamOperationStatePending,
								Timestamp: metav1.Now(),
							}
							newOperations = append(newOperations, op)
						}

						// add with the new permission
						repoTeamOperationAddFound := false
						for _, op := range operations {
							if strings.EqualFold(op.Team, team.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeAdd && op.State != GithubTeamOperationStateComplete {
								repoTeamOperationAddFound = true
								break
							}
						}

						if !repoTeamOperationAddFound {
							op := GithubRepoTeamOperation{
								Operation:  GithubRepoTeamOperationTypeAdd,
								Team:       configTeam.Team,
								Repo:       repo.Name,
								Permission: configTeam.Permission,
								State:      GithubRepoTeamOperationStatePending,
								Timestamp:  metav1.Now(),
							}
							newOperations = append(newOperations, op)
						}
					}
				}

			}

			if !configTeamFound {
				// add with the new permission
				repoTeamOperationAddFound := false
				for _, op := range operations {
					if strings.EqualFold(op.Team, configTeam.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeAdd && op.State != GithubTeamOperationStateComplete {
						repoTeamOperationAddFound = true
						break
					}
				}

				if !repoTeamOperationAddFound {
					op := GithubRepoTeamOperation{
						Operation:  GithubRepoTeamOperationTypeAdd,
						Team:       configTeam.Team,
						Repo:       repo.Name,
						Permission: configTeam.Permission,
						State:      GithubRepoTeamOperationStatePending,
						Timestamp:  metav1.Now(),
					}
					newOperations = append(newOperations, op)
				}
			}
		}

		// iterate over the teams in the repo
		for _, team := range repo.Teams {
			repoTeamFound := false
			for _, configTeam := range configExtendedWithExceptions {
				if team.Team == configTeam.Team {
					repoTeamFound = true
					if team.Permission != configTeam.Permission {
						// remove the team and add it with the config permission

						// check if there is a waiting task
						repoTeamOperationRemoveFound := false
						for _, op := range operations {
							if strings.EqualFold(op.Team, team.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeRemove && op.State != GithubTeamOperationStateComplete {
								repoTeamOperationRemoveFound = true
								break
							}
						}
						// if there is no waiting task, add new task
						if !repoTeamOperationRemoveFound {
							op := GithubRepoTeamOperation{
								Operation: GithubRepoTeamOperationTypeRemove,
								Team:      team.Team,
								Repo:      repo.Name,
								State:     GithubRepoTeamOperationStatePending,
								Timestamp: metav1.Now(),
							}
							newOperations = append(newOperations, op)
						}

						// add with the new permission
						repoTeamOperationAddFound := false
						for _, op := range operations {
							if strings.EqualFold(op.Team, team.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeAdd && op.State != GithubTeamOperationStateComplete {
								repoTeamOperationAddFound = true
								break
							}
						}

						if !repoTeamOperationAddFound {
							op := GithubRepoTeamOperation{
								Operation:  GithubRepoTeamOperationTypeAdd,
								Team:       configTeam.Team,
								Repo:       repo.Name,
								Permission: configTeam.Permission,
								State:      GithubRepoTeamOperationStatePending,
								Timestamp:  metav1.Now(),
							}
							newOperations = append(newOperations, op)
						}

					}
				}
			}

			if !repoTeamFound {
				// check if there is a waiting task
				repoTeamOperationRemoveFound := false
				for _, op := range operations {
					if strings.EqualFold(op.Team, team.Team) && repo.Name == op.Repo && op.Operation == GithubRepoTeamOperationTypeRemove && op.State != GithubTeamOperationStateComplete {
						repoTeamOperationRemoveFound = true
						break
					}
				}
				// if there is no waiting task, add new task
				if !repoTeamOperationRemoveFound {
					op := GithubRepoTeamOperation{
						Operation: GithubRepoTeamOperationTypeRemove,
						Team:      team.Team,
						Repo:      repo.Name,
						State:     GithubRepoTeamOperationStatePending,
						Timestamp: metav1.Now(),
					}
					newOperations = append(newOperations, op)
				}

			}

		}

	}
	return newOperations
}

func (g GithubOrganization) PendingOperationsFound() bool {

	for _, op := range g.Status.Operations.OrganizationOwnerOperations {
		if op.State == GithubUserOperationStatePending {
			return true
		}
	}

	for _, op := range g.Status.Operations.RepositoryTeamOperations {
		if op.State == GithubRepoTeamOperationStatePending {
			return true
		}
	}
	for _, op := range g.Status.Operations.GithubTeamOperations {
		if op.State == GithubTeamOperationStatePending {
			return true
		}
	}

	return false

}

func (g GithubOrganization) FailedOperationsFound() bool {

	for _, op := range g.Status.Operations.OrganizationOwnerOperations {
		if op.State == GithubUserOperationStateFailed {
			return true
		}
	}

	for _, op := range g.Status.Operations.RepositoryTeamOperations {
		if op.State == GithubRepoTeamOperationStateFailed {
			return true
		}
	}
	for _, op := range g.Status.Operations.GithubTeamOperations {
		if op.State == GithubTeamOperationStateFailed {
			return true
		}
	}

	return false

}

func (g GithubOrganization) CleanCompletedOperations() (GithubOrganizationStatus, bool) {

	newStatus := g.Status.DeepCopy()
	cleaned := false

	newOrganizationOwnerOperations := []GithubUserOperation{}
	for _, op := range g.Status.Operations.OrganizationOwnerOperations {
		if op.State == GithubUserOperationStateComplete {
			cleaned = true
		} else {
			newOrganizationOwnerOperations = append(newOrganizationOwnerOperations, op)
		}
	}

	newRepositoryTeamOperations := []GithubRepoTeamOperation{}
	for _, op := range g.Status.Operations.RepositoryTeamOperations {

		if op.State == GithubRepoTeamOperationStateComplete {
			cleaned = true
		} else {
			newRepositoryTeamOperations = append(newRepositoryTeamOperations, op)
		}
	}

	newGithubTeamOperations := []GithubTeamOperation{}
	for _, op := range g.Status.Operations.GithubTeamOperations {
		if op.State == GithubTeamOperationStateComplete {
			cleaned = true
		} else {
			newGithubTeamOperations = append(newGithubTeamOperations, op)
		}
	}

	newStatus.Operations.OrganizationOwnerOperations = newOrganizationOwnerOperations
	newStatus.Operations.RepositoryTeamOperations = newRepositoryTeamOperations
	newStatus.Operations.GithubTeamOperations = newGithubTeamOperations

	return *newStatus, cleaned

}

func (g GithubOrganization) CleanFailedOperations() (GithubOrganizationStatus, bool) {

	newStatus := g.Status.DeepCopy()
	cleaned := false

	newOrganizationOwnerOperations := []GithubUserOperation{}
	for _, op := range g.Status.Operations.OrganizationOwnerOperations {
		if op.State == GithubUserOperationStateFailed {
			cleaned = true
		} else {
			newOrganizationOwnerOperations = append(newOrganizationOwnerOperations, op)
		}
	}

	newRepositoryTeamOperations := []GithubRepoTeamOperation{}
	for _, op := range g.Status.Operations.RepositoryTeamOperations {

		if op.State == GithubRepoTeamOperationStateFailed {
			cleaned = true
		} else {
			newRepositoryTeamOperations = append(newRepositoryTeamOperations, op)
		}
	}

	newGithubTeamOperations := []GithubTeamOperation{}
	for _, op := range g.Status.Operations.GithubTeamOperations {
		if op.State == GithubTeamOperationStateFailed {
			cleaned = true
		} else {
			newGithubTeamOperations = append(newGithubTeamOperations, op)
		}
	}

	newStatus.Operations.OrganizationOwnerOperations = newOrganizationOwnerOperations
	newStatus.Operations.RepositoryTeamOperations = newRepositoryTeamOperations
	newStatus.Operations.GithubTeamOperations = newGithubTeamOperations

	return *newStatus, cleaned

}

func (g GithubOrganization) RepoChangeCalculator(exceptions []GithubTeamRepository) (bool, *GithubOrganizationStatus) {

	newStatus := g.Status.DeepCopy()
	changed := false

	if len(g.Spec.DefaultPrivateRepositoryTeams) == 0 {
		newStatus.OrganizationStatus = GithubOrganizationStateFailed
		newStatus.OrganizationStatusError = "DefaultPrivateRepositoryTeams is empty"
		newStatus.OrganizationStatusTimestamp = metav1.Now()
		return true, newStatus
	}
	if len(g.Spec.DefaultPublicRepositoryTeams) == 0 {
		newStatus.OrganizationStatus = GithubOrganizationStateFailed
		newStatus.OrganizationStatusError = "DefaultPublicRepositoryTeams is empty"
		newStatus.OrganizationStatusTimestamp = metav1.Now()
		return true, newStatus
	}

	skipList := make([]string, 0)
	if g.Annotations != nil && g.Annotations[GITHUB_ORG_ANNOTATION_SKIP_DEFAULT_TEAM_REPOSITORY] != "" {
		skipList = strings.Split(g.Annotations[GITHUB_ORG_ANNOTATION_SKIP_DEFAULT_TEAM_REPOSITORY], ",")
	}

	privateRepoOperations := repoChangeCalculator(g.Spec.DefaultPrivateRepositoryTeams, g.Status.PrivateRepositories, exceptions, skipList, g.Status.Operations.RepositoryTeamOperations)
	if len(privateRepoOperations) > 0 {
		newStatus.Operations.RepositoryTeamOperations = append(newStatus.Operations.RepositoryTeamOperations, privateRepoOperations...)
		changed = true
	}

	publicRepoOperations := repoChangeCalculator(g.Spec.DefaultPublicRepositoryTeams, g.Status.PublicRepositories, exceptions, skipList, g.Status.Operations.RepositoryTeamOperations)
	if len(publicRepoOperations) > 0 {
		newStatus.Operations.RepositoryTeamOperations = append(newStatus.Operations.RepositoryTeamOperations, publicRepoOperations...)
		changed = true
	}

	if changed {
		newStatus.OrganizationStatus = GithubOrganizationStatePendingOperations
		newStatus.OrganizationStatusError = ""
		newStatus.OrganizationStatusTimestamp = metav1.Now()
	}

	return changed, newStatus

}

const GITHUB_ORG_ANNOTATION_SKIP_DEFAULT_TEAM_REPOSITORY = "githubguard.sap/skipDefaultRepositoryTeams"
