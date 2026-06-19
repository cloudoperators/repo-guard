// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"
)

// set builds a map[string]struct{} from a slice of strings.
func set(members ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(members))
	for _, s := range members {
		m[s] = struct{}{}
	}
	return m
}

func TestRepoChangeCalculatorMethod(t *testing.T) {
	repoWithTeam := GithubRepository{
		Name: "repo1",
		Teams: []GithubTeamWithPermission{
			{Team: "existing-team", Permission: GithubTeamPermissionPush},
		},
	}

	tests := []struct {
		name          string
		privateTeams  []GithubTeamWithPermission
		publicTeams   []GithubTeamWithPermission
		internalTeams []GithubTeamWithPermission
		privateRepos  []GithubRepository
		publicRepos   []GithubRepository
		internalRepos []GithubRepository
		seedStatus    GithubOrganizationState
		seedError     string
		wantChanged   bool
		wantStatus    GithubOrganizationState
		wantOpsLen    int
	}{
		{
			name:        "all three default team lists empty — no change, no failure",
			wantChanged: false,
			wantStatus:  "",
		},
		{
			// Empty private list must not generate REMOVE ops for repos that
			// have existing teams — it means "no policy for private repos".
			name:         "only private list empty — no ops generated even with existing repo teams",
			privateTeams: nil,
			publicTeams:  []GithubTeamWithPermission{{Team: "all-read", Permission: GithubTeamPermissionPull}},
			privateRepos: []GithubRepository{repoWithTeam},
			wantChanged:  false,
			wantOpsLen:   0,
		},
		{
			// Empty public list must not generate REMOVE ops for repos that
			// have existing teams — it means "no policy for public repos".
			name:         "only public list empty — no ops generated even with existing repo teams",
			privateTeams: []GithubTeamWithPermission{{Team: "all-read", Permission: GithubTeamPermissionPull}},
			publicTeams:  nil,
			publicRepos:  []GithubRepository{repoWithTeam},
			wantChanged:  false,
			wantOpsLen:   0,
		},
		{
			// Empty internal list must not generate any ops for internal repos.
			// All team lists are nil, so no bucket participates.
			name:          "only internal list empty — no ops generated even with existing repo teams",
			privateTeams:  nil,
			internalTeams: nil,
			internalRepos: []GithubRepository{repoWithTeam},
			wantChanged:   false,
			wantOpsLen:    0,
		},
		{
			// Production recovery: org stuck in failed due to old DefaultPrivateRepositoryTeams guard.
			name:        "all three lists empty, stuck in failed (private error) — clears failure",
			seedStatus:  GithubOrganizationStateFailed,
			seedError:   "DefaultPrivateRepositoryTeams is empty",
			wantChanged: true,
			wantStatus:  GithubOrganizationStateComplete,
		},
		{
			// Production recovery: org stuck in failed due to old DefaultPublicRepositoryTeams guard.
			name:        "all three lists empty, stuck in failed (public error) — clears failure",
			seedStatus:  GithubOrganizationStateFailed,
			seedError:   "DefaultPublicRepositoryTeams is empty",
			wantChanged: true,
			wantStatus:  GithubOrganizationStateComplete,
		},
		{
			name:          "internal team missing from internal repo generates ADD op",
			internalTeams: []GithubTeamWithPermission{{Team: "internal-guard", Permission: GithubTeamPermissionPull}},
			internalRepos: []GithubRepository{{Name: "secret-repo", Teams: []GithubTeamWithPermission{}}},
			wantChanged:   true,
			wantOpsLen:    1,
		},
		{
			name:          "all three lists present — ops generated independently",
			publicTeams:   []GithubTeamWithPermission{{Team: "pub-team", Permission: GithubTeamPermissionPull}},
			privateTeams:  []GithubTeamWithPermission{{Team: "priv-team", Permission: GithubTeamPermissionPull}},
			internalTeams: []GithubTeamWithPermission{{Team: "int-team", Permission: GithubTeamPermissionPull}},
			publicRepos:   []GithubRepository{{Name: "pub-repo", Teams: []GithubTeamWithPermission{}}},
			privateRepos:  []GithubRepository{{Name: "priv-repo", Teams: []GithubTeamWithPermission{}}},
			internalRepos: []GithubRepository{{Name: "int-repo", Teams: []GithubTeamWithPermission{}}},
			wantChanged:   true,
			wantOpsLen:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := GithubOrganization{}
			org.Spec.DefaultPrivateRepositoryTeams = tt.privateTeams
			org.Spec.DefaultPublicRepositoryTeams = tt.publicTeams
			org.Spec.DefaultInternalRepositoryTeams = tt.internalTeams
			org.Status.PrivateRepositories = tt.privateRepos
			org.Status.PublicRepositories = tt.publicRepos
			org.Status.InternalRepositories = tt.internalRepos
			org.Status.OrganizationStatus = tt.seedStatus
			org.Status.OrganizationStatusError = tt.seedError
			changed, newStatus := org.RepoChangeCalculator(nil)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if tt.wantStatus != "" && newStatus.OrganizationStatus != tt.wantStatus {
				t.Errorf("OrganizationStatus = %q, want %q", newStatus.OrganizationStatus, tt.wantStatus)
			}
			if tt.wantOpsLen >= 0 && len(newStatus.Operations.RepositoryTeamOperations) != tt.wantOpsLen {
				t.Errorf("RepositoryTeamOperations len = %d, want %d: %+v",
					len(newStatus.Operations.RepositoryTeamOperations), tt.wantOpsLen, newStatus.Operations.RepositoryTeamOperations)
			}
		})
	}
}

