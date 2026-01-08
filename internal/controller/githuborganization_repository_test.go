// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	githubAPI "github.com/google/go-github/v81/github"
)

const (
	TEST_DEFAULT_PUBLIC_REPO_PULL  = "public-pull-team"
	TEST_DEFAULT_PUBLIC_REPO_PUSH  = "public-push-team"
	TEST_DEFAULT_PUBLIC_REPO_ADMIN = "public-admin-team"

	TEST_DEFAULT_PRIVATE_REPO_PULL  = "private-pull-team"
	TEST_DEFAULT_PRIVATE_REPO_PUSH  = "private-push-team"
	TEST_DEFAULT_PRIVATE_REPO_ADMIN = "private-admin-team"

	TEST_CUSTOM_TEAM = "custom-team-for-private-repo"
)

var TEST_REPOSITORY_PUBLIC = "repository-1-" + fmt.Sprintf("%d", GinkgoRandomSeed())
var TEST_REPOSITORY_PRIVATE = "repository-2-" + fmt.Sprintf("%d", GinkgoRandomSeed())

var _ = Describe("Github Organization controller - repository team assignments", Ordered, func() {

	var client *githubAPI.Client

	BeforeAll(func() {

		client = githubAPI.NewClient(nil).WithAuthToken(TEST_ENV["GITHUB_TOKEN"])

		ctx := context.Background()
		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Create(ctx, github)).Should(Succeed())

		Eventually(func() githubguardsapv1.GithubState {
			github := &githubguardsapv1.Github{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
			Expect(err).NotTo(HaveOccurred())
			return github.Status.State
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

		_, response, err := client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PUBLIC_REPO_PULL})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))
		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PUBLIC_REPO_PUSH})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))
		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PUBLIC_REPO_ADMIN})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))

		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PRIVATE_REPO_PULL})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))
		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PRIVATE_REPO_PUSH})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))
		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_DEFAULT_PRIVATE_REPO_ADMIN})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))

		_, response, err = client.Teams.CreateTeam(ctx, TEST_ENV["ORGANIZATION"], githubAPI.NewTeam{Name: TEST_CUSTOM_TEAM})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))

		_, response, err = client.Repositories.Create(ctx, TEST_ENV["ORGANIZATION"], &githubAPI.Repository{Name: &TEST_REPOSITORY_PUBLIC})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))
		_, response, err = client.Repositories.Create(ctx, TEST_ENV["ORGANIZATION"], &githubAPI.Repository{Name: &TEST_REPOSITORY_PRIVATE})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(201))

	})
	AfterAll(func() {

		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForRepositoryTests.DeepCopy()
		Expect(k8sClient.Delete(ctx, githubOrg)).Should(Succeed())

		githubTeamRepository := githubTeamRepository.DeepCopy()
		Expect(k8sClient.Delete(ctx, githubTeamRepository)).Should(Succeed())

		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

		_, err := client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PUBLIC_REPO_PULL)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PUBLIC_REPO_PUSH)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PUBLIC_REPO_ADMIN)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PRIVATE_REPO_PULL)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PRIVATE_REPO_PUSH)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_DEFAULT_PRIVATE_REPO_ADMIN)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_CUSTOM_TEAM)
		Expect(err).NotTo(HaveOccurred())

		response, err := client.Repositories.Delete(ctx, TEST_ENV["ORGANIZATION"], TEST_REPOSITORY_PUBLIC)
		Expect(response.StatusCode).To(Equal(204))
		Expect(err).NotTo(HaveOccurred())

		response, err = client.Repositories.Delete(ctx, TEST_ENV["ORGANIZATION"], TEST_REPOSITORY_PRIVATE)
		Expect(response.StatusCode).To(Equal(204))
		Expect(err).NotTo(HaveOccurred())

	})

	It("Private and public repositories should have assigned teams", func() {

		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForRepositoryTests.DeepCopy()
		Expect(k8sClient.Create(ctx, githubOrg)).Should(Succeed())

		githubTeamRepository := githubTeamRepository.DeepCopy()
		Expect(k8sClient.Create(ctx, githubTeamRepository)).Should(Succeed())

		Eventually(func() githubguardsapv1.GithubOrganizationState {
			orgDeployed := githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			return orgDeployed.Status.OrganizationStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubOrganizationStateComplete))

		Eventually(func() []githubguardsapv1.GithubRepoTeamOperation {
			orgDeployed := githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			return orgDeployed.Status.Operations.RepositoryTeamOperations
		}, timeout, interval).ShouldNot(BeEmpty())

		repositoryTeams, response, err := client.Repositories.ListTeams(ctx, TEST_ENV["ORGANIZATION"], TEST_REPOSITORY_PUBLIC, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(200))
		Expect(repositoryTeams).To(HaveLen(3))

		repositoryTeams, response, err = client.Repositories.ListTeams(ctx, TEST_ENV["ORGANIZATION"], TEST_REPOSITORY_PRIVATE, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(200))
		Expect(repositoryTeams).To(HaveLen(4)) // There are 3 default + 1 custom team assigned

	})
})
