// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// Verified against github.com/google/go-github/v88

package github

import (
	"encoding/json"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v88/github"
)

// TestMockResponseShapes validates that the JSON shapes used as mock fixtures
// decode correctly into the corresponding go-github structs.  If go-github is
// upgraded and field names change, this test will fail (or the decoded value
// will be silently zero), alerting the maintainer to update the fixtures.
// Note: this test validates expected shapes via hard-coded fixtures; the
// controller integration tests exercise the live mock server handlers.
func TestMockResponseShapes(t *testing.T) {
	t.Run("app", func(t *testing.T) {
		raw := `{"id":1,"slug":"mock-app","name":"Mock GitHub App"}`
		var app gogithub.App
		mustDecode(t, raw, &app)
		if app.GetID() != 1 {
			t.Errorf("App.ID: got %d, want 1", app.GetID())
		}
		if app.GetSlug() != "mock-app" {
			t.Errorf("App.Slug: got %q, want %q", app.GetSlug(), "mock-app")
		}
	})

	t.Run("installation_access_token", func(t *testing.T) {
		raw := `{"token":"mock-installation-token","expires_at":"2099-01-01T00:00:00Z"}`
		var tok gogithub.InstallationToken
		mustDecode(t, raw, &tok)
		if tok.GetToken() != "mock-installation-token" {
			t.Errorf("InstallationToken.Token: got %q", tok.GetToken())
		}
	})

	t.Run("user", func(t *testing.T) {
		raw := `{"id":42,"login":"testuser","type":"User"}`
		var u gogithub.User
		mustDecode(t, raw, &u)
		if u.GetID() != 42 {
			t.Errorf("User.ID: got %d, want 42", u.GetID())
		}
		if u.GetLogin() != "testuser" {
			t.Errorf("User.Login: got %q, want %q", u.GetLogin(), "testuser")
		}
	})

	t.Run("team", func(t *testing.T) {
		raw := `{"id":10,"name":"my-team","slug":"my-team"}`
		var team gogithub.Team
		mustDecode(t, raw, &team)
		if team.GetID() != 10 {
			t.Errorf("Team.ID: got %d, want 10", team.GetID())
		}
		if team.GetSlug() != "my-team" {
			t.Errorf("Team.Slug: got %q", team.GetSlug())
		}
	})

	t.Run("team_membership", func(t *testing.T) {
		raw := `{"state":"active","role":"member"}`
		var m gogithub.Membership
		mustDecode(t, raw, &m)
		if m.GetState() != "active" {
			t.Errorf("Membership.State: got %q", m.GetState())
		}
	})

	t.Run("repository", func(t *testing.T) {
		raw := `{"name":"my-repo","private":true,"visibility":"private","full_name":"org/my-repo"}`
		var repo gogithub.Repository
		mustDecode(t, raw, &repo)
		if repo.GetName() != "my-repo" {
			t.Errorf("Repository.Name: got %q", repo.GetName())
		}
		if !repo.GetPrivate() {
			t.Errorf("Repository.Private: got false, want true")
		}
	})

	t.Run("team_with_permission", func(t *testing.T) {
		// This shape is returned by repos/{owner}/{repo}/teams
		perm := "pull"
		raw := `{"id":5,"slug":"team-slug","name":"team-slug","permission":"pull"}`
		var team gogithub.Team
		mustDecode(t, raw, &team)
		if team.GetSlug() != "team-slug" {
			t.Errorf("Team.Slug: got %q", team.GetSlug())
		}
		if team.Permission == nil || *team.Permission != perm {
			t.Errorf("Team.Permission: got %v, want %q", team.Permission, perm)
		}
	})

	t.Run("org_membership", func(t *testing.T) {
		raw := `{"state":"active","role":"admin"}`
		var m gogithub.Membership
		mustDecode(t, raw, &m)
		if m.GetRole() != "admin" {
			t.Errorf("Membership.Role: got %q", m.GetRole())
		}
	})

	t.Run("users_list", func(t *testing.T) {
		raw := `[{"id":1,"login":"alice","type":"User"},{"id":2,"login":"bob","type":"User"}]`
		var users []*gogithub.User
		mustDecode(t, raw, &users)
		if len(users) != 2 {
			t.Fatalf("users: got %d, want 2", len(users))
		}
		if users[0].GetLogin() != "alice" {
			t.Errorf("users[0].Login: got %q", users[0].GetLogin())
		}
	})

	t.Run("teams_list", func(t *testing.T) {
		raw := `[{"id":10,"name":"team-a","slug":"team-a"},{"id":11,"name":"team-b","slug":"team-b"}]`
		var teams []*gogithub.Team
		mustDecode(t, raw, &teams)
		if len(teams) != 2 {
			t.Fatalf("teams: got %d, want 2", len(teams))
		}
		if teams[0].GetName() != "team-a" {
			t.Errorf("teams[0].Name: got %q", teams[0].GetName())
		}
	})

	t.Run("repos_list", func(t *testing.T) {
		raw := `[{"name":"repo1","private":false,"full_name":"org/repo1"},{"name":"repo2","private":true,"full_name":"org/repo2"}]`
		var repos []*gogithub.Repository
		mustDecode(t, raw, &repos)
		if len(repos) != 2 {
			t.Fatalf("repos: got %d, want 2", len(repos))
		}
		if repos[1].GetName() != "repo2" {
			t.Errorf("repos[1].Name: got %q", repos[1].GetName())
		}
	})
}

// mustDecode is a test helper that decodes JSON into v, failing the test on error.
func mustDecode(t *testing.T, raw string, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(strings.NewReader(raw)).Decode(v); err != nil {
		t.Fatalf("json decode into %T: %v", v, err)
	}
}
