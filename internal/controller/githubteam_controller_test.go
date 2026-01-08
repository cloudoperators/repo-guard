// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"os"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	githubAPI "github.com/google/go-github/v81/github"
)

var _ = Describe("Github Team controller", Ordered, func() {

	BeforeAll(func() {

		ctx := context.Background()
		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(k8sClient.Create(ctx, github)).Should(Succeed())

		org := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		Expect(k8sClient.Create(ctx, org)).Should(Succeed())

		// Ensure GithubAccountLink for USER_1 exists so the controller can deterministically
		// map the GitHub login to the expected GreenhouseID in Status.Members.
		// This prevents race conditions where the first reconcile happens before the link exists.
		gal := githubAccountLink.DeepCopy()
		if err := k8sClient.Create(ctx, gal); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		Eventually(func() githubguardsapv1.GithubState {
			github := &githubguardsapv1.Github{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
			Expect(err).NotTo(HaveOccurred())
			return github.Status.State
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

		Eventually(func() githubguardsapv1.GithubOrganizationState {
			org := &githubguardsapv1.GithubOrganization{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["ORGANIZATION_KUBERNETES_RESOURCE_NAME"]}, org)
			Expect(err).NotTo(HaveOccurred())
			return org.Status.OrganizationStatus
		}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubOrganizationStateComplete))

	})

	It("Should sync with github", func() {

		ctx := context.Background()

		team := githubTeamTest.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		// create greenhouse team
		greenhouseTeam := greenhouseTeamTest.DeepCopy()
		Expect(k8sClient.Create(ctx, greenhouseTeam)).Should(Succeed())

		// update greenhouse team status - members
		greenhouseTeamToStatusUpdate := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1"]}, greenhouseTeamToStatusUpdate)).Should(Succeed())
		greenhouseTeamToStatusUpdate.Status = greenhouseTeamTest.Status
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToStatusUpdate)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			//pretty.Println(teamDeployed.Spec.Members)
			if len(teamDeployed.Status.Members) == 1 { // there is only one member
				//pretty.Println(teamDeployed.Spec.Members)
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["USER_1_GREENHOUSE_ID"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

	})
	It("New Greenhouse member should be added to Github", func() {

		ctx := context.Background()

		team := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1"]}, team)).Should(Succeed())
		team.Status.Members = append(team.Status.Members, greenhousesapv1alpha1.User{
			ID:        TEST_ENV["USER_2"],
			Email:     "user2@example.com",
			FirstName: "User2",
			LastName:  "Test",
		}) // second member is added to greenhouse

		Expect(k8sClient.Status().Update(ctx, team)).Should(Succeed())

		Eventually(func() []githubguardsapv1.Member {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			//pretty.Println(teamDeployed.Status)
			return teamDeployed.Status.Members
		}, timeout, interval).Should(HaveLen(2))

	})
	It("Removed Greenhouse member should be removed from Github", func() {

		ctx := context.Background()

		team := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1"]}, team)).Should(Succeed())

		team.Status.Members = []greenhousesapv1alpha1.User{team.Status.Members[1]} // first user is removed

		Expect(k8sClient.Status().Update(ctx, team)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["USER_2"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

	})
	It("Disable adding user by labels", func() {

		ctx := context.Background()

		Eventually(func() error {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			teamDeployed.Labels = map[string]string{
				GITHUB_TEAMS_LABEL_ADD_USER: "false",
			}
			return k8sClient.Update(ctx, &teamDeployed)
		}, timeout, interval).Should(Succeed())

		team := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1"]}, team)).Should(Succeed())
		team.Status.Members = append(team.Status.Members, greenhousesapv1alpha1.User{
			ID:        "asd",
			Email:     "asd@example.com",
			FirstName: "Asd",
			LastName:  "User",
		})

		Expect(k8sClient.Status().Update(ctx, team)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["USER_2"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

	})
	It("Disable removing user by labels", func() {

		ctx := context.Background()

		Eventually(func() error {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			teamDeployed.Labels = map[string]string{
				GITHUB_TEAMS_LABEL_REMOVE_USER: "false",
			}
			return k8sClient.Update(ctx, &teamDeployed)
		}, timeout, interval).Should(Succeed())

		team := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1"]}, team)).Should(Succeed())
		team.Status.Members = []greenhousesapv1alpha1.User{} // remove all users

		Expect(k8sClient.Status().Update(ctx, team)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_1_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["USER_2"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

	})
	It("User with external Github username should sync with github", func() {

		ctx := context.Background()

		githubAccountLink := githubAccountLink.DeepCopy()
		if err := k8sClient.Create(ctx, githubAccountLink); err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		team := githubTeamTestWithExternalUsername.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		// create greenhouse team
		greenhouseTeam := greenhouseTeamTestWithExternalUsername.DeepCopy()
		Expect(k8sClient.Create(ctx, greenhouseTeam)).Should(Succeed())

		// update greenhouse team status - members
		greenhouseTeamToStatusUpdate := &greenhousesapv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_2"]}, greenhouseTeamToStatusUpdate)).Should(Succeed())
		greenhouseTeamToStatusUpdate.Status = greenhouseTeamTestWithExternalUsername.Status
		Expect(k8sClient.Status().Update(ctx, greenhouseTeamToStatusUpdate)).Should(Succeed())

		Eventually(func() int {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_2_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())
			return len(teamDeployed.Status.Members)
		}, timeout, interval).Should(Equal(1))

		Eventually(func() string {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["TEAM_2_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())

			return teamDeployed.Status.Members[0].GithubUsername
		}, timeout, interval).Should(Equal(TEST_ENV["USER_1_GITHUB_USERNAME"]))

		Expect(k8sClient.Delete(ctx, githubAccountLink)).Should(Succeed())

	})

	It("User with external member provider Generic HTTP should sync with github", func() {

		if os.Getenv("SKIP_EMP_HTTP_TESTS") != "" {
			Skip("SKIP_EMP_HTTP_TESTS")
		}

		ctx := context.Background()

		// Create External Member Provider HTTP secret and resource
		secret := empHTTPSecret.DeepCopy()
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

		emp := empHTTP.DeepCopy()
		Expect(k8sClient.Create(ctx, emp)).Should(Succeed())

		// Create the team using Generic provider
		team := githubTeamTestWithExternalMemberProviderHTTP.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		Eventally := Eventually // alias to avoid shadowing
		Eventally(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["EMP_HTTP_TEAM_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())

			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["EMP_HTTP_USER_INTERNAL_USERNAME"]))
				Expect(teamDeployed.Status.Members[0].GithubUsername).Should(Equal(TEST_ENV["EMP_HTTP_USER_GITHUB_USERNAME"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

		// Cleanup resources created in this test to avoid leaking into other specs
		Expect(k8sClient.Delete(ctx, team)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, emp)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
	})

	It("User with external member provider Static should sync with github", func() {
		ctx := context.Background()

		// Create External Member Provider Static resource
		emp := empStatic.DeepCopy()
		Expect(k8sClient.Create(ctx, emp)).Should(Succeed())

		// Create the team using Generic->Static provider
		team := githubTeamTestWithExternalMemberProviderStatic.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["EMP_STATIC_TEAM_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())

			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["EMP_STATIC_USER_INTERNAL_USERNAME"]))
				Expect(teamDeployed.Status.Members[0].GithubUsername).Should(Equal(TEST_ENV["EMP_STATIC_USER_GITHUB_USERNAME"]))
				return true
			}
			return false
		}, timeout, interval).Should(BeTrue())

	})

	It("User with external member provider LDAP should sync with github", func() {
		ctx := context.Background()

		ldapGroupProviderSecret := ldapGroupProviderSecret.DeepCopy()
		Expect(k8sClient.Create(ctx, ldapGroupProviderSecret)).Should(Succeed())

		ldapGroupProvider := ldapGroupProvider.DeepCopy()
		Expect(k8sClient.Create(ctx, ldapGroupProvider)).Should(Succeed())

		githubAccountLink := githubAccountLinkForExternalMemberProviderLDAP.DeepCopy()
		Expect(k8sClient.Create(ctx, githubAccountLink)).Should(Succeed())

		team := githubTeamTestWithExternalMemberProviderLDAP.DeepCopy()
		Expect(k8sClient.Create(ctx, team)).Should(Succeed())

		Eventually(func() bool {
			teamDeployed := githubguardsapv1.GithubTeam{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_KUBERNETES_RESOURCE_NAME"]}, &teamDeployed)).Should(Succeed())

			if len(teamDeployed.Status.Members) == 1 {
				Expect(teamDeployed.Status.Members[0].GreenhouseID).Should(Equal(TEST_ENV["LDAP_GROUP_PROVIDER_USER_INTERNAL_USERNAME"]))
				Expect(teamDeployed.Status.Members[0].GithubUsername).Should(Equal(TEST_ENV["LDAP_GROUP_PROVIDER_USER_GITHUB_USERNAME"]))
				return true
			}
			return false

		}, timeout, interval).Should(BeTrue())

		Expect(k8sClient.Delete(ctx, githubAccountLink)).Should(Succeed())

	})

	AfterAll(func() {

		ctx := context.Background()

		// Cleanup GithubAccountLink possibly created in BeforeAll if not already removed by specs
		gal := githubAccountLink.DeepCopy()
		_ = k8sClient.Delete(ctx, gal) // ignore error; may have been deleted by tests

		team1 := githubTeamTest.DeepCopy()
		Expect(k8sClient.Delete(ctx, team1)).Should(Succeed())
		team2 := githubTeamTestWithExternalUsername.DeepCopy()
		Expect(k8sClient.Delete(ctx, team2)).Should(Succeed())

		teamStatic := githubTeamTestWithExternalMemberProviderStatic.DeepCopy()
		Expect(k8sClient.Delete(ctx, teamStatic)).Should(Succeed())
		empStatic := empStatic.DeepCopy()
		Expect(k8sClient.Delete(ctx, empStatic)).Should(Succeed())

		teamLDAP := githubTeamTestWithExternalMemberProviderLDAP.DeepCopy()
		Expect(k8sClient.Delete(ctx, teamLDAP)).Should(Succeed())
		ldapGroupProvider := ldapGroupProvider.DeepCopy()
		Expect(k8sClient.Delete(ctx, ldapGroupProvider)).Should(Succeed())
		ldapGroupProviderSecret := ldapGroupProviderSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, ldapGroupProviderSecret)).Should(Succeed())

		org := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		Expect(k8sClient.Delete(ctx, org)).Should(Succeed())

		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

		client := githubAPI.NewClient(nil).WithAuthToken(TEST_ENV["GITHUB_TOKEN"])
		response, err := client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["TEAM_1"])
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(204))

		response, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["TEAM_2"])
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(204))

		// Cleanup teams created by External Member Provider tests
		if os.Getenv("SKIP_EMP_HTTP_TESTS") == "" {
			response, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["EMP_HTTP_TEAM_NAME"])
			if err != nil {
				if gerr, ok := err.(*githubAPI.ErrorResponse); ok && gerr.Response != nil && gerr.Response.StatusCode == 404 {
					// Team not found is acceptable during cleanup
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			} else {
				Expect(response.StatusCode).To(Equal(204))
			}
		}
		response, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["EMP_STATIC_TEAM_NAME"])
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(204))

		response, err = client.Teams.DeleteTeamBySlug(ctx, TEST_ENV["ORGANIZATION"], TEST_ENV["LDAP_GROUP_PROVIDER_TEAM_NAME"])
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(204))
	})
})
