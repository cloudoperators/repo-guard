// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"errors"
	"strconv"
	"strings"
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
		expectedPrefix := "invalid GitHub user ID: \"not-numeric\" (expected numeric ID):"
		if !strings.HasPrefix(err.Error(), expectedPrefix) {
			t.Errorf("expected error message to start with %q, got %q", expectedPrefix, err.Error())
		}
	})

	t.Run("GithubUsernameByID", func(t *testing.T) {
		_, _, err := u.GithubUsernameByID(uid)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, strconv.ErrSyntax) {
			t.Errorf("expected strconv.ErrSyntax, got %v", err)
		}
		expectedPrefix := "invalid GitHub user ID: \"not-numeric\" (expected numeric ID):"
		if !strings.HasPrefix(err.Error(), expectedPrefix) {
			t.Errorf("expected error message to start with %q, got %q", expectedPrefix, err.Error())
		}
	})

	t.Run("HasVerifiedEmailDomainForGithubUID", func(t *testing.T) {
		_, err := u.HasVerifiedEmailDomainForGithubUID(t.Context(), "org", uid, "domain.com")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, strconv.ErrSyntax) {
			t.Errorf("expected strconv.ErrSyntax, got %v", err)
		}
		expectedPrefix := "invalid GitHub user ID: \"not-numeric\" (expected numeric ID):"
		if !strings.HasPrefix(err.Error(), expectedPrefix) {
			t.Errorf("expected error message to start with %q, got %q", expectedPrefix, err.Error())
		}
	})
}
