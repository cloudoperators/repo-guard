// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package externalprovider

import "context"

type ExternalProvider interface {
	Users(ctx context.Context, group string) ([]string, error)
	TestConnection(ctx context.Context) error
}