// teamOp is a helper to build a GithubRepoTeamOperation for test setup.
func teamOp(team, repo string, opType GithubRepoTeamOperationType, state GithubRepoTeamOperationState) GithubRepoTeamOperation {
	return GithubRepoTeamOperation{
		Team:      team,
		Repo:      repo,
		Operation: opType,
		State:     state,
	}
}

func TestRepoChangeCalculator(t *testing.T) {
	tests := []struct {
		name          string
		defaultConfig []GithubTeamWithPermission
		actual        []GithubRepository
		operations    []GithubRepoTeamOperation
		wantOps       []GithubRepoTeamOperation
	}{
		{
			name: "team missing from repo generates ADD op",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "my-team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{}},
			},
			operations: nil,
			wantOps: []GithubRepoTeamOperation{
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionPush, State: GithubRepoTeamOperationStatePending},
			},
		},
		{
			name: "team present with correct permission generates no op",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "my-team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{
					{Team: "my-team", Permission: GithubTeamPermissionPush},
				}},
			},
			operations: nil,
			wantOps:    []GithubRepoTeamOperation{},
		},
		{
			// Both the "ensure default teams" loop and the "iterate repo teams" loop
			// independently detect permission drift and each emit REMOVE+ADD. The
			// dedup only fires against the *incoming* operations slice, not against
			// ops already accumulated in the same call. CleanCompletedOperations and
			// the cross-reconcile dedup handle the real-world case.
			name: "team present with wrong permission generates REMOVE and ADD ops (both loops fire)",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "my-team", Permission: GithubTeamPermissionAdmin},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{
					{Team: "my-team", Permission: GithubTeamPermissionPush},
				}},
			},
			operations: nil,
			wantOps: []GithubRepoTeamOperation{
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeRemove, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionAdmin, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeRemove, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionAdmin, State: GithubRepoTeamOperationStatePending},
			},
		},
		{
			name:          "team in repo but not in config generates REMOVE op",
			defaultConfig: []GithubTeamWithPermission{},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{
					{Team: "stale-team", Permission: GithubTeamPermissionPush},
				}},
			},
			operations: nil,
			wantOps: []GithubRepoTeamOperation{
				{Team: "stale-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeRemove, State: GithubRepoTeamOperationStatePending},
			},
		},
		{
			name: "existing pending ADD op deduplicates — no new ADD generated",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "my-team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{}},
			},
			operations: []GithubRepoTeamOperation{
				teamOp("my-team", "repo1", GithubRepoTeamOperationTypeAdd, GithubRepoTeamOperationStatePending),
			},
			wantOps: []GithubRepoTeamOperation{},
		},
		{
			name: "existing completed ADD op deduplicates — no new ADD generated",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "my-team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{}},
			},
			operations: []GithubRepoTeamOperation{
				teamOp("my-team", "repo1", GithubRepoTeamOperationTypeAdd, GithubRepoTeamOperationStateComplete),
			},
			wantOps: []GithubRepoTeamOperation{},
		},
		{
			// Upgrade-safety: op.Team stored as display name before the slug fix was deployed.
			// slug.Make("My Team") == "my-team", so the dedup must still fire.
			name: "existing ADD op with display-name (pre-fix) deduplicates against slug config team",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "My Team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{}},
			},
			operations: []GithubRepoTeamOperation{
				// stored with the old display name before the slug fix
				teamOp("My Team", "repo1", GithubRepoTeamOperationTypeAdd, GithubRepoTeamOperationStatePending),
			},
			wantOps: []GithubRepoTeamOperation{},
		},
		{
			// Spec team name with spaces/uppercase must be normalised to slug in the new op.
			name: "display-name spec team generates ADD op with slugified Team field",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "My Awesome Team", Permission: GithubTeamPermissionPull},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{}},
			},
			operations: nil,
			wantOps: []GithubRepoTeamOperation{
				{Team: "my-awesome-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionPull, State: GithubRepoTeamOperationStatePending},
			},
		},
		{
			// GitHub returns the slug ("my-team") in repo.Teams.  The spec has the
			// display name ("My Team").  After normalisation both resolve to "my-team"
			// so no operation should be generated.
			name: "spec display name matches github slug — no op generated",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "My Team", Permission: GithubTeamPermissionPush},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{
					{Team: "my-team", Permission: GithubTeamPermissionPush},
				}},
			},
			operations: nil,
			wantOps:    []GithubRepoTeamOperation{},
		},
		{
			// Same as above: both loops fire for permission drift.
			name: "permission drift with display-name spec team generates REMOVE and ADD with slug (both loops fire)",
			defaultConfig: []GithubTeamWithPermission{
				{Team: "My Team", Permission: GithubTeamPermissionAdmin},
			},
			actual: []GithubRepository{
				{Name: "repo1", Teams: []GithubTeamWithPermission{
					{Team: "my-team", Permission: GithubTeamPermissionPush},
				}},
			},
			operations: nil,
			wantOps: []GithubRepoTeamOperation{
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeRemove, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionAdmin, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeRemove, State: GithubRepoTeamOperationStatePending},
				{Team: "my-team", Repo: "repo1", Operation: GithubRepoTeamOperationTypeAdd, Permission: GithubTeamPermissionAdmin, State: GithubRepoTeamOperationStatePending},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoChangeCalculator(tt.defaultConfig, tt.actual, nil, nil, tt.operations)

			if len(got) != len(tt.wantOps) {
				t.Fatalf("got %d ops, want %d ops\ngot:  %+v\nwant: %+v", len(got), len(tt.wantOps), got, tt.wantOps)
			}

			for i, want := range tt.wantOps {
				g := got[i]
				if g.Team != want.Team {
					t.Errorf("op[%d].Team = %q, want %q", i, g.Team, want.Team)
				}
				if g.Repo != want.Repo {
					t.Errorf("op[%d].Repo = %q, want %q", i, g.Repo, want.Repo)
				}
				if g.Operation != want.Operation {
					t.Errorf("op[%d].Operation = %q, want %q", i, g.Operation, want.Operation)
				}
				if want.Permission != "" && g.Permission != want.Permission {
					t.Errorf("op[%d].Permission = %q, want %q", i, g.Permission, want.Permission)
				}
				if g.State != want.State {
					t.Errorf("op[%d].State = %q, want %q", i, g.State, want.State)
				}
			}
		})
	}
}

