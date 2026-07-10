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
		_ = ensureResourceCreated(ctx, operatorNsObj)

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

		testOrg := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		testOrg.Name = generateUniqueName("force-reconcile-org")
		testOrg.Spec.Github = github.Name
		if testOrg.Labels == nil {
			testOrg.Labels = map[string]string{}
		}
		testOrg.Labels[GITHUB_ORG_LABEL_FORCE_RECONCILE] = GITHUB_ORG_LABEL_FORCE_RECONCILE_VALUE
		Expect(ensureResourceCreated(ctx, testOrg)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, testOrg) })

		// The controller should remove the label and clear the status.
		// Once the label is gone, the forceReconcile logic has fired.
		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testOrg.Namespace, Name: testOrg.Name}, cur); err != nil {
				return true
			}
			return cur.Labels[GITHUB_ORG_LABEL_FORCE_RECONCILE] == GITHUB_ORG_LABEL_FORCE_RECONCILE_VALUE
		}, 3*timeout, interval).Should(BeFalse())
	})

	It("clears GithubTeam status and re-reconciles when forceReconcile label is set", func() {
		ctx := context.Background()

		teamName := "team-force-reconcile"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/addUser":    "false",
					"repo-guard.cloudoperators.dev/removeUser": "false",
					GITHUB_TEAM_LABEL_FORCE_RECONCILE:          GITHUB_TEAM_LABEL_FORCE_RECONCILE_VALUE,
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

		// The controller should remove the label and clear the status.
		// Once the label is gone, the forceReconcile logic has fired.
		Eventually(func() bool {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur); err != nil {
				return true
			}
			return cur.Labels[GITHUB_TEAM_LABEL_FORCE_RECONCILE] == GITHUB_TEAM_LABEL_FORCE_RECONCILE_VALUE
		}, 3*timeout, interval).Should(BeFalse())
	})
})
