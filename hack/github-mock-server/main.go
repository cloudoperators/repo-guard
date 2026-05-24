// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

// github-mock-server is a standalone HTTP server that serves canned GitHub API
// responses.  It is used by the e2e test harness to replace the real GitHub API
// when running in mock mode (USE_MOCK_GITHUB=true), allowing CI pipelines and
// local e2e runs to work without real GitHub credentials.
//
// Configuration is provided entirely through environment variables so the
// binary can be run as a Kubernetes Deployment inside a k3d cluster without
// any additional configuration files.
//
// Required environment variables:
//
//	MOCK_GITHUB_ORG            - GitHub organisation name (default: greenhouse-sandbox)
//
// Optional member / team / repo variables (comma-separated lists):
//
//	MOCK_GITHUB_MEMBERS        - comma-separated "login:id" pairs, e.g. "alice:1,bob:2"
//	MOCK_GITHUB_OWNERS         - comma-separated "login:id" pairs for org owners
//	MOCK_GITHUB_TEAMS          - comma-separated "slug:id" pairs for teams, e.g. "team-a:10,team-b:11"
//	MOCK_GITHUB_REPOS          - comma-separated "name:public|private" pairs, e.g. "repo1:public,repo2:private"
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	internalgithub "github.com/cloudoperators/repo-guard/internal/github"
)

func main() {
	var listen string
	flag.StringVar(&listen, "listen", ":8080", "listen address, e.g., :8080")
	flag.Parse()

	cfg := buildConfig()

	mux := internalgithub.NewMockGitHubMux(cfg)

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen error: %v", err)
	}
	addr := ln.Addr().String()
	log.Printf("Mock GitHub server listening at http://%s (org=%s, members=%d, teams=%d, repos=%d)",
		addr, cfg.Org, len(cfg.Members), len(cfg.Teams), len(cfg.Repos))

	srv := &http.Server{Handler: mux}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve error: %v", err)
	}
}

// buildConfig reads environment variables and constructs a MockConfig.
func buildConfig() internalgithub.MockConfig {
	org := envOrDefault("MOCK_GITHUB_ORG", "greenhouse-sandbox")

	return internalgithub.MockConfig{
		Org:     org,
		Members: parseUsers(os.Getenv("MOCK_GITHUB_MEMBERS")),
		Owners:  parseUsers(os.Getenv("MOCK_GITHUB_OWNERS")),
		Teams:   parseTeams(os.Getenv("MOCK_GITHUB_TEAMS")),
		Repos:   parseRepos(os.Getenv("MOCK_GITHUB_REPOS")),
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseUsers parses a comma-separated list of "login:id" pairs.
// Example: "alice:1,bob:2"
func parseUsers(s string) []internalgithub.MockUser {
	var users []internalgithub.MockUser
	for _, item := range splitNonEmpty(s) {
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			log.Printf("WARN: skipping malformed user entry %q (expected login:id)", item)
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			log.Printf("WARN: skipping user %q: invalid id %q: %v", parts[0], parts[1], err)
			continue
		}
		users = append(users, internalgithub.MockUser{
			Login: strings.TrimSpace(parts[0]),
			ID:    id,
		})
	}
	return users
}

// parseTeams parses a comma-separated list of "slug:id" pairs.
// Example: "team-a:10,team-b:11"
func parseTeams(s string) []internalgithub.MockTeam {
	var teams []internalgithub.MockTeam
	for _, item := range splitNonEmpty(s) {
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			log.Printf("WARN: skipping malformed team entry %q (expected slug:id)", item)
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			log.Printf("WARN: skipping team %q: invalid id %q: %v", parts[0], parts[1], err)
			continue
		}
		slug := strings.TrimSpace(parts[0])
		teams = append(teams, internalgithub.MockTeam{
			ID:   id,
			Name: slug,
			Slug: slug,
		})
	}
	return teams
}

// parseRepos parses a comma-separated list of "name:public|private" pairs.
// Example: "my-repo:public,secret-repo:private"
func parseRepos(s string) []internalgithub.MockRepo {
	var repos []internalgithub.MockRepo
	for _, item := range splitNonEmpty(s) {
		parts := strings.SplitN(item, ":", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		private := false
		if len(parts) == 2 && strings.TrimSpace(parts[1]) == "private" {
			private = true
		}
		repos = append(repos, internalgithub.MockRepo{
			Name:    name,
			Private: private,
		})
	}
	return repos
}

// splitNonEmpty splits s on commas and returns non-empty trimmed items.
func splitNonEmpty(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
