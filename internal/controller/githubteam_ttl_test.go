// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GithubTeam TTL labels maintenance", Ordered, func() {
	var (
		ctx    context.Context
		github *repoguardsapv1.Github
		secret *v1.Secret
		org    *repoguardsapv1.GithubOrganization
	)

	BeforeAll(func() {
		ctx = context.Background()

		// Use unique names to avoid interference
		ghName := generateUniqueName("gh-team-ttl")
		secName := generateUniqueName("sec-team-ttl")

		// Create secret in operator namespace with the exact expected name
		secret = githubComSecret.DeepCopy()
		secret.Name = secName
		secret.Namespace = TestOperatorNamespace
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())

		// Create cluster-scoped Github first (so we know expected secret name)
		github = githubCom.DeepCopy()
		github.Name = ghName
		github.Namespace = ""
		github.Spec.Secret = secName
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		// Create org fixture used by team tests
		org = githubOrganizationGreenhouseSandboxForTeamTests.DeepCopy()
		org.Name = fmt.Sprintf("%s--%s", ghName, TEST_ENV["ORGANIZATION"])
		org.Spec.Github = ghName
		Expect(ensureResourceCreated(ctx, org)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			cur := &repoguardsapv1.Github{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: github.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))
	})

	AfterAll(func() {
		ctx := context.Background()
		_ = deleteIgnoreNotFound(ctx, k8sClient, org)
		_ = deleteIgnoreNotFound(ctx, k8sClient, github)
		_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
	})

	It("clears failed status and failed operations after failedTTL", func() {
		ctx := context.Background()

		teamName := "team-failed-ttl"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_FAILED_TTL:              "1s",
					"repoguard.cloudoperators.dev/addUser":    "false",
					"repoguard.cloudoperators.dev/removeUser": "false",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       github.Name,
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         teamName,
			},
			Status: repoguardsapv1.GithubTeamStatus{
				TeamStatus:          repoguardsapv1.GithubTeamStateFailed,
				TeamStatusError:     "some failure",
				TeamStatusTimestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				Operations: []repoguardsapv1.GithubUserOperation{{
					Operation: repoguardsapv1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     repoguardsapv1.GithubUserOperationStateFailed,
					Timestamp: metav1.Now(),
				}},
			},
		}
		Expect(ensureResourceCreated(ctx, t)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, t) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TEST_ENV["NAMESPACE"]},
		}, func(cur *repoguardsapv1.GithubTeam) {
			cur.Status = t.Status
		})).To(Succeed())

		Eventually(func() string {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur); err != nil {
				return "error-get"
			}
			return cur.Status.TeamStatusError
		}, 3*timeout, interval).Should(BeEmpty())
	})

	It("clears completed operations after completedTTL", func() {
		ctx := context.Background()

		teamName := "team-completed-ttl"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_COMPLETED_TTL: "1s",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       github.Name,
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         teamName,
			},
			Status: repoguardsapv1.GithubTeamStatus{
				Operations: []repoguardsapv1.GithubUserOperation{{
					Operation: repoguardsapv1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     repoguardsapv1.GithubUserOperationStateComplete,
					Timestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				}},
			},
		}
		Expect(ensureResourceCreated(ctx, t)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, t) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TEST_ENV["NAMESPACE"]},
		}, func(cur *repoguardsapv1.GithubTeam) {
			cur.Status = t.Status
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Operations)
		}, 3*timeout, interval).Should(Equal(0))
	})

	It("clears notfound operations after notfoundTTL", func() {
		ctx := context.Background()

		teamName := "team-notfound-ttl"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_NOTFOUND_TTL: "1s",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       github.Name,
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         teamName,
			},
			Status: repoguardsapv1.GithubTeamStatus{
				Operations: []repoguardsapv1.GithubUserOperation{{
					Operation: repoguardsapv1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     repoguardsapv1.GithubUserOperationStateNotFound,
					Timestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				}},
			},
		}
		Expect(ensureResourceCreated(ctx, t)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, t) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TEST_ENV["NAMESPACE"]},
		}, func(cur *repoguardsapv1.GithubTeam) {
			cur.Status = t.Status
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Operations)
		}, 3*timeout, interval).Should(Equal(0))
	})

	It("clears skipped operations after skippedTTL", func() {
		ctx := context.Background()

		teamName := "team-skipped-ttl"
		name := fmt.Sprintf("%s--%s--%s", github.Name, TEST_ENV["ORGANIZATION"], teamName)

		t := &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: TEST_ENV["NAMESPACE"],
				Labels: map[string]string{
					GITHUB_TEAM_LABEL_SKIPPED_TTL: "1s",
				},
			},
			Spec: repoguardsapv1.GithubTeamSpec{
				Github:       github.Name,
				Organization: TEST_ENV["ORGANIZATION"],
				Team:         teamName,
			},
			Status: repoguardsapv1.GithubTeamStatus{
				Operations: []repoguardsapv1.GithubUserOperation{{
					Operation: repoguardsapv1.GithubUserOperationTypeAdd,
					User:      "user1",
					State:     repoguardsapv1.GithubUserOperationStateSkipped,
					Timestamp: metav1.NewTime(time.Now().Add(-2 * time.Second)),
				}},
			},
		}
		Expect(ensureResourceCreated(ctx, t)).To(Succeed())
		DeferCleanup(func() { _ = deleteIgnoreNotFound(ctx, k8sClient, t) })

		Expect(updateStatusWithRetry(ctx, k8sClient, &repoguardsapv1.GithubTeam{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: TEST_ENV["NAMESPACE"]},
		}, func(cur *repoguardsapv1.GithubTeam) {
			cur.Status = t.Status
		})).To(Succeed())

		Eventually(func() int {
			cur := &repoguardsapv1.GithubTeam{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: name}, cur); err != nil {
				return -1
			}
			return len(cur.Status.Operations)
		}, 3*timeout, interval).Should(Equal(0))
	})
})
