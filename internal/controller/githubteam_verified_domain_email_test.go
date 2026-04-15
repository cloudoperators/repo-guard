// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Team verified domain email filtering", Ordered, func() {
	var (
		ctx context.Context
		ns  string

		orgName string

		// resources
		github *repoguardsapv1.Github
		secret *v1.Secret
		org    *repoguardsapv1.GithubOrganization
		ghTeam *greenhousesapv1alpha1.Team
		linkU0 *repoguardsapv1.GithubAccountLink
		linkU1 *repoguardsapv1.GithubAccountLink
		teamCR *repoguardsapv1.GithubTeam

		// stable identities
		u0GHID string
		u1GHID string
		u0Mail string
		u1Mail string
		u0User string
		u1User string
	)

	BeforeAll(func() {

		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")

		// Use isolated namespace to avoid cross-test interference.
		ns = generateUniqueName("ns-verified-domain")
		Expect(ensureResourceCreated(ctx, &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		})).To(Succeed())

		// Inputs: IDs from env; emails are local test values used in annotation payloads
		u0GHID = nonEmpty(TEST_ENV["USER_0_GREENHOUSE_ID"], "user0")
		u1GHID = nonEmpty(TEST_ENV["USER_1_GREENHOUSE_ID"], "user1")
		u0User = TEST_ENV["USER_0_GITHUB_USERID"]
		u1User = TEST_ENV["USER_1_GITHUB_USERID"]

		u0Mail = "user@test.com"
		u1Mail = "user@other.com"

		// Unique Github + Secret per test to avoid cross-test pollution
		ghName := generateUniqueName(nonEmpty(TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], "com"))
		secName := generateUniqueName(nonEmpty(TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"], "com-secret"))

		github = githubCom.DeepCopy()
		github.Name = ghName
		github.Namespace = ""
		github.Spec.Secret = secName

		secret = githubComSecret.DeepCopy()
		secret.Name = secName
		secret.Namespace = TestOperatorNamespace

		// Create Secret BEFORE Github so Github can become Running deterministically
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		// Wait Github Running
		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: github.Name}, cur)
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		// ---- IMPORTANT FIX ----
		// GithubTeam reconciler looks up org by "<githubName>--<orgName>" (no random suffix).
		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", github.Name, orgName)
		org.Namespace = ns
		org.Spec.Github = github.Name
		org.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		// Ensure org reaches a steady state (Complete or RateLimited) so team reconciliation can proceed.
		Eventually(func() repoguardsapv1.GithubOrganizationState {
			cur := &repoguardsapv1.GithubOrganization{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: org.Name}, cur)
			if apierrors.IsNotFound(err) {
				return ""
			}
			if err != nil {
				return ""
			}
			return cur.Status.OrganizationStatus
		}, 3*timeout, interval).Should(SatisfyAny(
			BeEquivalentTo(repoguardsapv1.GithubOrganizationStateComplete),
			BeEquivalentTo(repoguardsapv1.GithubOrganizationStateRateLimited),
		))

		// Greenhouse Team
		ghTeam = &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateUniqueName("gh-team-verified-domain"),
				Namespace: ns,
			},
			Spec: greenhousesapv1alpha1.TeamSpec{},
		}
		Expect(ensureResourceCreated(ctx, ghTeam)).To(Succeed())

		// Set status deterministically: two members, with different domains
		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: ghTeam.Name, Namespace: ns},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status.Members = []greenhousesapv1alpha1.User{
				{ID: u0GHID, Email: u0Mail},
				{ID: u1GHID, Email: u1Mail},
			}
		})).To(Succeed())

		// Account links with unique names
		linkU0 = &repoguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateUniqueName("link-u0"),
			},
			Spec: repoguardsapv1.GithubAccountLinkSpec{
				Github:           github.Name,
				GreenhouseUserID: u0GHID,
				GithubUserID:     u0User,
			},
		}
		Expect(ensureResourceCreated(ctx, linkU0)).To(Succeed())

		linkU1 = &repoguardsapv1.GithubAccountLink{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateUniqueName("link-u1"),
			},
			Spec: repoguardsapv1.GithubAccountLinkSpec{
				Github:           github.Name,
				GreenhouseUserID: u1GHID,
				GithubUserID:     u1User,
			},
		}
		Expect(ensureResourceCreated(ctx, linkU1)).To(Succeed())

		// Wait for AccountLinks to be visible in cache before continuing
		Eventually(func() error {
			var l0, l1 repoguardsapv1.GithubAccountLink
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: linkU0.Name}, &l0); err != nil {
				return err
			}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: linkU1.Name}, &l1); err != nil {
				return err
			}
			return nil
		}, 3*timeout, interval).Should(Succeed())

		// Pre-annotate links to avoid race condition during first reconciliation
		now := time.Now().UTC().Format(time.RFC3339)
		emailChecksTest := map[string]map[string]any{
			orgName: {"domain": "test.com", "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_VERIFIED, "timestamp": now},
		}
		emailChecksOther := map[string]map[string]any{
			orgName: {"domain": "other.com", "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_VERIFIED, "timestamp": now},
		}
		bTest, _ := json.Marshal(emailChecksTest)
		bOther, _ := json.Marshal(emailChecksOther)

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU0.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bTest)
			})
		}, 3*timeout, interval).Should(Succeed())

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU1.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bOther)
			})
		}, 3*timeout, interval).Should(Succeed())
	})

	AfterAll(func() {
		_ = deleteIgnoreNotFound(ctx, k8sClient, teamCR)
		_ = deleteIgnoreNotFound(ctx, k8sClient, linkU1)
		_ = deleteIgnoreNotFound(ctx, k8sClient, linkU0)
		_ = deleteIgnoreNotFound(ctx, k8sClient, ghTeam)
		_ = deleteIgnoreNotFound(ctx, k8sClient, org)
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
		_ = deleteIgnoreNotFound(ctx, k8sClient, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	})

	It("filters members by verified domain", func() {
		requiredDomain := "test.com"

		now := time.Now().UTC().Format(time.RFC3339)
		emailChecksTest := map[string]map[string]any{
			orgName: {"domain": requiredDomain, "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_VERIFIED, "timestamp": now},
		}
		emailChecksOther := map[string]map[string]any{
			orgName: {"domain": "other.com", "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_VERIFIED, "timestamp": now},
		}

		bTest, err := json.Marshal(emailChecksTest)
		Expect(err).NotTo(HaveOccurred())
		bOther, err := json.Marshal(emailChecksOther)
		Expect(err).NotTo(HaveOccurred())

		// Annotate links with email checks
		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU0.Name}, // link is cluster-scoped
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bTest)
			})
		}, 3*timeout, interval).Should(Succeed(), "failed to annotate linkU0")

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU1.Name}, // link is cluster-scoped
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bOther)
			})
		}, 3*timeout, interval).Should(Succeed(), "failed to annotate linkU1")

		teamName := generateUniqueName("team-verified-domain")

		teamCR = &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--%s", github.Name, orgName, teamName),
				Namespace: ns,
				Labels: map[string]string{
					// the thing under test
					"repoguard.sap/require-verified-domain-email": requiredDomain,

					// prevent accidental side effects in CI / shared org
					"repoguard.sap/dryRun":     "true",
					"repoguard.sap/addUser":    "true",
					"repoguard.sap/removeUser": "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           teamName,
				GreenhouseTeam: ghTeam.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, teamCR)).To(Succeed())

		// Expect exactly one member (test.com) and it must be u0 user
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubTeam{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: teamCR.Name}, cur)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cur.Status.Members).To(SatisfyAll(
				HaveLen(1),
				ContainElement(HaveField("GreenhouseID", u0GHID)),
			))
		}, 5*timeout, interval).Should(Succeed())

		// Flip required domain to other.com and ensure the allowed user changes to u1
		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
				ObjectMeta: metav1.ObjectMeta{Name: teamCR.Name, Namespace: ns},
			}, func(obj *repoguardsapv1.GithubTeam) {
				if obj.Labels == nil {
					obj.Labels = map[string]string{}
				}
				obj.Labels["repoguard.sap/require-verified-domain-email"] = "other.com"
			})
		}, 3*timeout, interval).Should(Succeed(), "failed to update team domain")

		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubTeam{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: teamCR.Name}, cur)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cur.Status.Members).To(SatisfyAll(
				HaveLen(1),
				ContainElement(HaveField("GreenhouseID", u1GHID)),
			))
		}, 5*timeout, interval).Should(Succeed())
	})

	It("allows members with status 'not-part-of-org'", func() {
		requiredDomain := "test.com"
		now := time.Now().UTC().Format(time.RFC3339)

		// Link U0: verified
		// Link U1: not-part-of-org
		emailChecksU0 := map[string]map[string]any{
			orgName: {"domain": requiredDomain, "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_VERIFIED, "timestamp": now},
		}
		emailChecksU1 := map[string]map[string]any{
			orgName: {"domain": requiredDomain, "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_NOT_PART_OF_ORG, "timestamp": now},
		}

		bU0, _ := json.Marshal(emailChecksU0)
		bU1, _ := json.Marshal(emailChecksU1)

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU0.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bU0)
			})
		}, 3*timeout, interval).Should(Succeed())

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU1.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bU1)
			})
		}, 3*timeout, interval).Should(Succeed())

		teamName := generateUniqueName("team-not-part-of-org")
		teamCR2 := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--%s", github.Name, orgName, teamName),
				Namespace: ns,
				Labels: map[string]string{
					"repoguard.sap/require-verified-domain-email": requiredDomain,
					"repoguard.sap/dryRun":                        "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           teamName,
				GreenhouseTeam: ghTeam.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, teamCR2)).To(Succeed())
		defer func() { _ = deleteIgnoreNotFound(ctx, k8sClient, teamCR2) }()

		// Both users should be included:
		// u0 because it's VERIFIED
		// u1 because it's NOT_PART_OF_ORG
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubTeam{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: teamCR2.Name}, cur)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cur.Status.Members).To(HaveLen(2))
			g.Expect(cur.Status.Members).To(ContainElement(HaveField("GreenhouseID", u0GHID)))
			g.Expect(cur.Status.Members).To(ContainElement(HaveField("GreenhouseID", u1GHID)))
		}, 5*timeout, interval).Should(Succeed())
	})

	It("supports legacy verified field in results annotation", func() {
		requiredDomain := "test.com"
		now := time.Now().UTC().Format(time.RFC3339)

		// linkU0: Legacy format with only "verified": true
		emailChecksU0 := map[string]map[string]any{
			orgName: {"domain": requiredDomain, "verified": true, "timestamp": now},
		}
		bU0, _ := json.Marshal(emailChecksU0)

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU0.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bU0)
			})
		}, 3*timeout, interval).Should(Succeed())

		// linkU1: Explicitly not verified
		emailChecksU1 := map[string]map[string]any{
			orgName: {"domain": requiredDomain, "status": repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_VERIFIED_DOMAIN_STATUS_NO, "timestamp": now},
		}
		bU1, _ := json.Marshal(emailChecksU1)

		Eventually(func() error {
			return updateObjectWithRetry(ctx, k8sClient, &repoguardsapv1.GithubAccountLink{
				ObjectMeta: metav1.ObjectMeta{Name: linkU1.Name},
			}, func(obj *repoguardsapv1.GithubAccountLink) {
				if obj.Annotations == nil {
					obj.Annotations = map[string]string{}
				}
				obj.Annotations[repoguardsapv1.GITHUB_ACCOUNT_LINK_EMAIL_CHECK_RESULTS] = string(bU1)
			})
		}, 3*timeout, interval).Should(Succeed())

		teamName := generateUniqueName("team-legacy-verified")
		teamCRLegacy := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s--%s--%s", github.Name, orgName, teamName),
				Namespace: ns,
				Labels: map[string]string{
					"repoguard.sap/require-verified-domain-email": requiredDomain,
					"repoguard.sap/dryRun":                        "true",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:         github.Name,
				Organization:   orgName,
				Team:           teamName,
				GreenhouseTeam: ghTeam.Name,
			},
		}
		Expect(ensureResourceCreated(ctx, teamCRLegacy)).To(Succeed())
		defer func() { _ = deleteIgnoreNotFound(ctx, k8sClient, teamCRLegacy) }()

		// Expect only u0 (legacy verified) to be in members
		Eventually(func(g Gomega) {
			cur := &repoguardsapv1.GithubTeam{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: teamCRLegacy.Name}, cur)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cur.Status.Members).To(SatisfyAll(
				HaveLen(1),
				ContainElement(HaveField("GreenhouseID", u0GHID)),
			))
		}, 5*timeout, interval).Should(Succeed())
	})
})
