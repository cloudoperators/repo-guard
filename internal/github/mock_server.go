// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// Verified against github.com/google/go-github/v85

package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/gosimple/slug"
)

// MockConfig defines the canned data served by the mock GitHub HTTP server.
type MockConfig struct {
	Org     string
	Members []MockUser // all org members (role=all)
	Owners  []MockUser // org owners (role=admin)
	Teams   []MockTeam
	Repos   []MockRepo
}

// MockUser represents a GitHub user for mock purposes.
type MockUser struct {
	Login string
	ID    int64
}

// MockTeam represents a GitHub team for mock purposes.
type MockTeam struct {
	ID   int64
	Name string
	Slug string
}

// MockRepo represents a GitHub repository for mock purposes.
type MockRepo struct {
	Name    string
	Private bool
	Teams   []MockTeamWithPermission
}

// MockTeamWithPermission pairs a team slug with a permission for a repo.
type MockTeamWithPermission struct {
	Slug       string
	ID         int64
	Permission string
}

// MockTestHelper is a minimal interface satisfied by both *testing.T and
// Ginkgo's GinkgoT(), allowing NewMockGitHubServer to be called from either
// standard Go tests or Ginkgo test suites.
type MockTestHelper interface {
	Cleanup(func())
}

// NewMockGitHubServer starts an httptest.Server wired with canned GitHub API
// responses derived from cfg.  It returns the server and its mux so individual
// tests can register additional more-specific patterns (note: http.ServeMux
// panics on duplicate pattern registration, so existing handlers cannot be
// replaced — only extended with more-specific paths).
//
// To redirect go-github requests at this server, construct the client with
// WithEnterpriseURLs(srv.URL+"/api/v3/", srv.URL+"/").
func NewMockGitHubServer(t MockTestHelper, cfg MockConfig) (*httptest.Server, *http.ServeMux) {
	mux := http.NewServeMux()
	registerMockHandlers(mux, cfg)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mux
}

// NewMockGitHubMux returns an *http.ServeMux pre-wired with canned GitHub API
// responses for cfg.  Use this when you want to attach the handlers to your own
// net/http server (e.g. a standalone long-running process) rather than an
// httptest.Server.
func NewMockGitHubMux(cfg MockConfig) *http.ServeMux {
	mux := http.NewServeMux()
	registerMockHandlers(mux, cfg)
	return mux
}

