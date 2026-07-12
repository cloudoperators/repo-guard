// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	greenhousesapv1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v89/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Team controller", func() {
	var (
		ctx context.Context

		nsObj  *v1.Namespace
		secret *v1.Secret
		github *repoguardsapv1.Github
		org    *repoguardsapv1.GithubOrganization
		gal    *repoguardsapv1.GithubAccountLink

		uniqueID               string
		uniqueNamespace        string
		uniqueGithubName       string
		uniqueGithubSecretName string
		uniqueTeamName         string
		uniqueTeamResourceName string
		uniqueOrphanTeamName   string

		orgName string
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")
		requireEnv("GITHUB_TOKEN")

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNamespace = "ns-team-" + uniqueID
		uniqueGithubName = "gh-team-" + uniqueID
		uniqueGithubSecretName = "sec-team-" + uniqueID
		uniqueTeamName = "tm-" + uniqueID
		uniqueTeamResourceName = fmt.Sprintf("%s--%s--%s", uniqueGithubName, orgName, uniqueTeamName)
		uniqueOrphanTeamName = "orphan-" + uniqueID

		nsObj = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uniqueNamespace}}
		Expect(ensureResourceCreated(ctx, nsObj)).To(Succeed())

		github = githubCom.DeepCopy()
		github.Name = uniqueGithubName
		github.Namespace = ""
		github.Spec.Secret = uniqueGithubSecretName

		secret = githubComSecret.DeepCopy()
		secret.Name = uniqueGithubSecretName
		secret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", uniqueGithubName, orgName)
		org.Namespace = uniqueNamespace
		org.Spec.Github = uniqueGithubName
		org.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		gal = githubAccountLink.DeepCopy()
		gal.Name = generateUniqueName("team-gal")
		gal.Namespace = ""
		gal.Spec.Github = uniqueGithubName
		Expect(ensureResourceCreated(ctx, gal)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: uniqueGithubName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: org.Name}, cur); err != nil {
				return false
			}
			return cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateComplete ||
				cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateRateLimited
		}, 3*timeout, interval).Should(BeTrue())

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, gal)
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("reconciles no-provider team as complete with cleared operations and error", func() {
		// A GithubTeam with neither a GreenhouseTeam ref nor an ExternalMemberProvider
		// should observe GitHub-side members and clear any stale pending ops, but skip
		// ChangeCalculator entirely — always reporting TeamStatus=complete so that
		// ownersFromGithubTeams in the org controller never busy-loops on it.
		teamResourceName := fmt.Sprintf("%s--%s--%s", uniqueGithubName, orgName, uniqueOrphanTeamName)
		team := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      teamResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       uniqueGithubName,
				Organization: orgName,
				Team:         uniqueOrphanTeamName,
				// GreenhouseTeam and ExternalMemberProvider intentionally absent.
			},
		}
		Expect(ensureResourceCreated(ctx, team)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, team) })

		Eventually(func() repoguardsapv1.GithubTeamState {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: teamResourceName}, cur); err != nil {
				return ""
			}
			return cur.Status.TeamStatus
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubTeamState(repoguardsapv1.GithubTeamStateComplete)))

		cur := &repoguardsapv1.GithubTeam{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: teamResourceName}, cur)).To(Succeed())
		Expect(cur.Status.TeamStatusError).To(BeEmpty())
		Expect(cur.Status.Operations).To(BeEmpty())
	})

	It("syncs greenhouse team members into GithubTeam status", func() {
		team := githubTeamTest.DeepCopy()
		team.Name = uniqueTeamResourceName
		team.Namespace = uniqueNamespace
		team.Spec.Github = uniqueGithubName
		team.Spec.Organization = orgName
		team.Spec.Team = uniqueTeamName
		team.Spec.GreenhouseTeam = uniqueTeamName
		Expect(ensureResourceCreated(ctx, team)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, team) })

		ghTeam := greenhouseTeamTest.DeepCopy()
		ghTeam.Name = uniqueTeamName
		ghTeam.Namespace = uniqueNamespace
		Expect(ensureResourceCreated(ctx, ghTeam)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, ghTeam) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &greenhousesapv1alpha1.Team{
			ObjectMeta: metav1.ObjectMeta{Name: uniqueTeamName, Namespace: uniqueNamespace},
		}, func(obj *greenhousesapv1alpha1.Team) {
			obj.Status = greenhousesapv1alpha1.TeamStatus{
				Members: []greenhousesapv1alpha1.User{{
					ID:        requireEnvOr(TEST_ENV["USER_1_GREENHOUSE_ID"], "USER_1_GREENHOUSE_ID", TEST_ENV["USER_1"]),
					Email:     "user1@example.com",
					FirstName: "User1",
					LastName:  "Test",
				}},
			}
			if obj.Labels == nil {
				obj.Labels = map[string]string{}
			}
			obj.Labels["repo-guard.cloudoperators.dev/disableInternalUsernames"] = "false"
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: uniqueTeamResourceName}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Members)
		}, 3*timeout, interval).Should(Equal(1))
	})

	AfterEach(func() {
		ctx := context.Background()
		client, clientErr := githubAPI.NewClient(githubAPI.WithAuthToken(requireEnv("GITHUB_TOKEN")))
		Expect(clientErr).NotTo(HaveOccurred())
		if isMockMode() {
			// In mock mode point the client at the mock server so that cleanup
			// calls never hit api.github.com and the in-process mock state stays
			// clean between test runs.
			v3URL := strings.TrimSpace(TEST_ENV["GITHUB_V3_API_URL"])
			if v3URL != "" {
				if !strings.HasSuffix(v3URL, "/") {
					v3URL += "/"
				}
				uploadURL := strings.TrimSuffix(v3URL, "api/v3/")
				var err error
				client, err = githubAPI.NewClient(githubAPI.WithAuthToken("mock-token"), githubAPI.WithEnterpriseURLs(v3URL, uploadURL))
				Expect(err).NotTo(HaveOccurred())
			}
		}
		_, _ = client.Teams.DeleteTeamBySlug(ctx, orgName, uniqueTeamName)
		_, _ = client.Teams.DeleteTeamBySlug(ctx, orgName, uniqueOrphanTeamName)

		// Keep it small: enough to let reconcile settle in CI without long sleeps
		Eventually(func() bool { return true }, 200*time.Millisecond, 200*time.Millisecond).Should(BeTrue())
	})
})

