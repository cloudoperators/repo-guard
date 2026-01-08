package controller

import (
	"context"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:docs-gen:collapse=Imports
var _ = Describe("Github controller", Ordered, func() {

	Context("Github with its secret", func() {

		It("Should be in running state when deployed with the correct values", func() {

			ctx := context.Background()

			github := githubCom.DeepCopy()
			secret := githubComSecret.DeepCopy()

			if err := k8sClient.Create(ctx, secret); err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
			if err := k8sClient.Create(ctx, github); err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			Eventually(func() githubguardsapv1.GithubState {
				github := &githubguardsapv1.Github{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
				Expect(err).NotTo(HaveOccurred())
				return github.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateRunning))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, github)).Should(Succeed())

		})

	})

	Context("Github with its secret", func() {
		It("Should be in failed state when deployed with the wrong values", func() {

			ctx := context.Background()

			github := githubCom.DeepCopy()
			github.Spec.Secret = "asd"
			secret := githubComSecret.DeepCopy()

			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, github)).Should(Succeed())

			Eventually(func() githubguardsapv1.GithubState {
				github := &githubguardsapv1.Github{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
				Expect(err).NotTo(HaveOccurred())
				return github.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateFailed))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, github)).Should(Succeed())
		})
	})

	Context("Github with its secret", func() {
		It("Should be in failed state when deployed with the wrong secret values", func() {

			ctx := context.Background()

			github := githubCom.DeepCopy()
			secret := githubComSecret.DeepCopy()
			secret.StringData["privateKey"] = "asd"

			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, github)).Should(Succeed())

			Eventually(func() githubguardsapv1.GithubState {
				github := &githubguardsapv1.Github{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["GITHUB_KUBERNETES_RESOURCE_NAME"]}, github)
				Expect(err).NotTo(HaveOccurred())
				return github.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.GithubStateFailed))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, github)).Should(Succeed())
		})
	})
})
