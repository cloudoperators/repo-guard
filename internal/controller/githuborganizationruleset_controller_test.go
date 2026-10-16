// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GithubOrganizationRuleset controller", func() {
	var (
		ctx context.Context

		nsObj  *v1.Namespace
		secret *v1.Secret
		ghCR   *repoguardsapv1.Github
		org    *repoguardsapv1.GithubOrganization

		uniqueID               string
		uniqueNamespace        string
		uniqueGithubName       string
		uniqueGithubSecretName string
		orgName                string
		orgCRName              string
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNamespace = "ns-ruleset-" + uniqueID
		uniqueGithubName = "gh-ruleset-" + uniqueID
		uniqueGithubSecretName = "sec-ruleset-" + uniqueID
		orgCRName = fmt.Sprintf("%s--%s", uniqueGithubName, orgName)

		nsObj = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uniqueNamespace}}
		Expect(ensureResourceCreated(ctx, nsObj)).To(Succeed())

		ghCR = githubCom.DeepCopy()
		ghCR.Name = uniqueGithubName
		ghCR.Namespace = ""
		ghCR.Spec.Secret = uniqueGithubSecretName

		secret = githubComSecret.DeepCopy()
		secret.Name = uniqueGithubSecretName
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, ghCR)).To(Succeed())

		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = orgCRName
		org.Namespace = uniqueNamespace
		org.Spec.Github = uniqueGithubName
		org.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: uniqueGithubName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: orgCRName}, cur); err != nil {
				return false
			}
			return cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateComplete ||
				cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateRateLimited
		}, 3*timeout, interval).Should(BeTrue())

		DeferCleanup(func() {
			// Delete all rulesets in this namespace first and wait for their finalizers
			// to complete before removing the org/credentials — otherwise the finalizer
			// can't call the GitHub API and the namespace stalls during teardown.
			rsList := &repoguardsapv1.GithubOrganizationRulesetList{}
			if err := k8sClient.List(ctx, rsList, client.InNamespace(uniqueNamespace)); err == nil {
				for i := range rsList.Items {
					_ = deleteIgnoreNotFound(ctx, k8sClient, &rsList.Items[i])
				}
				// Wait for all rulesets to be fully gone before deleting credentials.
				Eventually(func() int {
					cur := &repoguardsapv1.GithubOrganizationRulesetList{}
					if err := k8sClient.List(ctx, cur, client.InNamespace(uniqueNamespace)); err != nil {
						return -1
					}
					return len(cur.Items)
				}, 3*timeout, interval).Should(Equal(0))
			}
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, ghCR)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("creates a ruleset on GitHub and reaches Ready state", func() {
		rulesetName := "test-require-pr-" + uniqueID
		rsResourceName := "ruleset-create-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
				Target:       repoguardsapv1.RulesetTargetBranch,
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
				Conditions: &repoguardsapv1.RulesetConditions{
					RefName: &repoguardsapv1.RulesetRefCondition{
						Include: []string{"~DEFAULT_BRANCH"},
					},
					RepositoryName: &repoguardsapv1.RulesetRepositoryNameCondition{
						// Use a uniquely-scoped pattern so this ruleset never applies to
						// real repositories in live mode; "~ALL" would affect every repo.
						Include: []string{"rg-test-" + uniqueID + "*"},
					},
				},
				Rules: &repoguardsapv1.RulesetRules{
					PullRequest: &repoguardsapv1.PullRequestRuleParameters{
						RequiredApprovingReviewCount: 1,
					},
				},
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, rs) })

		Eventually(func() repoguardsapv1.GithubOrganizationRulesetState {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateReady))

		cur := &repoguardsapv1.GithubOrganizationRuleset{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur)).To(Succeed())
		Expect(cur.Status.RulesetID).NotTo(BeNil())
		Expect(*cur.Status.RulesetID).To(BeNumerically(">", 0))
		Expect(cur.Status.Message).To(Equal("ruleset synced"))
		// Finalizer must be present.
		Expect(cur.Finalizers).To(ContainElement(rulesetFinalizer))
	})

	It("updates an existing ruleset when spec changes", func() {
		rulesetName := "test-update-rs-" + uniqueID
		rsResourceName := "ruleset-update-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
				Target:       repoguardsapv1.RulesetTargetBranch,
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, rs) })

		// Wait for initial Ready.
		Eventually(func() repoguardsapv1.GithubOrganizationRulesetState {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateReady))

		// Change enforcement to disabled.
		Expect(updateObjectWithRetry(ctx, k8sClient, rs, func(cur *repoguardsapv1.GithubOrganizationRuleset) {
			cur.Spec.Enforcement = repoguardsapv1.RulesetEnforcementDisabled
		})).To(Succeed())

		// Controller should re-reconcile and remain Ready.
		Eventually(func() repoguardsapv1.GithubOrganizationRulesetState {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateReady))
	})

	It("deletes the GitHub ruleset when the CR is deleted", func() {
		rulesetName := "test-delete-rs-" + uniqueID
		rsResourceName := "ruleset-delete-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
				Target:       repoguardsapv1.RulesetTargetBranch,
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())

		// Wait until the ruleset is created on GitHub (status.rulesetID set).
		var rulesetID int64
		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return false
			}
			if cur.Status.RulesetID == nil {
				return false
			}
			rulesetID = *cur.Status.RulesetID
			return cur.Status.State == repoguardsapv1.GithubOrganizationRulesetStateReady
		}, 3*timeout, interval).Should(BeTrue())
		Expect(rulesetID).To(BeNumerically(">", 0))

		// Delete the CR — the controller should remove it from GitHub and clear the finalizer.
		Expect(k8sClient.Delete(ctx, rs)).To(Succeed())

		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur)
			return apierrors.IsNotFound(err)
		}, 3*timeout, interval).Should(BeTrue())
	})

	It("adopts an existing organization-owned ruleset by name without creating a duplicate", func() {
		if !isMockMode() {
			Skip("adopt-by-name pre-seeding requires the mock server")
		}

		rulesetName := "test-adopt-rs-" + uniqueID
		rsResourceName := "ruleset-adopt-" + uniqueID

		// Pre-create a ruleset on the mock server so the controller can adopt it.
		body, err := json.Marshal(map[string]any{
			"name":        rulesetName,
			"enforcement": "active",
			"target":      "branch",
		})
		Expect(err).NotTo(HaveOccurred())
		url := mockGitHubServer.URL + "/api/v3/orgs/" + orgName + "/rulesets"
		resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint:noctx
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated))

		var created map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())
		preExistingID := int64(created["id"].(float64))
		Expect(preExistingID).To(BeNumerically(">", 0))

		// Create the CR with the same name — controller should adopt, not create anew.
		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
				Target:       repoguardsapv1.RulesetTargetBranch,
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, rs) })

		// Wait for Ready and confirm the pre-existing ID was reused.
		Eventually(func() *int64 {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return nil
			}
			if cur.Status.State != repoguardsapv1.GithubOrganizationRulesetStateReady {
				return nil
			}
			return cur.Status.RulesetID
		}, 3*timeout, interval).Should(HaveValue(Equal(preExistingID)))
	})

	It("fails gracefully when Github CR does not exist", func() {
		rsResourceName := "ruleset-nogithub-" + uniqueID
		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       "nonexistent-github-" + uniqueID,
				Organization: orgName,
				Name:         "some-ruleset",
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, rs) })

		Eventually(func() repoguardsapv1.GithubOrganizationRulesetState {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateFailed))
	})
})
