// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GithubOrganization TTL labels maintenance", Ordered, func() {
	var (
		ctx    context.Context
		github *repoguardsapv1.Github
		secret *v1.Secret
	)

	BeforeAll(func() {
		ctx = context.Background()

		// Use unique names to avoid interference
		ghName := generateUniqueName("gh-org-ttl")
		secName := generateUniqueName("sec-org-ttl")

		// Ensure operator namespace exists (needed for github secret)
		operatorNsObj := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: TestOperatorNamespace}}
		_ = ensureResourceCreated(ctx, operatorNsObj)

		// Create secret in operator namespace with EXACT expected name
		secret = githubComSecret.DeepCopy()
		secret.Name = secName
		secret.Namespace = TestOperatorNamespace
		secret.ObjectMeta.Namespace = TestOperatorNamespace
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())

		// Create cluster-scoped Github first, so we know expected secret name
		github = githubCom.DeepCopy()
		github.Name = ghName
		github.Namespace = ""
		github.ObjectMeta.Namespace = ""
		github.Spec.Secret = secName
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

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
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
	})

	It("clears failed status and failed operations after failedTTL expires", func() {
		ctx := context.Background()

		org := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		org.Name = generateUniqueName("ttl-failed")
		org.Spec.Github = github.Name
		if org.Labels == nil {
			org.Labels = map[string]string{}
		}
		org.Labels[GITHUB_ORG_LABEL_FAILED_TTL] = "1s"

		Expect(ensureResourceCreated(ctx, org)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, org) })

		// Force known failed status into status subresource
		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{Name: org.Name, Namespace: org.Namespace},
		}, func(cur *repoguardsapv1.GithubOrganization) {
			cur.Status = org.Status
		})).To(Succeed())

		Eventually(func() string {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: org.Namespace, Name: org.Name}, cur); err != nil {
				return "error-get"
			}
			return cur.Status.OrganizationStatusError
		}, 3*timeout, interval).Should(BeEmpty())
	})

	It("clears completed operations after completedTTL expires", func() {
		ctx := context.Background()

		org := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		org.Name = generateUniqueName("ttl-completed")
		if org.Labels == nil {
			org.Labels = map[string]string{}
		}
		org.Labels[GITHUB_ORG_LABEL_COMPLETED_TTL] = "1s"

		Expect(ensureResourceCreated(ctx, org)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, org) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{Name: org.Name, Namespace: org.Namespace},
		}, func(cur *repoguardsapv1.GithubOrganization) {
			cur.Status = org.Status
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: org.Namespace, Name: org.Name}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Operations.RepositoryTeamOperations) +
				len(cur.Status.Operations.GithubTeamOperations) +
				len(cur.Status.Operations.OrganizationOwnerOperations)
		}, 3*timeout, interval).Should(Equal(0))
	})
})
