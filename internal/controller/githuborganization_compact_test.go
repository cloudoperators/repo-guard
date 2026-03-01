// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GithubOrganization compact status", func() {
	It("computes out-of-policy repositories in compact mode without storing full lists", func() {
		org := repoguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "compact-org-test",
				Namespace: TEST_ENV["NAMESPACE"],
			},
			Spec: repoguardsapv1.GithubOrganizationSpec{
				Github:       "irrelevant",
				Organization: "irrelevant",
				DefaultPublicRepositoryTeams: []repoguardsapv1.GithubTeamWithPermission{
					{Team: "team-a", Permission: repoguardsapv1.GithubTeamPermissionPull},
				},
				DefaultPrivateRepositoryTeams: []repoguardsapv1.GithubTeamWithPermission{
					{Team: "team-b", Permission: repoguardsapv1.GithubTeamPermissionPush},
				},
			},
			Status: repoguardsapv1.GithubOrganizationStatus{
				PublicRepositories:  []repoguardsapv1.GithubRepository{{Name: "repo1", Teams: []repoguardsapv1.GithubTeamWithPermission{}}},
				PrivateRepositories: []repoguardsapv1.GithubRepository{{Name: "repo2", Teams: []repoguardsapv1.GithubTeamWithPermission{}}},
			},
		}

		changed, newStatus := org.RepoChangeCalculator([]repoguardsapv1.GithubTeamRepository{})
		Expect(changed).To(BeTrue())
		names := uniquePendingOrFailedRepoNames(newStatus.Operations.RepositoryTeamOperations)
		Expect(names).To(ContainElements("repo1", "repo2"))
	})
})
