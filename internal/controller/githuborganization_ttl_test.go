// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GithubOrganization compact status and TTL labels", Ordered, func() {

	BeforeAll(func() {

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

	})
	AfterAll(func() {

		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		Expect(k8sClient.Delete(ctx, githubOrg)).Should(Succeed())

		github := githubCom.DeepCopy()
		secret := githubComSecret.DeepCopy()
		Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

	})

	It("clears failed status and failed operations after failedTTL expires", func() {
		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()
		githubOrg.Labels[GITHUB_ORG_LABEL_FAILED_TTL] = "1s"
		Expect(k8sClient.Create(ctx, githubOrg)).Should(Succeed())

		githubOrgStatusUpdate := &githubguardsapv1.GithubOrganization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, githubOrgStatusUpdate)).To(Succeed())
		githubOrgStatusUpdate.Status = githubOrg.Status
		Expect(k8sClient.Status().Update(ctx, githubOrgStatusUpdate)).To(Succeed())

		// Update status
		Eventually(func() string {
			current := &githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, current)).To(Succeed())
			// when TTL task runs, error should be cleared and failed ops removed
			return current.Status.OrganizationStatusError
		}, timeout, interval).Should(BeEmpty())

		Eventually(func() bool {
			current := &githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, current)).To(Succeed())
			// ensure no failed operations remain
			for _, op := range current.Status.Operations.RepositoryTeamOperations {
				if op.State == githubguardsapv1.GithubRepoTeamOperationStateFailed {
					return false
				}
			}
			return true
		}, timeout, interval).Should(BeTrue())

	})

	It("clears completed operations after completedTTL expires", func() {
		ctx := context.Background()

		githubOrg := githubOrganizationGreenhouseSandboxForTTLTests.DeepCopy()

		current := &githubguardsapv1.GithubOrganization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, current)).To(Succeed())
		current.Labels[GITHUB_ORG_LABEL_COMPLETED_TTL] = "1s"
		githubOrgStatusUpdate := &githubguardsapv1.GithubOrganization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, githubOrgStatusUpdate)).To(Succeed())
		githubOrgStatusUpdate.Status = githubOrg.Status
		Expect(k8sClient.Status().Update(ctx, githubOrgStatusUpdate)).To(Succeed())

		Eventually(func() int {
			current := &githubguardsapv1.GithubOrganization{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: githubOrg.Name}, current)).To(Succeed())
			return len(current.Status.Operations.RepositoryTeamOperations) + len(current.Status.Operations.GithubTeamOperations) + len(current.Status.Operations.OrganizationOwnerOperations)
		}, timeout, interval).Should(Equal(0))
	})
})
