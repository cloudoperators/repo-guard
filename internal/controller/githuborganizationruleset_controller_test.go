// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

		// Skip all ruleset tests when the GitHub org does not have a Team/Enterprise
		// plan — the org-level rulesets API returns 403 on free plans.
		if !isMockMode() {
			ghClient := newTestGithubClient()
			_, resp, err := ghClient.Organizations.ListAllRepositoryRulesets(ctx, orgName, nil)
			if err != nil && resp != nil && resp.StatusCode == 403 {
				Skip("org " + orgName + " does not support organization-level rulesets (requires GitHub Team/Enterprise plan)")
			}
		}

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

	It("requires spec in the CRD schema to protect immutability constraints", func() {
		// envtest does not enforce CRD required-field validation at admission time,
		// so we verify the schema directly: read the installed CRD and assert that
		// "spec" appears in the top-level required list.
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "githuborganizationrulesets.repo-guard.cloudoperators.dev",
		}, crd)).To(Succeed())
		var specRequired bool
		for _, ver := range crd.Spec.Versions {
			if ver.Schema == nil || ver.Schema.OpenAPIV3Schema == nil {
				continue
			}
			for _, f := range ver.Schema.OpenAPIV3Schema.Required {
				if f == "spec" {
					specRequired = true
					break
				}
			}
		}
		Expect(specRequired).To(BeTrue(), "spec must be in required so XValidation immutability rules cannot be bypassed by omitting it")
	})

	It("dryRun=true skips GitHub mutations and still reaches Ready state", func() {
		if !isMockMode() {
			Skip("mock server required to verify no GitHub calls are made")
		}
		rulesetName := "test-dryrun-" + uniqueID
		rsResourceName := "ruleset-dryrun-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/dryRun": "true",
				},
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
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
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateReady))

		// Verify no ruleset was actually created in GitHub (RulesetID must remain nil).
		cur := &repoguardsapv1.GithubOrganizationRuleset{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur)).To(Succeed())
		Expect(cur.Status.RulesetID).To(BeNil(), "dryRun must not create a ruleset in GitHub")
	})

	It("addRuleset=false skips GitHub create and still reaches Ready state", func() {
		if !isMockMode() {
			Skip("mock server required to verify no GitHub calls are made")
		}
		rulesetName := "test-add-false-" + uniqueID
		rsResourceName := "ruleset-add-false-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/addRuleset": "false",
				},
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
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
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubOrganizationRulesetStateReady))

		cur := &repoguardsapv1.GithubOrganizationRuleset{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur)).To(Succeed())
		Expect(cur.Status.RulesetID).To(BeNil(), "addRuleset=false must not create a ruleset in GitHub")
	})

	It("applies ruleset rules to a targeted repository (live GitHub only)", func() {
		if isMockMode() {
			Skip("requires real GitHub to verify repo-level ruleset enforcement")
		}

		ghClient := newTestGithubClient()
		repoName := "rg-test-rs-" + uniqueID

		// Create a private repo scoped to this test run.
		Expect(githubEnsureRepoWithVisibility(ctx, ghClient, orgName, repoName, "private")).To(Succeed())
		DeferCleanup(func() {
			_, _ = ghClient.Repositories.Delete(ctx, orgName, repoName)
		})

		rulesetName := "test-repo-rule-" + uniqueID
		rsResourceName := "ruleset-repo-" + uniqueID

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
						Include: []string{repoName},
					},
				},
				Rules: &repoguardsapv1.RulesetRules{
						NonFastForward: func() *bool { v := true; return &v }(),
					},
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, rs) })

		// Wait for the controller to push it to GitHub.
		var rulesetID int64
		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return false
			}
			if cur.Status.RulesetID == nil || cur.Status.State != repoguardsapv1.GithubOrganizationRulesetStateReady {
				return false
			}
			rulesetID = *cur.Status.RulesetID
			return true
		}, 3*timeout, interval).Should(BeTrue())
		Expect(rulesetID).To(BeNumerically(">", 0))

		// Verify the ruleset is visible on the targeted repository via the GitHub API.
		Eventually(func() bool {
			rulesets, _, err := ghClient.Repositories.GetAllRulesets(ctx, orgName, repoName, nil)
			if err != nil {
				return false
			}
			for _, r := range rulesets {
				if r.GetID() == rulesetID {
					return true
				}
			}
			return false
		}, 3*timeout, interval).Should(BeTrue(),
			"ruleset %d should appear on repo %s after controller reconciles", rulesetID, repoName)
	})

	It("removeRuleset=false orphans the GitHub ruleset instead of deleting it on CR removal", func() {
		if !isMockMode() {
			Skip("mock server required to inspect mock ruleset list after CR deletion")
		}
		rulesetName := "test-remove-false-" + uniqueID
		rsResourceName := "ruleset-remove-false-" + uniqueID

		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rsResourceName,
				Namespace: uniqueNamespace,
				Labels: map[string]string{
					"repo-guard.cloudoperators.dev/removeRuleset": "false",
				},
			},
			Spec: repoguardsapv1.GithubOrganizationRulesetSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Name:         rulesetName,
				Enforcement:  repoguardsapv1.RulesetEnforcementActive,
			},
		}
		Expect(ensureResourceCreated(ctx, rs)).To(Succeed())

		// Wait for ruleset to be created in GitHub.
		var rulesetID int64
		Eventually(func() *int64 {
			cur := &repoguardsapv1.GithubOrganizationRuleset{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur); err != nil {
				return nil
			}
			if cur.Status.State != repoguardsapv1.GithubOrganizationRulesetStateReady {
				return nil
			}
			return cur.Status.RulesetID
		}, 3*timeout, interval).Should(Not(BeNil()))

		cur := &repoguardsapv1.GithubOrganizationRuleset{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, cur)).To(Succeed())
		rulesetID = *cur.Status.RulesetID

		// Delete the CR — the controller should remove the finalizer without calling DELETE on GitHub.
		Expect(k8sClient.Delete(ctx, rs)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: rsResourceName}, &repoguardsapv1.GithubOrganizationRuleset{})
			return apierrors.IsNotFound(err)
		}, 3*timeout, interval).Should(BeTrue())

		// The ruleset must still exist in the mock GitHub (not been deleted).
		url := mockGitHubServer.URL + fmt.Sprintf("/api/v3/orgs/%s/rulesets/%d", orgName, rulesetID)
		resp, err := http.Get(url) //nolint:noctx
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = resp.Body.Close() }()
		Expect(resp.StatusCode).To(Equal(http.StatusOK), "ruleset should still exist in GitHub after CR deletion with removeRuleset=false")
	})
})

