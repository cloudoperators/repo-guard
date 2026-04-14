// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	githubAPI "github.com/google/go-github/v83/github"
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
	)

	BeforeEach(func() {
		ctx = context.Background()

		orgName = requireEnv("ORGANIZATION")
		client = githubAPI.NewClient(nil).WithAuthToken(requireEnv("GITHUB_TOKEN"))

		uniqueID = fmt.Sprintf("%08x", testRand.Uint32())
		uniqueNS = "ns-repo-" + uniqueID
		uniqueGHName = "gh-repo-" + uniqueID
		uniqueSecName = "sec-repo-" + uniqueID
		orgResource = "org-res-repo-" + uniqueID
		repoPublic = "repo-pub-" + uniqueID
		repoPrivate = "repo-priv-" + uniqueID

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
			customTeam,
		}
		for _, t := range teams {
			_ = githubEnsureTeam(ctx, client, orgName, t)
		}

		Expect(githubEnsureRepoWithVisibility(ctx, client, orgName, repoPublic, false)).To(Succeed())
		Expect(githubEnsureRepoWithVisibility(ctx, client, orgName, repoPrivate, true)).To(Succeed())

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
			for _, t := range teams {
				_, _ = client.Teams.DeleteTeamBySlug(ctx, orgName, t)
			}
		})
	})

	It("assigns default teams to public/private repos and custom team to private repo", func() {
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

		publicTeams, resp, err := client.Repositories.ListTeams(ctx, orgName, repoPublic, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(publicTeams).To(HaveLen(3))

		privateTeams, resp, err := client.Repositories.ListTeams(ctx, orgName, repoPrivate, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(200))
		Expect(privateTeams).To(HaveLen(4))
	})
})
