package controller

import (
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("GithubOrganization compact status", func() {

	It("computes out-of-policy repositories in compact mode without storing full lists", func() {
		// Build a minimal GithubOrganization object with default teams and a repo missing teams
		org := githubguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "compact-org-test",
				Namespace: TEST_ENV["NAMESPACE"],
			},
			Spec: githubguardsapv1.GithubOrganizationSpec{
				Github:                        "irrelevant",
				Organization:                  "irrelevant",
				DefaultPublicRepositoryTeams:  []githubguardsapv1.GithubTeamWithPermission{{Team: "team-a", Permission: githubguardsapv1.GithubTeamPermissionPull}},
				DefaultPrivateRepositoryTeams: []githubguardsapv1.GithubTeamWithPermission{{Team: "team-b", Permission: githubguardsapv1.GithubTeamPermissionPush}},
			},
			Status: githubguardsapv1.GithubOrganizationStatus{
				PublicRepositories:  []githubguardsapv1.GithubRepository{{Name: "repo1", Teams: []githubguardsapv1.GithubTeamWithPermission{}}},
				PrivateRepositories: []githubguardsapv1.GithubRepository{{Name: "repo2", Teams: []githubguardsapv1.GithubTeamWithPermission{}}},
			},
		}

		// Calculate repo changes and ensure we can derive out-of-policy repository names
		changed, newStatus := org.RepoChangeCalculator([]githubguardsapv1.GithubTeamRepository{})
		Expect(changed).To(BeTrue())
		names := uniquePendingOrFailedRepoNames(newStatus.Operations.RepositoryTeamOperations)
		Expect(names).To(ContainElements("repo1", "repo2"))
	})

})
