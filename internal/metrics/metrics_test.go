// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestSetGithubOrganizationMetrics_ManagedTotals(t *testing.T) {
	org := &v1.GithubOrganization{
		Spec: v1.GithubOrganizationSpec{
			Github:       "github.com",
			Organization: "managed-org",
		},
		Status: v1.GithubOrganizationStatus{
			Teams: []string{"alpha", "beta", "gamma"},
			PublicRepositories: []v1.GithubRepository{
				{Name: "pub-1"},
				{Name: "pub-2"},
			},
			PrivateRepositories: []v1.GithubRepository{
				{Name: "priv-1"},
			},
			InternalRepositories: []v1.GithubRepository{
				{Name: "int-1"},
				{Name: "int-2"},
				{Name: "int-3"},
			},
		},
	}

	SetGithubOrganizationMetrics(org)

	assert.Equal(t, float64(3),
		testutil.ToFloat64(ManagedTeamsTotal.WithLabelValues("github.com", "managed-org")),
		"ManagedTeamsTotal should equal len(Teams)")

	assert.Equal(t, float64(2),
		testutil.ToFloat64(ManagedReposTotal.WithLabelValues("github.com", "managed-org", "public")),
		"ManagedReposTotal public should equal 2")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(ManagedReposTotal.WithLabelValues("github.com", "managed-org", "private")),
		"ManagedReposTotal private should equal 1")

	assert.Equal(t, float64(3),
		testutil.ToFloat64(ManagedReposTotal.WithLabelValues("github.com", "managed-org", "internal")),
		"ManagedReposTotal internal should equal 3")
}

func TestSetGithubOrganizationMetrics_PendingOperationsTotal(t *testing.T) {
	org := &v1.GithubOrganization{
		Spec: v1.GithubOrganizationSpec{
			Github:       "github.com",
			Organization: "pending-ops-org",
		},
		Status: v1.GithubOrganizationStatus{
			Operations: v1.GithubOrganizationStatusOperations{
				OrganizationOwnerOperations: []v1.GithubUserOperation{
					{Operation: v1.GithubUserOperationTypeAdd, User: "u1", State: v1.GithubUserOperationStatePending, Timestamp: metav1.Now()},
					{Operation: v1.GithubUserOperationTypeRemove, User: "u2", State: v1.GithubUserOperationStateComplete, Timestamp: metav1.Now()},
				},
				GithubTeamOperations: []v1.GithubTeamOperation{
					{Operation: v1.GithubTeamOperationTypeAdd, Team: "t1", State: v1.GithubTeamOperationStatePending, Timestamp: metav1.Now()},
					{Operation: v1.GithubTeamOperationTypeAdd, Team: "t2", State: v1.GithubTeamOperationStatePending, Timestamp: metav1.Now()},
				},
				RepositoryCollaboratorOperations: []v1.GithubRepoUserOperation{
					{Operation: v1.GithubRepoUserOperationTypeRemove, Repo: "r1", User: "c1", State: v1.GithubRepoUserOperationStatePending, Timestamp: metav1.Now()},
				},
			},
		},
	}

	SetGithubOrganizationMetrics(org)

	// 1 owner pending + 2 team pending + 1 collab pending = 4
	assert.Equal(t, float64(4),
		testutil.ToFloat64(PendingOperationsTotal.WithLabelValues("github.com", "pending-ops-org")),
		"PendingOperationsTotal should be 4 (1+2+0+0+1)")
}

func TestSetGithubTeamMetrics_ManagedMembers(t *testing.T) {
	team := &v1.GithubTeam{
		Spec: v1.GithubTeamSpec{
			Organization: "sapcc",
			Team:         "members-team",
		},
		Status: v1.GithubTeamStatus{
			Members: []v1.Member{
				{GreenhouseID: "id1", GithubUsername: "u1"},
				{GreenhouseID: "id2", GithubUsername: "u2"},
				{GreenhouseID: "id3", GithubUsername: "u3"},
			},
		},
	}

	SetGithubTeamMetrics(team)

	assert.Equal(t, float64(3),
		testutil.ToFloat64(ManagedMembersTotal.WithLabelValues("sapcc", "members-team")),
		"ManagedMembersTotal should equal len(Members)")
}

func TestObserveRateLimitHit(t *testing.T) {
	// Use unique controller names to avoid interference from other tests
	ctrl := "TestRateLimitController"

	// Read counter before
	before := testutil.ToFloat64(RateLimitHitsTotal.WithLabelValues(ctrl, "api"))

	backoff := 10 * time.Minute
	ObserveRateLimitHit(ctrl, "api", backoff)

	// Counter should have incremented
	after := testutil.ToFloat64(RateLimitHitsTotal.WithLabelValues(ctrl, "api"))
	assert.Equal(t, before+1, after, "RateLimitHitsTotal should increment by 1")

	// Collect from the histogram vec and verify sample count > 0 for our controller label
	histCh := make(chan prometheus.Metric, 32)
	RateLimitBackoffSeconds.Collect(histCh)
	close(histCh)
	found := false
	for m := range histCh {
		desc := m.Desc().String()
		if strings.Contains(desc, "ratelimit_backoff_seconds") {
			var dtoMetric dto.Metric
			if err := m.Write(&dtoMetric); err != nil {
				continue
			}
			if dtoMetric.GetHistogram().GetSampleCount() == 0 {
				continue
			}
			for _, lp := range dtoMetric.GetLabel() {
				if lp.GetName() == "controller" && lp.GetValue() == ctrl {
					found = true
				}
			}
		}
	}
	assert.True(t, found, "histogram should have at least one sample after observation")
}

func TestObserveRateLimitHit_ZeroBackoff(t *testing.T) {
	ctrl := "ZeroBackoffCtrl"

	before := testutil.ToFloat64(RateLimitHitsTotal.WithLabelValues(ctrl, "invitation"))
	ObserveRateLimitHit(ctrl, "invitation", 0)
	after := testutil.ToFloat64(RateLimitHitsTotal.WithLabelValues(ctrl, "invitation"))

	assert.Equal(t, before+1, after, "counter should still increment even with zero backoff")

	// Collect the histogram — for ZeroBackoffCtrl there should be no samples (sample count == 0 or label absent)
	histCh := make(chan prometheus.Metric, 32)
	RateLimitBackoffSeconds.Collect(histCh)
	close(histCh)
	for m := range histCh {
		var dtoMetric dto.Metric
		if err := m.Write(&dtoMetric); err != nil {
			continue
		}
		for _, lp := range dtoMetric.GetLabel() {
			if lp.GetName() == "controller" && lp.GetValue() == ctrl {
				assert.Equal(t, uint64(0), dtoMetric.GetHistogram().GetSampleCount(),
					"histogram sample count should be 0 with zero backoff")
			}
		}
	}
}
