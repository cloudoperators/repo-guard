// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	githubguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:docs-gen:collapse=Imports
var _ = Describe("LDAP Group Provider controller", Ordered, func() {

	Context("LDAP Group Provider with its secret", func() {

		It("Should be in running state when deployed with the correct values", func() {

			ctx := context.Background()

			ldap := ldapGroupProvider.DeepCopy()
			secret := ldapGroupProviderSecret.DeepCopy()

			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, ldap)).Should(Succeed())

			Eventually(func() githubguardsapv1.LDAPGroupProviderState {
				l := &githubguardsapv1.LDAPGroupProvider{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"]}, l)
				Expect(err).NotTo(HaveOccurred())
				return l.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.LDAPGroupProviderStateRunning))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ldap)).Should(Succeed())

		})

	})

	Context("LDAP Group Provider with its secret", func() {
		It("Should be in failed state when deployed with the wrong values", func() {

			ctx := context.Background()

			ldap := ldapGroupProvider.DeepCopy()
			ldap.Spec.Secret = "asd"
			secret := ldapGroupProviderSecret.DeepCopy()

			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, ldap)).Should(Succeed())

			Eventually(func() githubguardsapv1.LDAPGroupProviderState {
				l := &githubguardsapv1.LDAPGroupProvider{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"]}, l)
				Expect(err).NotTo(HaveOccurred())
				return l.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.LDAPGroupProviderStateFailed))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ldap)).Should(Succeed())
		})
	})

	Context("LDAP Group Provider with its secret", func() {
		It("Should be in failed state when deployed with the wrong secret values", func() {

			ctx := context.Background()

			ldap := ldapGroupProvider.DeepCopy()

			secret := ldapGroupProviderSecret.DeepCopy()
			secret.StringData["bindPW"] = "asd"

			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, ldap)).Should(Succeed())

			Eventually(func() githubguardsapv1.LDAPGroupProviderState {
				l := &githubguardsapv1.LDAPGroupProvider{}
				err := k8sClient.Get(ctx, types.NamespacedName{Namespace: TEST_ENV["NAMESPACE"], Name: TEST_ENV["LDAP_GROUP_PROVIDER_KUBERNETES_RESOURCE_NAME"]}, l)
				Expect(err).NotTo(HaveOccurred())
				return l.Status.State
			}, timeout, interval).Should(BeEquivalentTo(githubguardsapv1.LDAPGroupProviderStateFailed))

			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ldap)).Should(Succeed())
		})
	})
})