// transientProviderErrorServer is a dedicated empHTTPTestServer used only by the
// transient-error tests below. It is separate from empHTTPServer so that flipping
// its error mode never interferes with other test suites.
var transientProviderErrorServer *empHTTPTestServer

var _ = Describe("Github Team controller — transient external provider errors", func() {
	var (
		ctx            context.Context
		nsObj          *v1.Namespace
		githubResource *repoguardsapv1.Github
		githubSecret   *v1.Secret
		org            *repoguardsapv1.GithubOrganization
		providerCRD    *repoguardsapv1.GenericExternalMemberProvider
		providerSecret *v1.Secret
		team           *repoguardsapv1.GithubTeam

		uniqueID         string
		uniqueNamespace  string
		uniqueGHName     string
		uniqueSecName    string
		orgName          string
		teamResourceName string
		providerName     string
	)

	BeforeEach(func() {
		ctx = context.Background()
		orgName = requireEnv("ORGANIZATION")
		requireEnv("GITHUB_TOKEN")

		// Start the dedicated error-injectable HTTP server once.
		if transientProviderErrorServer == nil {
			transientProviderErrorServer = newEMPHTTPTestServer(
				TEST_ENV["EMP_HTTP_USERNAME"],
				TEST_ENV["EMP_HTTP_PASSWORD"],
				TEST_ENV["EMP_HTTP_GROUP_ID"],
				TEST_ENV["EMP_HTTP_USER_INTERNAL_USERNAME"],
			)
		}
		// Always start each test with error mode off.
		transientProviderErrorServer.SetErrorMode(false)

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNamespace = "ns-tprov-" + uniqueID
		uniqueGHName = "gh-tprov-" + uniqueID
		uniqueSecName = "sec-tprov-" + uniqueID
		providerName = "emp-tprov-" + uniqueID
		teamSlug := "tm-tprov-" + uniqueID
		teamResourceName = fmt.Sprintf("%s--%s--%s", uniqueGHName, orgName, teamSlug)

		nsObj = &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uniqueNamespace}}
		Expect(ensureResourceCreated(ctx, nsObj)).To(Succeed())

		// Github resource + secret
		githubResource = githubCom.DeepCopy()
		githubResource.Name = uniqueGHName
		githubResource.Namespace = ""
		githubResource.Spec.Secret = uniqueSecName

		githubSecret = githubComSecret.DeepCopy()
		githubSecret.Name = uniqueSecName
		githubSecret.Namespace = TestOperatorNamespace

		Expect(ensureResourceCreated(ctx, githubSecret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, githubResource)).To(Succeed())

		// GithubOrganization
		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", uniqueGHName, orgName)
		org.Namespace = uniqueNamespace
		org.Spec.Github = uniqueGHName
		org.Spec.Organization = orgName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		// GenericExternalMemberProvider pointing at the transient test server
		base := transientProviderErrorServer.baseURL()
		providerSecret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerName + "-secret",
				Namespace: uniqueNamespace,
			},
			StringData: map[string]string{
				repoguardsapv1.SECRET_USERNAME_KEY:      TEST_ENV["EMP_HTTP_USERNAME"],
				repoguardsapv1.SECRET_PASSWORD_KEY:      TEST_ENV["EMP_HTTP_PASSWORD"],
				repoguardsapv1.SECRET_CLIENT_ID_KEY:     TEST_ENV["EMP_HTTP_CLIENT_ID"],
				repoguardsapv1.SECRET_CLIENT_SECRET_KEY: TEST_ENV["EMP_HTTP_CLIENT_SECRET"],
			},
		}
		Expect(ensureResourceCreated(ctx, providerSecret)).To(Succeed())

		providerCRD = &repoguardsapv1.GenericExternalMemberProvider{
			ObjectMeta: metav1.ObjectMeta{
				Name:      providerName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GenericExternalMemberProviderSpec{
				Endpoint:          fmt.Sprintf("%s/api/sp/groups/{group}/users.json", base),
				Secret:            providerName + "-secret",
				ResultsField:      "results",
				IDField:           "id",
				Paginated:         true,
				TotalPagesField:   "total_pages",
				PageParam:         "page",
				TestConnectionURL: fmt.Sprintf("%s/api/sp/search.json", base),
			},
		}
		Expect(ensureResourceCreated(ctx, providerCRD)).To(Succeed())

		// Wait for the provider to be registered in the runtime registry.
		providerKey := types.NamespacedName{Name: providerName, Namespace: uniqueNamespace}
		Eventually(func() bool {
			_, ok := GenericHTTPProviders.Load(providerKey)
			return ok
		}, 3*timeout, interval).Should(BeTrue(), "provider should be registered in GenericHTTPProviders")

		// GithubTeam referencing the provider
		team = &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      teamResourceName,
				Namespace: uniqueNamespace,
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       uniqueGHName,
				Organization: orgName,
				Team:         teamSlug,
				ExternalMemberProvider: &repoguardsapv1.ExternalMemberProviderConfig{
					GenericHTTP: &repoguardsapv1.GenericProvider{
						ExternalMemberProvider: providerName,
						Group:                  TEST_ENV["EMP_HTTP_GROUP_ID"],
					},
				},
			},
		}

		// Wait for Github resource to be Running before the test body runs.
		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: uniqueGHName}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))

		// Wait for GithubOrganization to be reconciled so the teams provider is
		// initialized before the test body creates a GithubTeam.
		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNamespace, Name: org.Name}, cur); err != nil {
				return false
			}
			return cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateComplete ||
				cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateRateLimited
		}, 3*timeout, interval).Should(BeTrue())

		DeferCleanup(func() {
			transientProviderErrorServer.SetErrorMode(false)
			_ = deleteIgnoreNotFound(ctx, k8sClient, team)
			_ = deleteIgnoreNotFound(ctx, k8sClient, providerCRD)
			_ = deleteIgnoreNotFound(ctx, k8sClient, providerSecret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, org)
			_ = deleteIgnoreNotFound(ctx, k8sClient, githubResource)
			_ = deleteIgnoreNotFound(ctx, k8sClient, githubSecret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)
		})
	})

	It("sets TeamStatus=failed on HTTP 502 and self-heals when the provider recovers", func() {
		// Inject a 502 error before the team is even created so the first
		// reconcile hits the error path immediately.
		transientProviderErrorServer.SetErrorMode(true)

		Expect(ensureResourceCreated(ctx, team)).To(Succeed())

		teamKey := types.NamespacedName{Namespace: uniqueNamespace, Name: teamResourceName}

		// 1. TeamStatus must reach "failed" with the expected error.
		Eventually(func() repoguardsapv1.GithubTeamState {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, teamKey, cur); err != nil {
				return ""
			}
			return cur.Status.TeamStatus
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubTeamState(repoguardsapv1.GithubTeamStateFailed)),
			"team should reach failed state while provider is returning 502")

		cur := &repoguardsapv1.GithubTeam{}
		Expect(k8sClient.Get(ctx, teamKey, cur)).To(Succeed())
		Expect(cur.Status.TeamStatusError).To(ContainSubstring("502"),
			"error message should mention the 502 status code")

		// 2. Restore the provider and confirm the team self-heals.
		transientProviderErrorServer.SetErrorMode(false)

		Eventually(func() repoguardsapv1.GithubTeamState {
			cur3 := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, teamKey, cur3); err != nil {
				return ""
			}
			return cur3.Status.TeamStatus
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubTeamState(repoguardsapv1.GithubTeamStateComplete)),
			"team should transition to complete once the provider recovers")

		// 3. Verify the stale error message was also cleared.
		cur4 := &repoguardsapv1.GithubTeam{}
		Expect(k8sClient.Get(ctx, teamKey, cur4)).To(Succeed())
		Expect(cur4.Status.TeamStatusError).To(BeEmpty(),
			"TeamStatusError should be cleared when transitioning to complete")
	})
})
