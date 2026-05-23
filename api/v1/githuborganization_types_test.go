// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"
)

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
