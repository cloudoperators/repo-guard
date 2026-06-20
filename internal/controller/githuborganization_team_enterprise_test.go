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
		// Disable actual team removal so that this test does not mutate the shared mock
		// state and does not loop waiting for other tests' teams to disappear from the org.
		// The test only needs to verify that no REMOVE op is *generated* for the enterprise
		// team — which is equally observable when removals are skipped vs. executed.
		org.Labels["repo-guard.cloudoperators.dev/removeTeam"] = "false"

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("does not generate remove operations for enterprise-managed teams", func() {
		if !isMockMode() {
			Skip("relies on mock-seeded enterprise team; only valid in mock mode")
		}
		// The mock server seeds an enterprise team (TEST_ENTERPRISE_TEAM / "enterprise-team")
		// with type="enterprise".  teams.go:List() must filter it out before it reaches
		// TeamChangeCalculator, so it must never appear in status.Teams and no REMOVE op
		// must ever be generated for it.
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		// Wait until the controller has fetched the team list at least once
		// (status.Teams becomes non-empty when the first reconcile completes its
		// team-list phase, regardless of any pending/skipped operations).
		Eventually(func() int {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(org), cur); err != nil {
				return 0
			}
			return len(cur.Status.Teams)
		}, 3*timeout, interval).Should(BeNumerically(">", 0))

		// Snapshot the status right after the first team-list fetch.
		cur := &repoguardsapv1.GithubOrganization{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(org), cur)).To(Succeed())

		// enterprise-team must not appear in the team list returned by the mock.
		for _, team := range cur.Status.Teams {
			if strings.EqualFold(team, TEST_ENTERPRISE_TEAM) {
				Fail(fmt.Sprintf("enterprise team %q must be filtered by teams.List() and must not appear in status.Teams", TEST_ENTERPRISE_TEAM))
			}
		}
		// No REMOVE op must have been generated for the enterprise team.
		for _, op := range cur.Status.Operations.GithubTeamOperations {
			if strings.EqualFold(op.Team, TEST_ENTERPRISE_TEAM) &&
				op.Operation == repoguardsapv1.GithubTeamOperationTypeRemove {
				Fail(fmt.Sprintf("found unexpected REMOVE operation for enterprise team %q", TEST_ENTERPRISE_TEAM))
			}
		}
	})
})