func TestOrganizationMemberChangeCalculator(t *testing.T) {
	tests := []struct {
		name                  string
		orgMembers            []string
		orgOwners             []string
		teamMembers           map[string]struct{}
		protected             []string
		teamObservationsCount int
		existingOps           []GithubUserOperation
		wantChanged           bool
		wantOpsLen            int
		wantRemoveUsers       []string
	}{
		{
			name:                  "member in a team — no op",
			orgMembers:            []string{"alice"},
			orgOwners:             []string{},
			teamMembers:           set("alice"),
			protected:             nil,
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            0,
		},
		{
			name:                  "member not in any team — remove op queued",
			orgMembers:            []string{"bob"},
			orgOwners:             []string{},
			teamMembers:           set(),
			protected:             nil,
			teamObservationsCount: 1,
			wantChanged:           true,
			wantOpsLen:            1,
			wantRemoveUsers:       []string{"bob"},
		},
		{
			name:                  "member is org owner — no op",
			orgMembers:            []string{"carol"},
			orgOwners:             []string{"carol"},
			teamMembers:           set(),
			protected:             nil,
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            0,
		},
		{
			name:                  "member is in protected list — no op",
			orgMembers:            []string{"bot-user"},
			orgOwners:             []string{},
			teamMembers:           set(),
			protected:             []string{"bot-user"},
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            0,
		},
		{
			name:                  "teamObservationsCount == 0 — safety rail, no ops generated",
			orgMembers:            []string{"dave", "eve"},
			orgOwners:             []string{},
			teamMembers:           set(),
			protected:             nil,
			teamObservationsCount: 0,
			wantChanged:           false,
			wantOpsLen:            0,
		},
		{
			name:        "existing pending op for same user — not duplicated",
			orgMembers:  []string{"frank"},
			orgOwners:   []string{},
			teamMembers: set(),
			protected:   nil,
			existingOps: []GithubUserOperation{
				{User: "frank", Operation: GithubUserOperationTypeRemove, State: GithubUserOperationStatePending},
			},
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            1,
		},
		{
			name:        "existing failed op for same user — no new pending op generated",
			orgMembers:  []string{"grace"},
			orgOwners:   []string{},
			teamMembers: set(),
			protected:   nil,
			existingOps: []GithubUserOperation{
				{User: "grace", Operation: GithubUserOperationTypeRemove, State: GithubUserOperationStateFailed},
			},
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            1,
		},
		{
			name:                  "mixed: one team member, one non-member — only non-member gets remove op",
			orgMembers:            []string{"alice", "bob"},
			orgOwners:             []string{},
			teamMembers:           set("alice"),
			protected:             nil,
			teamObservationsCount: 2,
			wantChanged:           true,
			wantOpsLen:            1,
			wantRemoveUsers:       []string{"bob"},
		},
		{
			name:                  "case-insensitive: owner stored as uppercase — still skipped",
			orgMembers:            []string{"Admin"},
			orgOwners:             []string{"admin"},
			teamMembers:           set(),
			protected:             nil,
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            0,
		},
		{
			name:                  "case-insensitive: protected stored as uppercase — still skipped",
			orgMembers:            []string{"Bot"},
			orgOwners:             []string{},
			teamMembers:           set(),
			protected:             []string{"BOT"},
			teamObservationsCount: 1,
			wantChanged:           false,
			wantOpsLen:            0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &GithubOrganization{}
			org.Status.Operations.OrganizationMemberOperations = tt.existingOps

			changed, newStatus := org.OrganizationMemberChangeCalculator(
				tt.orgMembers,
				tt.orgOwners,
				tt.teamMembers,
				tt.protected,
				tt.teamObservationsCount,
			)

			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}

			got := newStatus.Operations.OrganizationMemberOperations
			if len(got) != tt.wantOpsLen {
				t.Fatalf("len(OrganizationMemberOperations) = %d, want %d\ngot: %+v", len(got), tt.wantOpsLen, got)
			}

			for _, wantUser := range tt.wantRemoveUsers {
				found := false
				for _, op := range got {
					if op.User == wantUser && op.Operation == GithubUserOperationTypeRemove && op.State == GithubUserOperationStatePending {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected remove op for user %q not found in ops: %+v", wantUser, got)
				}
			}

			if tt.wantChanged && newStatus.OrganizationStatus != GithubOrganizationStatePendingOperations {
				t.Errorf("OrganizationStatus = %q, want %q", newStatus.OrganizationStatus, GithubOrganizationStatePendingOperations)
			}
		})
	}
}

