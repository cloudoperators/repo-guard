// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("forceReconcile label", Ordered, func() {
	var (
		ctx    context.Context
		github *repoguardsapv1.Github
		secret *v1.Secret
		org    *repoguardsapv1.GithubOrganization
	)

	BeforeAll(func() {
		ctx = context.Background()

		ghName := generateUniqueName("gh-force-reconcile")
		secName := generateUniqueName("sec-force-reconcile")

		operatorNsObj := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: TestOperatorNamespace}}
		Expect(ensureResourceCreated(ctx, operatorNsObj)).To(Succeed())

		secret = githubComSecret.DeepCopy()
		secret.Name = secName
		secret.Namespace = TestOperatorNamespace
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())

		github = githubCom.DeepCopy()
		github.Name = ghName
		github.Namespace = ""
		github.Spec.Secret = secName
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", ghName, TEST_ENV["ORGANIZATION"])
		org.Spec.Github = ghName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: github.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))
	})

	AfterAll(func() {
		ctx := context.Background()
		_ = deleteIgnoreNotFound(ctx, k8sClient, org)
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
	})

	It("clears GithubOrganization status and re-reconciles when forceReconcile label is set", func() {
		ctx := context.Background()

		const sentinelError = "force-reconcile-sentinel-error"

		testOrg := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		testOrg.Name = generateUniqueName("force-reconcile-org")
		testOrg.Spec.Github = github.Name
		if testOrg.Labels == nil {
			testOrg.Labels = map[string]string{}
		}
		Expect(ensureResourceCreated(ctx, testOrg)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, testOrg) })

		// Seed a failed status with a sentinel error so we can verify it is wiped.
		Expect(updateStatusWithRetry(ctx, k8sClient, testOrg, func(cur *repoguardsapv1.GithubOrganization) {
			cur.Status.OrganizationStatus = repoguardsapv1.GithubOrganizationStateFailed
			cur.Status.OrganizationStatusError = sentinelError
		})).To(Succeed())

		// Set the forceReconcile label; the controller should wipe the status and remove the label.
		Expect(labelWithRetry(ctx, k8sClient, testOrg, GITHUB_ORG_LABEL_FORCE_RECONCILE, GITHUB_ORG_LABEL_FORCE_RECONCILE_VALUE)).To(Succeed())

		// Once the label is gone the forceReconcile path has run. Assert both that the label key
		// is absent (not merely set to a different value) and that the sentinel error is cleared
		// (status was actually wiped).
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubOrganization{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOrg.Namespace, Name: testOrg.Name}, cur)).To(Succeed())
			g.Expect(cur.Labels).NotTo(HaveKey(GITHUB_ORG_LABEL_FORCE_RECONCILE))
			g.Expect(cur.Status.OrganizationStatusError).NotTo(Equal(sentinelError))
		}, 3*timeout, interval).Should(Succeed())
	})

	It("clears GithubTeam status and re-reconciles when forceReconcile label is set", func() {
		ctx := context.Background()

		const sentinelError = "force-reconcile-sentinel-error"

		teamName := "team-force-reconcile"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/addUser":    "false",
					"repo-guard.cloudoperators.dev/removeUser": "false",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       github.Name,
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         teamName,
			},
		}
		Expect(ensureResourceCreated(ctx, t)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, t) })

		// Seed a failed status with a sentinel error so we can verify it is wiped.
		Expect(updateStatusWithRetry(ctx, k8sClient, t, func(cur *repoguardsapv1.GithubTeam) {
			cur.Status.TeamStatus = repoguardsapv1.GithubTeamStateFailed
			cur.Status.TeamStatusError = sentinelError
		})).To(Succeed())

		// Set the forceReconcile label; the controller should wipe the status and remove the label.
		Expect(labelWithRetry(ctx, k8sClient, t, GITHUB_TEAM_LABEL_FORCE_RECONCILE, GITHUB_TEAM_LABEL_FORCE_RECONCILE_VALUE)).To(Succeed())

		// Once the label is gone the forceReconcile path has run. Assert both that the label key
		// is absent (not merely set to a different value) and that the sentinel error is cleared
		// (status was actually wiped).
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubTeam{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur)).To(Succeed())
			g.Expect(cur.Labels).NotTo(HaveKey(GITHUB_TEAM_LABEL_FORCE_RECONCILE))
			g.Expect(cur.Status.TeamStatusError).NotTo(Equal(sentinelError))
		}, 3*timeout, interval).Should(Succeed())
	})
})