// registerMockHandlers wires all default GitHub API handlers onto mux.
func registerMockHandlers(mux *http.ServeMux, cfg MockConfig) {
	org := cfg.Org

	// teams is a mutable copy of cfg.Teams so that POST /teams can add entries.
	var teamsMu sync.Mutex
	teams := make([]MockTeam, len(cfg.Teams))
	copy(teams, cfg.Teams)

	// nextTeamID is the auto-increment counter for dynamically created teams.
	// Start at max seeded ID + 1 to avoid collisions with seeded teams.
	nextTeamID := int64(1)
	for _, t := range cfg.Teams {
		if t.ID >= nextTeamID {
			nextTeamID = t.ID + 1
		}
	}

	// teamMembers tracks the members of each team by slug.
	teamMembers := make(map[string][]MockUser)

	// teamRepoPerms tracks team-repo permission assignments: repoName -> []MockTeamWithPermission.
	// Seeded from cfg.Repos[*].Teams; updated by PUT/DELETE on the team repos endpoint.
	teamRepoPerms := make(map[string][]MockTeamWithPermission)
	for _, repo := range cfg.Repos {
		if len(repo.Teams) > 0 {
			perms := make([]MockTeamWithPermission, len(repo.Teams))
			copy(perms, repo.Teams)
			teamRepoPerms[repo.Name] = perms
		}
	}

	// repos is a mutable copy of cfg.Repos so that POST /repos can add entries.
	repos := make([]MockRepo, len(cfg.Repos))
	copy(repos, cfg.Repos)

	// lookupRepo finds a repo by name in the mutable repos slice (must be called with teamsMu held).
	lookupRepo := func(name string) *MockRepo {
		for i := range repos {
			if repos[i].Name == name {
				return &repos[i]
			}
		}
		return nil
	}

	// orgAdmins is the mutable set of org admins (login -> MockUser).
	// Seeded from cfg.Owners; updated when PUT /memberships/{user} sets role=admin.
	orgAdmins := make(map[string]MockUser)
	for _, u := range cfg.Owners {
		orgAdmins[strings.ToLower(u.Login)] = u
	}

	// orgMembers is the mutable set of all org members (login -> MockUser).
	// Seeded from cfg.Members ∪ cfg.Owners so that owners who are later demoted
	// to "member" still appear in the role=all listing rather than disappearing.
	orgMembers := make(map[string]MockUser, len(cfg.Members)+len(cfg.Owners))
	for _, u := range cfg.Members {
		orgMembers[strings.ToLower(u.Login)] = u
	}
	for _, u := range cfg.Owners {
		if _, exists := orgMembers[strings.ToLower(u.Login)]; !exists {
			orgMembers[strings.ToLower(u.Login)] = u
		}
	}

	// lookupUser finds a user by login across cfg.Members and cfg.Owners.
	lookupUser := func(login string) (MockUser, bool) {
		for _, u := range cfg.Members {
			if strings.EqualFold(u.Login, login) {
				return u, true
			}
		}
		for _, u := range cfg.Owners {
			if strings.EqualFold(u.Login, login) {
				return u, true
			}
		}
		// Unknown user: synthesise a minimal entry so the PUT still succeeds.
		return MockUser{Login: login, ID: 0}, true
	}

	// ---- org endpoint ----

	// GET /api/v3/orgs/{org}
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s", org), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]interface{}{
			"login": org,
			"id":    1,
			"name":  org,
			"type":  "Organization",
		})
	})

	// ---- app endpoints ----

	// GET /api/v3/app  – NewAppClient() validation
	mux.HandleFunc("/api/v3/app", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]interface{}{
			"id":   1,
			"slug": "mock-app",
			"name": "Mock GitHub App",
		})
	})

	// POST /api/v3/app/installations/{id}/access_tokens – NewInstallationClient()
	mux.HandleFunc("/api/v3/app/installations/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/access_tokens") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONCreated(w, map[string]interface{}{
			"token":      "mock-installation-token",
			"expires_at": "2099-01-01T00:00:00Z",
		})
	})

	// ---- org member endpoints ----

	// GET  /api/v3/orgs/{org}/members           (list members by role)
	// also used for IsMember: GET /api/v3/orgs/{org}/members/{user} (returns 204/404)
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/members", org), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		role := r.URL.Query().Get("role")
		var users []MockUser
		switch role {
		case "admin":
			teamsMu.Lock()
			for _, u := range orgAdmins {
				users = append(users, u)
			}
			teamsMu.Unlock()
		default:
			// role=all returns the full set of org members.  Use orgMembers (which
			// is seeded from cfg.Members ∪ cfg.Owners and stays stable across
			// promotions/demotions) then include any users who were promoted to
			// admin but were not in the initial seed (dynamically invited members).
			teamsMu.Lock()
			for _, u := range orgMembers {
				users = append(users, u)
			}
			seen := make(map[string]struct{}, len(orgMembers))
			for k := range orgMembers {
				seen[k] = struct{}{}
			}
			for _, u := range orgAdmins {
				if _, exists := seen[strings.ToLower(u.Login)]; !exists {
					users = append(users, u)
				}
			}
			teamsMu.Unlock()
		}
		result := make([]map[string]interface{}, 0, len(users))
		for _, u := range users {
			result = append(result, userToMap(u))
		}
		writeJSON(w, result)
	})

	// /api/v3/orgs/{org}/members/{user} — IsMember check and DELETE
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/members/", org), func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/api/v3/orgs/%s/members/", org))
		switch r.Method {
		case http.MethodGet:
			// IsMember: return 204 if member, 404 otherwise.
			// Check orgMembers (the mutable set seeded from cfg.Members ∪ cfg.Owners
			// and updated by PUT /memberships) and orgAdmins (for dynamically
			// promoted users not in the initial seed).
			teamsMu.Lock()
			_, isMember := orgMembers[strings.ToLower(username)]
			_, isAdmin := orgAdmins[strings.ToLower(username)]
			teamsMu.Unlock()
			if isMember || isAdmin {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.Error(w, "not found", http.StatusNotFound)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// GET /api/v3/orgs/{org}/outside_collaborators
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/outside_collaborators", org), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, []interface{}{})
	})

	// GET /api/v3/orgs/{org}/invitations  (pending org invitations)
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/invitations", org), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, []interface{}{})
	})

	// /api/v3/orgs/{org}/memberships/{user} — PUT to change role, GET to get membership
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/memberships/", org), func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/api/v3/orgs/%s/memberships/", org))
		switch r.Method {
		case http.MethodPut:
			var body struct {
				Role string `json:"role"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
				return
			}
			if strings.EqualFold(body.Role, "admin") {
				u, _ := lookupUser(username)
				teamsMu.Lock()
				orgAdmins[strings.ToLower(username)] = u
				// Also ensure the user is in the members set.
				orgMembers[strings.ToLower(username)] = u
				teamsMu.Unlock()
			} else {
				u, _ := lookupUser(username)
				teamsMu.Lock()
				delete(orgAdmins, strings.ToLower(username))
				// Keep the user in orgMembers — demoted admins remain org members.
				orgMembers[strings.ToLower(username)] = u
				teamsMu.Unlock()
			}
			writeJSON(w, map[string]interface{}{"state": "active", "role": body.Role})
		case http.MethodGet:
			teamsMu.Lock()
			_, isAdmin := orgAdmins[strings.ToLower(username)]
			teamsMu.Unlock()
			role := "member"
			if isAdmin {
				role = "admin"
			}
			writeJSON(w, map[string]interface{}{"state": "active", "role": role})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// GET /api/v3/user/memberships/orgs/{org}
	mux.HandleFunc(fmt.Sprintf("/api/v3/user/memberships/orgs/%s", org), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]interface{}{"state": "active", "role": "member"})
	})

	// ---- teams ----

	// GET  /api/v3/orgs/{org}/teams
	// POST /api/v3/orgs/{org}/teams
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/teams", org), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			teamsMu.Lock()
			snapshot := make([]MockTeam, len(teams))
			copy(snapshot, teams)
			teamsMu.Unlock()
			result := make([]map[string]interface{}, 0, len(snapshot))
			for _, team := range snapshot {
				result = append(result, teamToMap(team))
			}
			writeJSON(w, result)
		case http.MethodPost:
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
				return
			}
			teamSlugNew := slug.Make(body.Name)
			if teamSlugNew == "" {
				teamSlugNew = "new-team"
			}
			teamsMu.Lock()
			for _, t := range teams {
				if t.Slug == teamSlugNew {
					teamsMu.Unlock()
					writeJSONError(w, `{"message":"Validation Failed","errors":[{"resource":"Team","code":"already_exists"}]}`, http.StatusUnprocessableEntity)
					return
				}
			}
			id := nextTeamID
			nextTeamID++
			teams = append(teams, MockTeam{ID: id, Name: body.Name, Slug: teamSlugNew})
			teamsMu.Unlock()
			writeJSONCreated(w, map[string]interface{}{"id": id, "name": body.Name, "slug": teamSlugNew})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /api/v3/orgs/{org}/teams/{slug}[/...] — team detail, members, memberships, repos
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/teams/", org), func(w http.ResponseWriter, r *http.Request) {
		prefix := fmt.Sprintf("/api/v3/orgs/%s/teams/", org)
		remainder := strings.TrimPrefix(r.URL.Path, prefix)
		parts := strings.SplitN(remainder, "/", 2)
		teamSlug := parts[0]
		var subPath string
		if len(parts) > 1 {
			subPath = parts[1]
		}

		switch {
		// DELETE /api/v3/orgs/{org}/teams/{slug}
		case subPath == "" && r.Method == http.MethodDelete:
			teamsMu.Lock()
			newTeams := teams[:0]
			for _, t := range teams {
				if t.Slug != teamSlug {
					newTeams = append(newTeams, t)
				}
			}
			teams = newTeams
			delete(teamMembers, teamSlug)
			for repoName, perms := range teamRepoPerms {
				newPerms := perms[:0]
				for _, tp := range perms {
					if tp.Slug != teamSlug {
						newPerms = append(newPerms, tp)
					}
				}
				teamRepoPerms[repoName] = newPerms
			}
			teamsMu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		// GET /api/v3/orgs/{org}/teams/{slug}
		case subPath == "" && r.Method == http.MethodGet:
			teamsMu.Lock()
			snapshot := make([]MockTeam, len(teams))
			copy(snapshot, teams)
			teamsMu.Unlock()
			for _, team := range snapshot {
				if team.Slug == teamSlug {
					writeJSON(w, teamToMap(team))
					return
				}
			}
			http.Error(w, "not found", http.StatusNotFound)

		// GET /api/v3/orgs/{org}/teams/{slug}/members
		case subPath == "members" && r.Method == http.MethodGet:
			teamsMu.Lock()
			members := make([]MockUser, len(teamMembers[teamSlug]))
			copy(members, teamMembers[teamSlug])
			teamsMu.Unlock()
			result := make([]map[string]interface{}, 0, len(members))
			for _, u := range members {
				result = append(result, userToMap(u))
			}
			writeJSON(w, result)

		// PUT/DELETE/GET /api/v3/orgs/{org}/teams/{slug}/memberships/{user}
		case strings.HasPrefix(subPath, "memberships/"):
			username := strings.TrimPrefix(subPath, "memberships/")
			switch r.Method {
			case http.MethodGet:
				// Return membership state if user is in the team, 404 otherwise.
				teamsMu.Lock()
				found := false
				for _, m := range teamMembers[teamSlug] {
					if strings.EqualFold(m.Login, username) {
						found = true
						break
					}
				}
				teamsMu.Unlock()
				if found {
					writeJSON(w, map[string]interface{}{"state": "active", "role": "member"})
				} else {
					http.Error(w, "not found", http.StatusNotFound)
				}
			case http.MethodPut:
				if u, ok := lookupUser(username); ok {
					teamsMu.Lock()
					// Add only if not already present.
					found := false
					for _, m := range teamMembers[teamSlug] {
						if strings.EqualFold(m.Login, username) {
							found = true
							break
						}
					}
					if !found {
						teamMembers[teamSlug] = append(teamMembers[teamSlug], u)
					}
					teamsMu.Unlock()
				}
				writeJSON(w, map[string]interface{}{"state": "active", "role": "member"})
			case http.MethodDelete:
				teamsMu.Lock()
				var newList []MockUser
				for _, m := range teamMembers[teamSlug] {
					if !strings.EqualFold(m.Login, username) {
						newList = append(newList, m)
					}
				}
				teamMembers[teamSlug] = newList
				teamsMu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}

		// PUT/DELETE /api/v3/orgs/{org}/teams/{slug}/repos/{owner}/{repo}
		case strings.HasPrefix(subPath, "repos/"):
			// subPath is "repos/{owner}/{repo}" — strip the owner segment too.
			repoPath := strings.TrimPrefix(subPath, "repos/")
			repoParts := strings.SplitN(repoPath, "/", 2)
			if len(repoParts) < 2 {
				http.NotFound(w, r)
				return
			}
			repoName := repoParts[1]
			switch r.Method {
			case http.MethodPut:
				var body struct {
					Permission string `json:"permission"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeJSONError(w, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
					return
				}
				if body.Permission == "" {
					body.Permission = "pull"
				}
				teamsMu.Lock()
				// Validate the repo exists; mirror real GitHub 404 behaviour for
				// unknown repositories.  Team existence is not validated here
				// because seeded teams may be deleted by other concurrent
				// reconcilers during the test suite, causing spurious 404s.
				repoFound := lookupRepo(repoName) != nil
				if !repoFound {
					teamsMu.Unlock()
					writeJSONError(w, `{"message":"Not Found"}`, http.StatusNotFound)
					return
				}
				perms := teamRepoPerms[repoName]
				found := false
				for i, tp := range perms {
					if tp.Slug == teamSlug {
						perms[i].Permission = body.Permission
						found = true
						break
					}
				}
				if !found {
					// Look up the team ID.
					var teamID int64
					for _, t := range teams {
						if t.Slug == teamSlug {
							teamID = t.ID
							break
						}
					}
					perms = append(perms, MockTeamWithPermission{Slug: teamSlug, ID: teamID, Permission: body.Permission})
				}
				teamRepoPerms[repoName] = perms
				teamsMu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			case http.MethodDelete:
				teamsMu.Lock()
				perms := teamRepoPerms[repoName]
				newPerms := perms[:0]
				for _, tp := range perms {
					if tp.Slug != teamSlug {
						newPerms = append(newPerms, tp)
					}
				}
				teamRepoPerms[repoName] = newPerms
				teamsMu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}

		default:
			http.Error(w, fmt.Sprintf("mock: unhandled team path: %s %s/%s", r.Method, teamSlug, subPath), http.StatusNotFound)
		}
	})

	// ---- repos ----

	// GET  /api/v3/orgs/{org}/repos  (list org repos)
	// POST /api/v3/orgs/{org}/repos  (create repo)
	mux.HandleFunc(fmt.Sprintf("/api/v3/orgs/%s/repos", org), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			teamsMu.Lock()
			snapshot := make([]MockRepo, len(repos))
			copy(snapshot, repos)
			teamsMu.Unlock()
			result := make([]map[string]interface{}, 0, len(snapshot))
			for _, repo := range snapshot {
				result = append(result, map[string]interface{}{
					"name":      repo.Name,
					"private":   repo.Private,
					"full_name": fmt.Sprintf("%s/%s", org, repo.Name),
				})
			}
			writeJSON(w, result)
		case http.MethodPost:
			var body struct {
				Name    string `json:"name"`
				Private bool   `json:"private"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSONError(w, `{"message":"Problems parsing JSON"}`, http.StatusBadRequest)
				return
			}
			if body.Name == "" {
				body.Name = "new-repo"
			}
			teamsMu.Lock()
			if lookupRepo(body.Name) != nil {
				teamsMu.Unlock()
				writeJSONError(w, `{"message":"Validation Failed","errors":[{"resource":"Repository","code":"already_exists"}]}`, http.StatusUnprocessableEntity)
				return
			}
			repos = append(repos, MockRepo{Name: body.Name, Private: body.Private})
			teamsMu.Unlock()
			writeJSONCreated(w, map[string]interface{}{
				"name":      body.Name,
				"private":   body.Private,
				"full_name": fmt.Sprintf("%s/%s", org, body.Name),
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /api/v3/repos/{org}/{repo}[/...]
	mux.HandleFunc(fmt.Sprintf("/api/v3/repos/%s/", org), func(w http.ResponseWriter, r *http.Request) {
		prefix := fmt.Sprintf("/api/v3/repos/%s/", org)
		remainder := strings.TrimPrefix(r.URL.Path, prefix)
		parts := strings.SplitN(remainder, "/", 2)
		repoName := parts[0]
		var subPath string
		if len(parts) > 1 {
			subPath = parts[1]
		}

		teamsMu.Lock()
		repoEntry := lookupRepo(repoName)
		var repoSnapshot *MockRepo
		if repoEntry != nil {
			cp := *repoEntry
			repoSnapshot = &cp
		}
		teamsMu.Unlock()

		switch {
		// GET /api/v3/repos/{org}/{repo}
		case subPath == "" && r.Method == http.MethodGet:
			if repoSnapshot == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]interface{}{
				"name":       repoSnapshot.Name,
				"private":    repoSnapshot.Private,
				"visibility": visibilityStr(repoSnapshot.Private),
				"full_name":  fmt.Sprintf("%s/%s", org, repoSnapshot.Name),
			})

		// DELETE /api/v3/repos/{org}/{repo}
		case subPath == "" && r.Method == http.MethodDelete:
			teamsMu.Lock()
			newRepos := repos[:0]
			for _, rp := range repos {
				if rp.Name != repoName {
					newRepos = append(newRepos, rp)
				}
			}
			repos = newRepos
			delete(teamRepoPerms, repoName)
			teamsMu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		// GET /api/v3/repos/{org}/{repo}/teams
		case subPath == "teams" && r.Method == http.MethodGet:
			if repoSnapshot == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			teamsMu.Lock()
			perms := make([]MockTeamWithPermission, len(teamRepoPerms[repoName]))
			copy(perms, teamRepoPerms[repoName])
			teamsMu.Unlock()
			result := make([]map[string]interface{}, 0, len(perms))
			for _, tp := range perms {
				result = append(result, map[string]interface{}{
					"id":         tp.ID,
					"slug":       tp.Slug,
					"name":       tp.Slug,
					"permission": tp.Permission,
				})
			}
			writeJSON(w, result)

		// GET /api/v3/repos/{org}/{repo}/collaborators
		case subPath == "collaborators" && r.Method == http.MethodGet:
			if repoSnapshot == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSON(w, []interface{}{})

		// DELETE /api/v3/repos/{org}/{repo}/collaborators/{user}
		case strings.HasPrefix(subPath, "collaborators/") && r.Method == http.MethodDelete:
			if repoSnapshot == nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, fmt.Sprintf("mock: unhandled repos path: %s %s/%s", r.Method, repoName, subPath), http.StatusNotFound)
		}
	})

	// ---- users ----

	// GET /api/v3/users/{username}
	mux.HandleFunc("/api/v3/users/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		username := strings.TrimPrefix(r.URL.Path, "/api/v3/users/")
		for _, u := range cfg.Members {
			if strings.EqualFold(u.Login, username) {
				writeJSON(w, userToMap(u))
				return
			}
		}
		for _, u := range cfg.Owners {
			if strings.EqualFold(u.Login, username) {
				writeJSON(w, userToMap(u))
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	// GET /api/v3/user/{id}  — GetByID
	// Also handles /api/v3/user/memberships/ (returns 404 for sub-paths)
	mux.HandleFunc("/api/v3/user/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v3/user/")
		// delegate /user/memberships/... to the registered handler above
		if strings.HasPrefix(path, "memberships/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		idStr := path
		for _, u := range cfg.Members {
			if fmt.Sprintf("%d", u.ID) == idStr {
				writeJSON(w, userToMap(u))
				return
			}
		}
		for _, u := range cfg.Owners {
			if fmt.Sprintf("%d", u.ID) == idStr {
				writeJSON(w, userToMap(u))
				return
			}
		}
		http.Error(w, "not found", http.StatusNotFound)
	})
}

// userToMap converts a MockUser to a JSON-serialisable map with the field
// shapes that go-github v85 expects.
func userToMap(u MockUser) map[string]interface{} {
	return map[string]interface{}{
		"id":    u.ID,
		"login": u.Login,
		"type":  "User",
	}
}

// teamToMap converts a MockTeam into the JSON map shape go-github expects.
func teamToMap(team MockTeam) map[string]interface{} {
	return map[string]interface{}{
		"id":   team.ID,
		"name": team.Name,
		"slug": team.Slug,
	}
}

func visibilityStr(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	// Encode errors are ignored: headers are already committed and we cannot
	// send an error response at this point without corrupting the stream.
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes an error response with Content-Type: application/json.
// This mirrors the real GitHub API which returns JSON bodies on errors, allowing
// go-github's error-response decoder to parse the message field.
func writeJSONError(w http.ResponseWriter, body string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

// writeJSONCreated writes a 201 Created response with a JSON body.
// Content-Type must be set before WriteHeader; this helper ensures correct ordering.
// Encode errors are silently ignored because headers are already committed by the
// time Encode writes the body and there is no way to signal the error to the client.
func writeJSONCreated(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(v)
}
