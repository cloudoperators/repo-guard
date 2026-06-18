// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"strings"
	"testing"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestSetGithubOrganizationMetrics(t *testing.T) {
	org := &v1.GithubOrganization{
		Spec: v1.GithubOrganizationSpec{
			Github:       "github.com",
			Organization: "sapcc",
		},
		Status: v1.GithubOrganizationStatus{
			OrganizationStatus: v1.GithubOrganizationStateFailed,
			Operations: v1.GithubOrganizationStatusOperations{
				GithubTeamOperations: []v1.GithubTeamOperation{
					{
						Operation: v1.GithubTeamOperationTypeAdd,
						Team:      "team1",
						State:     v1.GithubUserOperationStatePending,
					},
				},
				OrganizationMemberOperations: []v1.GithubUserOperation{
					{
						Operation: v1.GithubUserOperationTypeRemove,
						User:      "user1",
						State:     v1.GithubUserOperationStatePending,
					},
				},
				RepositoryCollaboratorOperations: []v1.GithubRepoUserOperation{
					{
						Operation: v1.GithubRepoUserOperationTypeRemove,
						Repo:      "repo1",
						User:      "user2",
						State:     v1.GithubRepoUserOperationStatePending,
					},
				},
			},
		},
	}

	SetGithubOrganizationMetrics(org)

	// Check GithubOrganizationStatus
	// We expect status="failed" to be 1, others to be 0
	expectedStatus := `
# HELP repo_guard_githuborganization_status Current status of a GithubOrganization resource (one-hot gauge).
# TYPE repo_guard_githuborganization_status gauge
repo_guard_githuborganization_status{github="github.com",organization="sapcc",status="complete"} 0
repo_guard_githuborganization_status{github="github.com",organization="sapcc",status="dry-run"} 0
repo_guard_githuborganization_status{github="github.com",organization="sapcc",status="failed"} 1
repo_guard_githuborganization_status{github="github.com",organization="sapcc",status="pending"} 0
repo_guard_githuborganization_status{github="github.com",organization="sapcc",status="ratelimited"} 0
`
	err := testutil.CollectAndCompare(GithubOrganizationStatus, strings.NewReader(expectedStatus), "repo_guard_githuborganization_status")
	assert.NoError(t, err)

	// Check GithubOrganizationOperations
	// We expect teams/add/pending to be 1
	// The dashboard query for "Org Pending Ops" is:
	// sum(repo_guard_githuborganization_operations{state="pending", github=~"$github", organization=~"$org"}) or vector(0)

	count := testutil.ToFloat64(GithubOrganizationOperations.WithLabelValues("github.com", "sapcc", "teams", "add", "pending"))
	assert.Equal(t, 1.0, count)

	// Check orgmembers scope (#147)
	countOrgMembers := testutil.ToFloat64(GithubOrganizationOperations.WithLabelValues("github.com", "sapcc", "orgmembers", "remove", "pending"))
	assert.Equal(t, 1.0, countOrgMembers)

	// Check repocollaborators scope (#146)
	countRepoCollab := testutil.ToFloat64(GithubOrganizationOperations.WithLabelValues("github.com", "sapcc", "repocollaborators", "remove", "pending"))
	assert.Equal(t, 1.0, countRepoCollab)
}

func TestSetGithubTeamMetrics(t *testing.T) {
	team := &v1.GithubTeam{
		Spec: v1.GithubTeamSpec{
			Organization: "sapcc",
			Team:         "team1",
		},
		Status: v1.GithubTeamStatus{
			TeamStatus: v1.GithubTeamStatePendingOperations,
			Operations: []v1.GithubUserOperation{
				{
					Operation: v1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     v1.GithubUserOperationStatePending,
				},
			},
		},
	}

	SetGithubTeamMetrics(team)

	// Check GithubTeamStatus
	expectedStatus := `
# HELP repo_guard_githubteam_status Current status of a GithubTeam resource (one-hot gauge).
# TYPE repo_guard_githubteam_status gauge
repo_guard_githubteam_status{organization="sapcc",status="complete",team="team1"} 0
repo_guard_githubteam_status{organization="sapcc",status="dry-run",team="team1"} 0
repo_guard_githubteam_status{organization="sapcc",status="failed",team="team1"} 0
repo_guard_githubteam_status{organization="sapcc",status="pending",team="team1"} 1
repo_guard_githubteam_status{organization="sapcc",status="ratelimited",team="team1"} 0
`
	err := testutil.CollectAndCompare(GithubTeamStatus, strings.NewReader(expectedStatus), "repo_guard_githubteam_status")
	assert.NoError(t, err)

	// Check GithubTeamOperations
	count := testutil.ToFloat64(GithubTeamOperations.WithLabelValues("sapcc", "team1", "add", "pending"))
	assert.Equal(t, 1.0, count)
}
