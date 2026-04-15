// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Controller Singleton Multi-Namespace support", Ordered, func() {
	var (
		ctx context.Context

		ns1 string
		ns2 string

		github *repoguardsapv1.Github
		secret *v1.Secret

		orgName string
	)

	BeforeAll(func() {
		ctx = context.Background()
		orgName = requireEnv("ORGANIZATION")

		// Create two distinct namespaces
		ns1 = generateUniqueName("ns-singleton-1")
		Expect(ensureResourceCreated(ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns1},
		})).To(Succeed())

		ns2 = generateUniqueName("ns-singleton-2")
		Expect(ensureResourceCreated(ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns2},
		})).To(Succeed())

		// Create a single Cluster-scoped Github resource
		ghName := generateUniqueName("gh-singleton")
		secName := generateUniqueName("sec-singleton")

		github = githubCom.DeepCopy()
		github.Name = ghName
		github.Namespace = ""
		github.Spec.Secret = secName

		secret = githubComSecret.DeepCopy()
		secret.Name = secName
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		// Wait Github Running
		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: github.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))
	})

	AfterAll(func() {
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
		_ = deleteIgnoreNotFound(ctx, k8sClient, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns1}})
		_ = deleteIgnoreNotFound(ctx, k8sClient, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns2}})
	})

	It("handles GithubOrganizations in multiple namespaces simultaneously", func() {
		// Create Org in NS1
		org1 := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org1.Name = fmt.Sprintf("%s--%s", github.Name, orgName)
		org1.Namespace = ns1
		org1.Spec.Github = github.Name
		org1.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org1)).To(Succeed())

		// Create Org in NS2
		org2 := githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org2.Name = fmt.Sprintf("%s--%s", github.Name, orgName)
		org2.Namespace = ns2
		org2.Spec.Github = github.Name
		org2.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org2)).To(Succeed())

		// Verify both reach steady state
		for _, o := range []*repoguardsapv1.GithubOrganization{org1, org2} {
			Eventually(func() repoguardsapv1.GithubOrganizationState {
				cur := &repoguardsapv1.GithubOrganization{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: o.Namespace, Name: o.Name}, cur); err != nil {
					return ""
				}
				return cur.Status.OrganizationStatus
			}, 3*timeout, interval).Should(SatisfyAny(
				BeEquivalentTo(repoguardsapv1.GithubOrganizationStateComplete),
				BeEquivalentTo(repoguardsapv1.GithubOrganizationStateRateLimited),
			))
		}
	})

	It("handles GithubTeams in multiple namespaces simultaneously", func() {
		// We'll create one Team in each namespace, linked to the same Greenhouse Team (shared status)
		// but using their respective local GithubOrganizations if needed (though GithubTeam looks up org by name pattern)

		ghTeamName := generateUniqueName("gh-shared-team")
		ghTeam := &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ghTeamName,
				Namespace: ns1, // GH team in ns1
			},
			Spec: greenhousesapv1alpha1.TeamSpec{},
		}
		Expect(ensureResourceCreated(ctx, ghTeam)).To(Succeed())

		// Setup Greenhouse Team members
		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: ghTeam.Name, Namespace: ns1},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status.Members = []greenhousesapv1alpha1.User{
				{ID: "user-singleton", Email: "user@singleton.com"},
			}
		})).To(Succeed())

		// Setup second Greenhouse Team in ns2
		ghTeam2 := &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ghTeamName,
				Namespace: ns2,
			},
			Spec: greenhousesapv1alpha1.TeamSpec{},
		}
		Expect(ensureResourceCreated(ctx, ghTeam2)).To(Succeed())

		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: ghTeam2.Name, Namespace: ns2},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status.Members = []greenhousesapv1alpha1.User{
				{ID: "user-singleton", Email: "user@singleton.com"},
			}
		})).To(Succeed())

		// Setup AccountLink (Cluster-scoped)
		link := &repoguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateUniqueName("link-singleton"),
			},
			Spec: repoguardsapv1.GithubAccountLinkSpec{
				Github:           github.Name,
				GreenhouseUserID: "user-singleton",
				GithubUserID:     "12345", // must be numeric string for internal resolution
			},
		}
		Expect(ensureResourceCreated(ctx, link)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, link) })

		// Create GithubTeam in NS1
		gt1 := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--gt1", github.Name, orgName),
				Namespace: ns1,
				Labels: map[string]string{
					"repoguard.sap/dryRun":     "true",
					"repoguard.sap/addUser":    "true",
					"repoguard.sap/removeUser": "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           "team-ns1",
				GreenhouseTeam: ghTeam.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, gt1)).To(Succeed())

		// Create GithubTeam in NS2
		gt2 := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--gt2", github.Name, orgName),
				Namespace: ns2,
				Labels: map[string]string{
					"repoguard.sap/dryRun":     "true",
					"repoguard.sap/addUser":    "true",
					"repoguard.sap/removeUser": "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           "team-ns2",
				GreenhouseTeam: ghTeam.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, gt2)).To(Succeed())

		// Verify both teams reconcile correctly
		for _, gt := range []*repoguardsapv1.GithubTeam{gt1, gt2} {
			Eventually(func() int {
				cur := &repoguardsapv1.GithubTeam{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: gt.Namespace, Name: gt.Name}, cur); err != nil {
					return -1
				}
				return len(cur.Status.Members)
			}, 3*timeout, interval).Should(Equal(1))
		}
	})
})
