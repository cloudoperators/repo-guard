// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func TestDefaultUsersProvider_ParseIntErrorWrapping(t *testing.T) {
	u := &DefaultUsersProvider{}
	uid := "not-numeric"

	t.Run("IsMemberOfOrg", func(t *testing.T) {
		_, err := u.IsMemberOfOrg(t.Context(), "org", uid)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, strconv.ErrSyntax) {
			t.Errorf("expected strconv.ErrSyntax, got %v", err)
		}
		expectedMsg := fmt.Sprintf("invalid GitHub user ID: %q (expected numeric ID): strconv.ParseInt: parsing %q: %s", uid, uid, strconv.ErrSyntax)
		if err.Error() != expectedMsg {
			t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("GithubUsernameByID", func(t *testing.T) {
		_, _, err := u.GithubUsernameByID(uid)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedMsg := fmt.Sprintf("invalid GitHub user ID: %q (expected numeric ID): strconv.ParseInt: parsing %q: %s", uid, uid, strconv.ErrSyntax)
		if err.Error() != expectedMsg {
			t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
		}
	})

	t.Run("HasVerifiedEmailDomainForGithubUID", func(t *testing.T) {
		_, err := u.HasVerifiedEmailDomainForGithubUID(t.Context(), "org", uid, "domain.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		expectedMsg := fmt.Sprintf("invalid GitHub user ID: %q (expected numeric ID): strconv.ParseInt: parsing %q: %s", uid, uid, strconv.ErrSyntax)
		if err.Error() != expectedMsg {
			t.Errorf("expected error message %q, got %q", expectedMsg, err.Error())
		}
	})
}
