// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v83/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Team controller", func() {
	var (
		ctx context.Context

		nsObj  *v1.Namespace
		secret *v1.Secret
		github *repoguardsapv1.Github
		org    *repoguardsapv1.GithubOrganization
		gal    *repoguardsapv1.GithubAccountLink

		uniqueID               string
		uniqueNamespace        string
		uniqueGithubName       string
		uniqueGithubSecretName string
		uniqueTeamName         string
		uniqueTeamResourceName string

		orgName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")
		requireEnv("GITHUB_TOKEN")

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNamespace = "ns-team-" + uniqueID
		uniqueGithubName = "gh-team-" + uniqueID
		uniqueGithubSecretName = "sec-team-" + uniqueID
		uniqueTeamName = "tm-" + uniqueID
		uniqueTeamResourceName = fmt.Sprintf("%s--%s--%s", uniqueGithubName, orgName, uniqueTeamName)

		nsObj = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uniqueNamespace}}
		Expect(ensureResourceCreated(ctx, nsObj)).To(Succeed())

		github = githubCom.DeepCopy()
		github.Name = uniqueGithubName
		github.Namespace = ""
		github.Spec.Secret = uniqueGithubSecretName

		secret = githubComSecret.DeepCopy()
		secret.Name = uniqueGithubSecretName
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", uniqueGithubName, orgName)
		org.Namespace = uniqueNamespace
		org.Spec.Github = uniqueGithubName
		org.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		gal = githubAccountLink.DeepCopy()
		gal.Name = generateUniqueName("team-gal")
		gal.Namespace = ""
		gal.Spec.Github = uniqueGithubName
		Expect(ensureResourceCreated(ctx, gal)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: uniqueGithubName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: org.Name}, cur); err != nil {
				return false
			}
			return cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateComplete ||
				cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateRateLimited
		}, 3*timeout, interval).Should(BeTrue())

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, gal)
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("syncs greenhouse team members into GithubTeam status", func() {
		team := githubTeamTest.DeepCopy()
		team.Name = uniqueTeamResourceName
		team.Namespace = uniqueNamespace
		team.Spec.Github = uniqueGithubName
		team.Spec.Organization = orgName
		team.Spec.Team = uniqueTeamName
		team.Spec.GreenhouseTeam = uniqueTeamName
		Expect(ensureResourceCreated(ctx, team)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, team) })

		ghTeam := greenhouseTeamTest.DeepCopy()
		ghTeam.Name = uniqueTeamName
		ghTeam.Namespace = uniqueNamespace
		Expect(ensureResourceCreated(ctx, ghTeam)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, ghTeam) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: uniqueTeamName, Namespace: uniqueNamespace},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status = greenhousesapv1alpha1.TeamStatus{
				Members: []greenhousesapv1alpha1.User{{
					ID:        requireEnvOr(TEST_ENV["USER_1_GREENHOUSE_ID"], "USER_1_GREENHOUSE_ID", TEST_ENV["USER_1"]),
					Email:     "user1@example.com",
					FirstName: "User1",
					LastName:  "Test",
				}},
			}
			if obj.Labels == nil {
				obj.Labels = map[string]string{}
			}
			obj.Labels["repoguard.sap/disableInternalUsernames"] = "false"
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: uniqueTeamResourceName}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Members)
		}, 3*timeout, interval).Should(Equal(1))
	})

	AfterEach(func() {
		ctx := context.Background()
		client := githubAPI.NewClient(nil).WithAuthToken(requireEnv("GITHUB_TOKEN"))
		_, _ = client.Teams.DeleteTeamBySlug(ctx, orgName, uniqueTeamName)

		// Keep it small: enough to let reconcile settle in CI without long sleeps
		Eventually(func() bool { return true }, 200*time.Millisecond, 200*time.Millisecond).Should(BeTrue())
	})
})
