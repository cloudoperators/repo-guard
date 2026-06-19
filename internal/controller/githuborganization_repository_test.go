// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v88/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github Organization controller - repository team assignments", func() {
	const (
		defaultPublicPull  = "public-pull-team"
		defaultPublicPush  = "public-push-team"
		defaultPublicAdmin = "public-admin-team"

		defaultPrivatePull  = "private-pull-team"
		defaultPrivatePush  = "private-push-team"
		defaultPrivateAdmin = "private-admin-team"

		defaultInternalPull  = "internal-pull-team"
		defaultInternalPush  = "internal-push-team"
		defaultInternalAdmin = "internal-admin-team"

		customTeam = "custom-team-for-private-repo"
	)

	var (
		client *githubAPI.Client
		ctx    context.Context

		nsObj  *v1.Namespace
		secret *v1.Secret
		github *repoguardsapv1.Github
		orgCR  *repoguardsapv1.GithubOrganization
		tr     *repoguardsapv1.GithubTeamRepository

		orgName string

		uniqueID      string
		uniqueNS      string
		uniqueGHName  string
		uniqueSecName string
		orgResource   string
		repoPublic    string
		repoPrivate   string
		repoInternal  string
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")
		var clientErr error
		client, clientErr = githubAPI.NewClient(githubAPI.WithAuthToken(requireEnv("GITHUB_TOKEN")))
		Expect(clientErr).NotTo(HaveOccurred())
		if isMockMode() {
			// In mock mode point the client at the mock server so that any direct
			// API calls (e.g. cleanup in DeferCleanup) never hit api.github.com.
			v3URL := strings.TrimSpace(TEST_ENV["GITHUB_V3_API_URL"])
			if v3URL != "" {
				if !strings.HasSuffix(v3URL, "/") {
					v3URL += "/"
				}
				// uploadURL must be the server root so that go-github appends
				// "/api/uploads" correctly; passing v3URL would produce
				// "…/api/v3/api/uploads".
				uploadURL := strings.TrimSuffix(v3URL, "api/v3/")
				var err error
				client, err = githubAPI.NewClient(githubAPI.WithAuthToken("mock-token"), githubAPI.WithEnterpriseURLs(v3URL, uploadURL))
				Expect(err).NotTo(HaveOccurred())
			}
		}

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNS = "ns-repo-" + uniqueID
		uniqueGHName = "gh-repo-" + uniqueID
		uniqueSecName = "sec-repo-" + uniqueID
		orgResource = "org-res-repo-" + uniqueID
		repoPublic = "repo-pub-" + uniqueID
		repoPrivate = "repo-priv-" + uniqueID
		repoInternal = "repo-int-" + uniqueID

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

		teams := []string{
			defaultPublicPull, defaultPublicPush, defaultPublicAdmin,
			defaultPrivatePull, defaultPrivatePush, defaultPrivateAdmin,
			defaultInternalPull, defaultInternalPush, defaultInternalAdmin,
			customTeam,
		}
		for _, t := range teams {
			Expect(githubEnsureTeam(ctx, client, orgName, t)).To(Succeed())
		}

		Expect(githubEnsureRepoWithVisibility(ctx, client, orgName, repoPublic, "public")).To(Succeed())
		Expect(githubEnsureRepoWithVisibility(ctx, client, orgName, repoPrivate, "private")).To(Succeed())
		Expect(githubEnsureRepoWithVisibility(ctx, client, orgName, repoInternal, "internal")).To(Succeed())

		orgCR = githubOrganizationGreenhouseSandboxForRepositoryTests.DeepCopy()
		orgCR.Name = orgResource
		orgCR.Namespace = uniqueNS
		orgCR.Spec.Github = uniqueGHName
		orgCR.Spec.Organization = orgName

		tr = githubTeamRepository.DeepCopy()
		tr.Name = generateUniqueName("team-repo")
		tr.Namespace = uniqueNS
		tr.Spec.Github = uniqueGHName
		tr.Spec.Organization = orgName
		tr.Spec.Repository = []string{repoPrivate}

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, tr)
			_ = deleteIgnoreNotFound(ctx, k8sClient, orgCR)
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
			_ = deleteIgnoreNotFound(ctx, k8sClient, nsObj)

			_, _ = client.Repositories.Delete(ctx, orgName, repoPublic)
			_, _ = client.Repositories.Delete(ctx, orgName, repoPrivate)
			_, _ = client.Repositories.Delete(ctx, orgName, repoInternal)
			for _, t := range teams {
				_, _ = client.Teams.DeleteTeamBySlug(ctx, orgName, t)
			}
		})
	})

	It("assigns default teams to public/private/internal repos and custom team to private repo", func() {
		Expect(ensureResourceCreated(ctx, orgCR)).To(Succeed())
		Expect(ensureResourceCreated(ctx, tr)).To(Succeed())

		Eventually(func() bool {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNS, Name: orgResource}, cur); err != nil {
				return false
			}
			return cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateComplete ||
				cur.Status.OrganizationStatus == repoguardsapv1.GithubOrganizationStateRateLimited
		}, 3*timeout, interval).Should(BeTrue())

		// Controller executed operations
		Eventually(func() int {
			cur := &repoguardsapv1.GithubOrganization{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: uniqueNS, Name: orgResource}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Operations.RepositoryTeamOperations)
		}, 3*timeout, interval).Should(BeNumerically(">", 0))

		// Verify that GitHub (or the mock) actually received the team assignments.
		publicTeams, resp, err := client.Repositories.ListTeams(ctx, orgName, repoPublic, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(publicTeams).To(HaveLen(3))

		privateTeams, resp, err := client.Repositories.ListTeams(ctx, orgName, repoPrivate, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(privateTeams).To(HaveLen(4))

		internalTeams, resp, err := client.Repositories.ListTeams(ctx, orgName, repoInternal, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(internalTeams).To(HaveLen(3))
	})
})
