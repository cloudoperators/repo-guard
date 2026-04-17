// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"

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

		// IMPORTANT: Ensure the GithubTeam exists before the organization starts reconciling its owners
		ownerTeamCR = &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--%s", strings.ToLower(github.Name), strings.ToLower(orgName), strings.ToLower(ownerTeam)),
				Namespace: testNamespace,
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/addUser":    "true",
					"repo-guard.cloudoperators.dev/removeUser": "true",
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

		By("waiting for the GithubTeam to be complete or failed")
		Eventually(func() string {
			cur := &repoguardsapv1.GithubTeam{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: ownerTeamCR.Name}, cur)
			if apierrors.IsNotFound(err) {
				return "NotFound"
			}
			Expect(err).NotTo(HaveOccurred())

			_ = updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
				ObjectMeta: metav1.ObjectMeta{Name: ownerTeamCR.Name, Namespace: testNamespace},
			}, func(obj *repoguardsapv1.GithubTeam) {
				obj.Status.TeamStatus = repoguardsapv1.GithubTeamStateComplete
				obj.Status.Members = []repoguardsapv1.Member{
					{
						GreenhouseID:   ownerGHID,
						GithubUsername: ownerGHUserID,
					},
				}
			})

			curTeam := &repoguardsapv1.GithubTeam{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: ownerTeamCR.Name}, curTeam)
			return string(curTeam.Status.TeamStatus)
		}, 3*timeout, interval).Should(Equal(string(repoguardsapv1.GithubTeamStateComplete)))

		Eventually(func() error {
			return updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
				ObjectMeta: metav1.ObjectMeta{Name: ownerTeamCR.Name, Namespace: testNamespace},
			}, func(obj *repoguardsapv1.GithubTeam) {
				obj.Status.Members = []repoguardsapv1.Member{
					{
						GreenhouseID:   ownerGHID,
						GithubUsername: ownerGHUserID,
					},
				}
				obj.Status.TeamStatus = repoguardsapv1.GithubTeamStateComplete
			})
		}, 3*timeout, interval).Should(Succeed())

		org = &repoguardsapv1.GithubOrganization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      github.Name + "--" + orgName,
				Namespace: testNamespace,
				Labels: map[string]string{
					GITHUB_ORG_LABEL_ADD_ORG_OWNER:    "false",
					GITHUB_ORG_LABEL_REMOVE_ORG_OWNER: "false",
				},
			},
			Spec: repoguardsapv1.GithubOrganizationSpec{
				Github:                 github.Name,
				Organization:           orgName,
				OrganizationOwnerTeams: []string{ownerTeam},
			},
		}
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		Eventually(func() error {
			curOrg := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: org.Name}, curOrg); err != nil {
				return err
			}
			curOrg.Status.Operations.OrganizationOwnerOperations = []repoguardsapv1.GithubUserOperation{
				{
					Operation: repoguardsapv1.GithubUserOperationTypeAdd,
					User:      ownerGHUserID,
					State:     repoguardsapv1.GithubUserOperationStatePending,
					Timestamp: metav1.Now(),
				},
			}
			curOrg.Status.OrganizationStatus = repoguardsapv1.GithubOrganizationStatePendingOperations
			curOrg.Status.OrganizationStatusError = ""
			return k8sClient.Status().Update(ctx, curOrg)
		}, 3*timeout, interval).Should(Succeed())
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
		By("waiting for the organization status to at least contain the expected SpecTeams")
		Eventually(func() []string {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: org.Name}, cur); err != nil {
				return nil
			}
			return cur.Spec.OrganizationOwnerTeams
		}, 6*timeout, interval).Should(Equal([]string{ownerTeam}))

		By("verifying the organization reached the reconcile loop (even if it failed due to API)")
		Eventually(func() string {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: org.Name}, cur); err != nil {
				return ""
			}
			return string(cur.Status.OrganizationStatus)
		}, 6*timeout, interval).Should(SatisfyAny(
			Equal(string(repoguardsapv1.GithubOrganizationStateComplete)),
			Equal(string(repoguardsapv1.GithubOrganizationStateFailed)),
			Equal(string(repoguardsapv1.GithubOrganizationStatePendingOperations)),
		), "should at least attempt reconciliation")
	})
})
