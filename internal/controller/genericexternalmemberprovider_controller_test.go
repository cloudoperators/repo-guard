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

var _ = Describe("Generic External Member Provider controller", Ordered, func() {
	It("cleans up the registry when GenericExternalMemberProvider is deleted", func() {
		ctx := context.Background()

		emp := empHTTP.DeepCopy()
		secret := empHTTPSecret.DeepCopy()

		key := types.NamespacedName{Name: emp.Name, Namespace: emp.Namespace}

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, emp)).To(Succeed())

		Eventually(func() bool {
			_, ok := GenericHTTPProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeTrue(), "provider should be in registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, emp)).To(Succeed())

		Eventually(func() bool {
			_, ok := GenericHTTPProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeFalse(), "provider should be removed from registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})

	It("cleans up the registry when ClusterGenericExternalMemberProvider is deleted", func() {
		ctx := context.Background()

		cemp := &repoguardsapv1.ClusterGenericExternalMemberProvider{
			ObjectMeta: empHTTP.ObjectMeta,
			Spec:       empHTTP.Spec,
		}
		cemp.Namespace = ""
		cemp.Name = "cluster-" + empHTTP.Name
		secret := empHTTPSecret.DeepCopy()
		secret.Namespace = TestOperatorNamespace

		key := types.NamespacedName{Name: cemp.Name}

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, cemp)).To(Succeed())

		Eventually(func() bool {
			_, ok := GenericHTTPProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeTrue(), "cluster provider should be in registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, cemp)).To(Succeed())

		Eventually(func() bool {
			_, ok := GenericHTTPProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeFalse(), "cluster provider should be removed from registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})

	It("cleans up the registry when LDAPGroupProvider is deleted", func() {
		ctx := context.Background()

		ldap := ldapGroupProvider.DeepCopy()
		secret := ldapGroupProviderSecret.DeepCopy()

		key := types.NamespacedName{Name: ldap.Name, Namespace: ldap.Namespace}

		Expect(ensureResourceCreated(ctx, secret)).To(Succeed())
		Expect(ensureResourceCreated(ctx, ldap)).To(Succeed())

		Eventually(func() bool {
			_, ok := LDAPGroupProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeTrue(), "ldap provider should be in registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, ldap)).To(Succeed())

		Eventually(func() bool {
			_, ok := LDAPGroupProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeFalse(), "ldap provider should be removed from registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, secret)).To(Succeed())
	})

	It("cleans up the registry when StaticMemberProvider is deleted", func() {
		ctx := context.Background()

		emp := empStatic.DeepCopy()

		key := types.NamespacedName{Name: emp.Name, Namespace: emp.Namespace}

		Expect(ensureResourceCreated(ctx, emp)).To(Succeed())

		Eventually(func() bool {
			_, ok := StaticProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeTrue(), "static provider should be in registry")

		Expect(deleteIgnoreNotFound(ctx, k8sClient, emp)).To(Succeed())

		Eventually(func() bool {
			_, ok := StaticProviders.Load(key)
			return ok
		}, 3*timeout, interval).Should(BeFalse(), "static provider should be removed from registry")
	})
})
