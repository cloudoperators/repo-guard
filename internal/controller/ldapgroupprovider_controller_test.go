// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	repoguardsapv1 "github.com/cloudoperators/repo-guard/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("LDAP Group Provider controller", Ordered, func() {
	It("becomes Running with correct secret", func() {
		ctx := context.Background()

		ldap := ldapGroupProvider.DeepCopy()
		secret := ldapGroupProviderSecret.DeepCopy()

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, ldap)).To(Succeed())

		Eventually(func() repoguardsapv1.LDAPGroupProviderState {
			cur := &repoguardsapv1.LDAPGroupProvider{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: nonEmpty(TEST_ENV["NAMESPACE"], "default"), Name: ldap.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.LDAPGroupProviderStateRunning))

		Expect(deleteIgnoreNotFound(ctx, k8sClient, ldap)).To(Succeed())
		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})

	It("becomes Failed when secret reference is wrong", func() {
		ctx := context.Background()

		ldap := ldapGroupProvider.DeepCopy()
		ldap.Spec.Secret = "missing-secret"
		secret := ldapGroupProviderSecret.DeepCopy()

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, ldap)).To(Succeed())

		Eventually(func() repoguardsapv1.LDAPGroupProviderState {
			cur := &repoguardsapv1.LDAPGroupProvider{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: nonEmpty(TEST_ENV["NAMESPACE"], "default"), Name: ldap.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.LDAPGroupProviderStateFailed))

		Expect(deleteIgnoreNotFound(ctx, k8sClient, ldap)).To(Succeed())
		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})

	It("becomes Failed when secret values are wrong", func() {
		ctx := context.Background()

		ldap := ldapGroupProvider.DeepCopy()
		secret := ldapGroupProviderSecret.DeepCopy()
		if secret.StringData == nil {
			secret.StringData = map[string]string{}
		}
		secret.StringData["bindPW"] = "invalid"

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, ldap)).To(Succeed())

		Eventually(func() repoguardsapv1.LDAPGroupProviderState {
			cur := &repoguardsapv1.LDAPGroupProvider{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: nonEmpty(TEST_ENV["NAMESPACE"], "default"), Name: ldap.Name}, cur); err != nil {
				return ""
			}
			return cur.Status.State
		}, 3*timeout, interval).Should(Equal(repoguardsapv1.LDAPGroupProviderStateFailed))

		Expect(deleteIgnoreNotFound(ctx, k8sClient, ldap)).To(Succeed())
		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})
})
