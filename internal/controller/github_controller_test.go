// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Github controller", func() {
	var (
		ctx        context.Context
		github     *repoguardsapv1.Github
		secret     *v1.Secret
		githubName string
		secretName string
	)

	getGithubState := func() (repoguardsapv1.GithubState, error) {
		cur := &repoguardsapv1.Github{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: githubName}, cur)
		if err != nil {
			return "", err
		}
		return cur.Status.State, nil
	}

	BeforeEach(func() {
		ctx = context.Background()

		githubName = generateUniqueName(nonEmpty(TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"], "github"))
		secretName = generateUniqueName(nonEmpty(TEST_ENV["GITHUB_KUBERNETES_SECRET_RESOURCE_NAME"], "github-secret"))

		github = githubCom.DeepCopy()
		github.Name = githubName
		github.Namespace = ""
		github.Spec.Secret = secretName

		secret = githubComSecret.DeepCopy()
		secret.Name = secretName
		secret.Namespace = TestOperatorNamespace

		DeferCleanup(func() {
			_ = deleteIgnoreNotFound(ctx, k8sClient, github)
			_ = deleteIgnoreNotFound(ctx, k8sClient, secret)
		})
	})

	It("becomes Running when deployed with correct values", func() {
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			state, err := getGithubState()
			if apierrors.IsNotFound(err) {
				return ""
			}
			return state
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateRunning))
	})

	It("becomes Failed when secret reference is wrong", func() {
		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		github.Spec.Secret = "does-not-exist"
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			state, err := getGithubState()
			if apierrors.IsNotFound(err) {
				return ""
			}
			return state
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateFailed))
	})

	It("becomes Failed when secret values are wrong", func() {
		if secret.StringData == nil {
			secret.StringData = map[string]string{}
		}
		secret.StringData["privateKey"] = "invalid"

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, github)).To(Succeed())

		Eventually(func() repoguardsapv1.GithubState {
			state, err := getGithubState()
			if apierrors.IsNotFound(err) {
				return ""
			}
			return state
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.GithubStateFailed))
	})
})
