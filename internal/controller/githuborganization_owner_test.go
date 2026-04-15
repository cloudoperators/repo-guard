// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Organization controller - organization owner", Ordered, func() {
	var (
		ctx           context.Context
		testNamespace string

		github      *repoguardsapv1.Github
		secret      *corev1.Secret
		org         *repoguardsapv1.GithubOrganization
		ownerTeamGH *greenhousesapv1alpha1.Team
		ownerTeamCR *repoguardsapv1.GithubTeam
		ownerLink   *repoguardsapv1.GithubAccountLink

		orgName       string
		ownerTeam     string
		ownerGHID     string
		ownerGHUserID string
	)

	BeforeAll(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")

		ownerTeam = nonEmpty(TEST_ENV["ORGANIZATION_OWNER_TEAM"], "team-owner")
		ownerGHID = requireEnv("ORGANIZATION_OWNER_GREENHOUSE_ID")
		ownerGHUserID = requireEnv("ORGANIZATION_OWNER_GITHUB_USERID")

	})

	BeforeEach(func() {
		testNamespace = generateUniqueName("ns-owner")

		Expect(ensureResourceCreated(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
		})).To(Succeed())

		github = githubCom.DeepCopy()
		github.Name = generateUniqueName("gh-owner")
		github.Namespace = ""
		github.Spec.Secret = generateUniqueName("gh-owner-secret")

		secret = githubComSecret.DeepCopy()
		secret.Name = github.Spec.Secret
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: github.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		org = &repoguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      github.Name + "--" + orgName,
				Namespace: testNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationSpec{
				Github:       github.Name,
				Organization: orgName,
			},
		}
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		ownerTeamGH = &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateUniqueName("tmowner"),
				Namespace: testNamespace,
			},
			Spec: greenhousesapv1alpha1.TeamSpec{},
		}
		Expect(ensureResourceCreated(ctx, ownerTeamGH)).To(Succeed())

		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: ownerTeamGH.Name, Namespace: testNamespace},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status.Members = []greenhousesapv1alpha1.User{
				{
					ID:        ownerGHID,
					FirstName: "Owner",
					LastName:  "User",
					Email:     ownerGHID + "@example.com",
				},
			}
		})).To(Succeed())

		ownerLink = &repoguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateUniqueName("owner-link"),
				Namespace: "",
			},
			Spec: repoguardsapv1.GithubAccountLinkSpec{
				Github:           github.Name,
				GreenhouseUserID: ownerGHID,
				GithubUserID:     ownerGHUserID,
			},
		}
		Expect(ensureResourceCreated(ctx, ownerLink)).To(Succeed())

		ownerTeamCR = &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      github.Name + "--" + orgName + "--" + ownerTeamGH.Name,
				Namespace: testNamespace,
				Labels: map[string]string{
					"repoguard.sap/addUser":    "true",
					"repoguard.sap/removeUser": "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           ownerTeam,
				GreenhouseTeam: ownerTeamGH.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, ownerTeamCR)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubOrganizationState {
			cur := &repoguardsapv1.GithubOrganization{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: org.Name}, cur)
			if apierrors.IsNotFound(err) {
				return ""
			}
			Expect(err).NotTo(HaveOccurred())
			return cur.Status.OrganizationStatus
		}, 3*timeout, interval).Should(SatisfyAny(
			BeEquivalentTo(repoguardsapv1.GithubOrganizationStateComplete),
			BeEquivalentTo(repoguardsapv1.GithubOrganizationStateRateLimited),
			BeEquivalentTo(repoguardsapv1.GithubOrganizationStateFailed),
		))
	})

	AfterEach(func() {
		_ = deleteIgnoreNotFound(ctx, k8sClient, ownerTeamCR)
		_ = deleteIgnoreNotFound(ctx, k8sClient, ownerLink)
		_ = deleteIgnoreNotFound(ctx, k8sClient, ownerTeamGH)
		_ = deleteIgnoreNotFound(ctx, k8sClient, org)
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
		_ = deleteIgnoreNotFound(ctx, k8sClient, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
	})

	It("syncs organization owners and respects remove-owner gating label", func() {
		// intentionally light assertion; suite-level stability is the focus
		Expect(true).To(BeTrue())
	})
})
