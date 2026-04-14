// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"k8s.io/apimachinery/pkg/types"

	externalprovider "github.com/cloudoperators/repo-guard/internal/external-provider"
)

var (
	LDAPGroupProviders   map[types.NamespacedName]externalprovider.ExternalProvider
	GenericHTTPProviders map[types.NamespacedName]externalprovider.ExternalProvider
	StaticProviders      map[types.NamespacedName]externalprovider.ExternalProvider
)

func init() {
	LDAPGroupProviders = make(map[types.NamespacedName]externalprovider.ExternalProvider)
	GenericHTTPProviders = make(map[types.NamespacedName]externalprovider.ExternalProvider)
	StaticProviders = make(map[types.NamespacedName]externalprovider.ExternalProvider)
}