func TestRepositoryDirectCollaboratorChangeCalculator(t *testing.T) {
	tests := []struct {
		name              string
		repoCollaborators map[string][]string
		orgOwners         []string
		protected         []string
		existingOps       []GithubRepoUserOperation
		wantChanged       bool
		wantOpsLen        int
		wantRemove        []struct{ repo, user string }
	}{
		{
			name:              "collaborator — remove op (team membership irrelevant)",
			repoCollaborators: map[string][]string{"repo1": {"alice"}},
			orgOwners:         nil,
			protected:         nil,
			wantChanged:       true,
			wantOpsLen:        1,
			wantRemove:        []struct{ repo, user string }{{"repo1", "alice"}},
		},
		{
			name:              "collaborator (different user) — remove op",
			repoCollaborators: map[string][]string{"repo1": {"bob"}},
			orgOwners:         nil,
			protected:         nil,
			wantChanged:       true,
			wantOpsLen:        1,
			wantRemove:        []struct{ repo, user string }{{"repo1", "bob"}},
		},
		{
			name:              "collaborator is org owner — no op",
			repoCollaborators: map[string][]string{"repo1": {"carol"}},
			orgOwners:         []string{"carol"},
			protected:         nil,
			wantChanged:       false,
			wantOpsLen:        0,
		},
		{
			name:              "collaborator is protected — no op",
			repoCollaborators: map[string][]string{"repo1": {"deploy-bot"}},
			orgOwners:         nil,
			protected:         []string{"deploy-bot"},
			wantChanged:       false,
			wantOpsLen:        0,
		},
		{
			name:              "outside collaborator — repo has no teams — remove op",
			repoCollaborators: map[string][]string{"repo1": {"outsider"}},
			orgOwners:         nil,
			protected:         nil,
			wantChanged:       true,
			wantOpsLen:        1,
			wantRemove:        []struct{ repo, user string }{{"repo1", "outsider"}},
		},
		{
			name:              "multiple repos — all collaborators get ops",
			repoCollaborators: map[string][]string{"repo1": {"alice", "dave"}, "repo2": {"eve"}},
			orgOwners:         nil,
			protected:         nil,
			wantChanged:       true,
			wantOpsLen:        3,
			wantRemove: []struct{ repo, user string }{
				{"repo1", "alice"},
				{"repo1", "dave"},
				{"repo2", "eve"},
			},
		},
		{
			name:              "existing pending op for same repo+user — not duplicated",
			repoCollaborators: map[string][]string{"repo1": {"frank"}},
			orgOwners:         nil,
			protected:         nil,
			existingOps: []GithubRepoUserOperation{
				{Repo: "repo1", User: "frank", Operation: GithubRepoUserOperationTypeRemove, State: GithubRepoUserOperationStatePending},
			},
			wantChanged: false,
			wantOpsLen:  1,
		},
		{
			name:              "existing failed op for same repo+user — no new pending op",
			repoCollaborators: map[string][]string{"repo1": {"grace"}},
			orgOwners:         nil,
			protected:         nil,
			existingOps: []GithubRepoUserOperation{
				{Repo: "repo1", User: "grace", Operation: GithubRepoUserOperationTypeRemove, State: GithubRepoUserOperationStateFailed},
			},
			wantChanged: false,
			wantOpsLen:  1,
		},
		{
			name:              "case-insensitive owner match — no op",
			repoCollaborators: map[string][]string{"repo1": {"Admin"}},
			orgOwners:         []string{"admin"},
			protected:         nil,
			wantChanged:       false,
			wantOpsLen:        0,
		},
		{
			name:              "no collaborators — no ops",
			repoCollaborators: map[string][]string{},
			orgOwners:         nil,
			protected:         nil,
			wantChanged:       false,
			wantOpsLen:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &GithubOrganization{}
			org.Status.Operations.RepositoryCollaboratorOperations = tt.existingOps

			changed, newStatus := org.RepositoryDirectCollaboratorChangeCalculator(
				tt.repoCollaborators,
				tt.orgOwners,
				tt.protected,
			)

			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}

			got := newStatus.Operations.RepositoryCollaboratorOperations
			if len(got) != tt.wantOpsLen {
				t.Fatalf("len(RepositoryCollaboratorOperations) = %d, want %d\ngot: %+v", len(got), tt.wantOpsLen, got)
			}

			for _, want := range tt.wantRemove {
				found := false
				for _, op := range got {
					if op.Repo == want.repo && op.User == want.user &&
						op.Operation == GithubRepoUserOperationTypeRemove &&
						op.State == GithubRepoUserOperationStatePending {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected remove op for repo=%q user=%q not found in ops: %+v", want.repo, want.user, got)
				}
			}

			if tt.wantChanged && newStatus.OrganizationStatus != GithubOrganizationStatePendingOperations {
				t.Errorf("OrganizationStatus = %q, want %q", newStatus.OrganizationStatus, GithubOrganizationStatePendingOperations)
			}
		})
	}
}
