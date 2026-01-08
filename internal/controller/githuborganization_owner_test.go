// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"k8s.io/apimachinery/pkg/api/errors"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v81/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Organization controller - organization owner", Ordered, func() {

	BeforeAll(func() {

		ctx := context.Background()
		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		if err := k8sClient.Create(ctx, github); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(func() githubguardsapv1.GithubState {
			github := &githubguardsapv1.Github{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
			Expect(err).NotTo(HaveOccurred())
			return github.Status.State
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

		// create greenhouse team
		greenhouseTeam := greenhouseTeamOwnerTest.DeepCopy()
		Expect(k8sClient.Create(ctx, greenhouseTeam)).Should(Succeed())

		// update greenhouse team status - members
		greenhouseTeamToStatusUpdate := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM"]}, greenhouseTeamToStatusUpdate)).Should(Succeed())
		greenhouseTeamToStatusUpdate.Status = greenhouseTeamOwnerTest.Status
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToStatusUpdate)).Should(Succeed())

		githubAccountLinkOrgOwner := githubAccountLinkOrgOwner.DeepCopy()
		Expect(k8sClient.Create(ctx, githubAccountLinkOrgOwner)).Should(Succeed())
		githubAccountLinkLDAP := githubAccountLinkForExternalMemberProviderLDAP.DeepCopy()
		Expect(k8sClient.Create(ctx, githubAccountLinkLDAP)).Should(Succeed())
	})
	AfterAll(func() {

		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForOrganizationTests.DeepCopy()
		Expect(k8sClient.Delete(ctx, githubOrg)).Should(Succeed())

		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

		greenhouseTeam := greenhouseTeamOwnerTest.DeepCopy()
		Expect(k8sClient.Delete(ctx, greenhouseTeam)).Should(Succeed())

		client := githubAPI.NewClient(nil).WithAuthToken(TEST_ENV["GITHUB_TOKEN"])
		response, err := client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["ORGANIZATION_OWNER_TEAM"])
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(204))

		role := "member"
		_, response, err = client.Organizations.EditOrgMembership(ctx, TEST_ENV["USER_1_GITHUB_USERNAME"], TEST_ENV["ORGANIZATION"], &githubAPI.Membership{Role: &role})
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(200))
	})

	It("Should sync organization owners", func() {

		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForOrganizationTests.DeepCopy()
		Expect(k8sClient.Create(ctx, githubOrg)).Should(Succeed())

		team := githubTeamOwnerTest.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		Eventually(func() int {
			ownerTeamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &ownerTeamDeployed)).Should(Succeed())
			return len(ownerTeamDeployed.Status.Members)
		}, timeout, interval).Should(BeEquivalentTo(1))

		Eventually(func() githubguardsapv1.GithubTeamState {
			ownerTeamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &ownerTeamDeployed)).Should(Succeed())
			return ownerTeamDeployed.Status.TeamStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubTeamStateComplete))

		Eventually(func() githubguardsapv1.GithubOrganizationState {
			orgDeployed := githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			return orgDeployed.Status.OrganizationStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubOrganizationStateComplete))

		orgDeployed := githubguardsapv1.GithubOrganization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
		Expect(orgDeployed.Status.OrganizationOwners).To(HaveLen(1))
		Expect(orgDeployed.Status.OrganizationOwners[0].GreenhouseID).Should(Equal(TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"]))

		// Update organization owner team in Greenhouse side
		greenhouseTeamToBeUpdated := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM"]}, greenhouseTeamToBeUpdated)).Should(Succeed())
		greenhouseTeamToBeUpdated.Status.Members = append(greenhouseTeamToBeUpdated.Status.Members, greenhousesapv1alpha1.User{
			ID:        TEST_ENV["USER_1_GREENHOUSE_ID"],
			Email:     "user1@example.com",
			FirstName: "User1",
			LastName:  "Test",
		})
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToBeUpdated)).Should(Succeed())

		Eventually(func() githubguardsapv1.GithubTeamState {
			ownerTeamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &ownerTeamDeployed)).Should(Succeed())
			return ownerTeamDeployed.Status.TeamStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubTeamStateComplete))

		Eventually(func() []githubguardsapv1.Member {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			return teamDeployed.Status.Members
		}, timeout, interval).Should(HaveLen(2))

		Eventually(func() []githubguardsapv1.Member {
			orgDeployed := githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			return orgDeployed.Status.OrganizationOwners
		}, timeout, interval).Should(HaveLen(2))

		// Owners should stay as it is when sync is disabled - "githubguard.sap/removeOrganizationOwner"
		orgDeployed = githubguardsapv1.GithubOrganization{}
		Eventually(func() error {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			orgDeployed.Labels[GITHUB_ORG_LABEL_REMOVE_ORG_OWNER] = "false"
			return k8sClient.Update(ctx, &orgDeployed)
		}, timeout, interval).Should(Succeed())

		greenhouseTeamToBeUpdated = &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM"]}, greenhouseTeamToBeUpdated)).Should(Succeed())
		greenhouseTeamToBeUpdated.Status.Members = make([]greenhousesapv1alpha1.User, 0)
		// Use Greenhouse ID here; the team controller maps Greenhouse IDs to GitHub usernames via GithubAccountLink
		greenhouseTeamToBeUpdated.Status.Members = append(greenhouseTeamToBeUpdated.Status.Members, greenhousesapv1alpha1.User{ID: TEST_ENV["ORGANIZATION_OWNER_GREENHOUSE_ID"], Email: "owner@example.com", FirstName: "Owner", LastName: "User"})
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToBeUpdated)).Should(Succeed())

		// Team should be updated but not organization owners
		Eventually(func() []githubguardsapv1.Member {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_OWNER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			return teamDeployed.Status.Members
		}, timeout, interval).Should(HaveLen(1))

		Eventually(func() []githubguardsapv1.Member {
			orgDeployed := githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, &orgDeployed)).Should(Succeed())
			return orgDeployed.Status.OrganizationOwners
		}, timeout, interval).Should(HaveLen(2))
	})

})
