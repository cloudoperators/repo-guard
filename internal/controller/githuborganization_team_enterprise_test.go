// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Github Organization controller - enterprise team filtering", func() {
	var (
		ctx context.Context

		nsObj  *v1.Namespace
		secret *v1.Secret
		github *repoguardsapv1.Github
		org    *repoguardsapv1.GithubOrganization

		uniqueID      string
		uniqueNS      string
		uniqueGHName  string
		uniqueSecName string
		orgResource   string

		orgName string
	)

	BeforeEach(func() {
		ctx = context.Background()
		orgName = requireEnv("ORGANIZATION")

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNS = "ns-ent-" + uniqueID
		uniqueGHName = "gh-ent-" + uniqueID
		uniqueSecName = "sec-ent-" + uniqueID
		orgResource = "org-ent-" + uniqueID

		nsObj = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uniqueNS}}
		Expect(ensureResourceCreated(ctx, nsObj)).To(Succeed())

		github = githubCom.DeepCopy()
		github.Name = uniqueGHName
		github.Namespace = ""
		github.Spec.Secret = uniqueSecName

		secret = githubComSecret.DeepCopy()
		secret.Name = uniqueSecName
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: uniqueGHName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = orgResource
		org.Namespace = uniqueNS
		org.Spec.Github = uniqueGHName
		org.Spec.Organization = orgName

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("does not generate remove operations for enterprise-managed teams", func() {
		// The mock server seeds an enterprise team (TEST_ENTERPRISE_TEAM / "enterprise-team")
		// that has no matching GithubTeam CR.  Without the fix, the controller would
		// enqueue a REMOVE op for it and hit a 422 from the mock, causing a failed cycle.
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		// Wait for the org to finish its first reconcile.
		Eventually(func() repoguardsapv1.GithubOrganizationState {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(org), cur); err != nil {
				return ""
			}
			return cur.Status.OrganizationStatus
		}, 3*timeout, interval).Should(Or(
			Equal(repoguardsapv1.GithubOrganizationStateComplete),
			Equal(repoguardsapv1.GithubOrganizationStateRateLimited),
		))

		// Assert: no REMOVE operation for the enterprise team, and org status is not failed.
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubOrganization{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(org), cur)).To(Succeed())
			g.Expect(cur.Status.OrganizationStatus).NotTo(Equal(repoguardsapv1.GithubOrganizationStateFailed),
				"org status must not be failed — enterprise team filter may not be working")
			for _, op := range cur.Status.Operations.GithubTeamOperations {
				if strings.EqualFold(op.Team, TEST_ENTERPRISE_TEAM) &&
					op.Operation == repoguardsapv1.GithubTeamOperationTypeRemove {
					Fail(fmt.Sprintf("found unexpected REMOVE operation for enterprise team %q", TEST_ENTERPRISE_TEAM))
				}
			}
		}, 3*timeout, interval).Should(Succeed())
	})
})