var _ = Describe("rulesetTTLRequeue", func() {
	makeRS := func(state repoguardsapv1.GithubOrganizationRulesetState, labels map[string]string, syncAge string) *repoguardsapv1.GithubOrganizationRuleset {
		rs := &repoguardsapv1.GithubOrganizationRuleset{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Status:     repoguardsapv1.GithubOrganizationRulesetStatus{State: state},
		}
		if syncAge != "" {
			d, _ := time.ParseDuration(syncAge)
			t := metav1.NewTime(time.Now().Add(-d))
			rs.Status.LastSyncTime = &t
		}
		return rs
	}

	It("returns 0 when no TTL labels are set", func() {
		rs := makeRS(repoguardsapv1.GithubOrganizationRulesetStateReady, nil, "10m")
		Expect(rulesetTTLRequeue(rs)).To(Equal(time.Duration(0)))
	})

	It("returns 0 when LastSyncTime is nil", func() {
		rs := makeRS(repoguardsapv1.GithubOrganizationRulesetStateReady, map[string]string{
			rulesetLabelCompletedTTL: "1h",
		}, "")
		Expect(rulesetTTLRequeue(rs)).To(Equal(time.Duration(0)))
	})

	It("returns remaining duration when TTL has not yet elapsed", func() {
		rs := makeRS(repoguardsapv1.GithubOrganizationRulesetStateReady, map[string]string{
			rulesetLabelCompletedTTL: "1h",
		}, "30m")
		d := rulesetTTLRequeue(rs)
		Expect(d).To(BeNumerically(">", 29*time.Minute))
		Expect(d).To(BeNumerically("<=", 30*time.Minute))
	})

	It("returns the full TTL interval after a completed reconcile so same-state repeats get a fresh window", func() {
		// Simulates the case where LastSyncTime was just stamped (elapsed ≈ 0).
		rs := makeRS(repoguardsapv1.GithubOrganizationRulesetStateReady, map[string]string{
			rulesetLabelCompletedTTL: "72h",
		}, "1s")
		d := rulesetTTLRequeue(rs)
		Expect(d).To(BeNumerically(">", 71*time.Hour+59*time.Minute))
	})

	It("returns the full TTL interval when TTL has already elapsed (no tight loop)", func() {
		rs := makeRS(repoguardsapv1.GithubOrganizationRulesetStateFailed, map[string]string{
			rulesetLabelFailedTTL: "24h",
		}, "25h")
		d := rulesetTTLRequeue(rs)
		// Must return the full TTL (24h), not 0 or 1ms.
		Expect(d).To(Equal(24 * time.Hour))
	})

	It("uses failedTTL when state is Failed and completedTTL when state is Ready", func() {
		labels := map[string]string{
			rulesetLabelFailedTTL:    "6h",
			rulesetLabelCompletedTTL: "72h",
		}
		failed := makeRS(repoguardsapv1.GithubOrganizationRulesetStateFailed, labels, "1s")
		ready := makeRS(repoguardsapv1.GithubOrganizationRulesetStateReady, labels, "1s")
		dFailed := rulesetTTLRequeue(failed)
		dReady := rulesetTTLRequeue(ready)
		Expect(dFailed).To(BeNumerically(">", 5*time.Hour+59*time.Minute))
		Expect(dReady).To(BeNumerically(">", 71*time.Hour+59*time.Minute))
	})
})
