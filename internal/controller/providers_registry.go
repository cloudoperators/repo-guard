// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"sync"
)

var (
	LDAPGroupProviders   sync.Map
	GenericHTTPProviders sync.Map
	StaticProviders      sync.Map
)
