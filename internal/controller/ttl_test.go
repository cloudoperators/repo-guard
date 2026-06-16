// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
)

// makeUserOp builds a GithubUserOperation with the given state and timestamp.
func makeUserOp(user string, state v1.GithubUserOperationState, ts time.Time) v1.GithubUserOperation {
	return v1.GithubUserOperation{
		Operation: v1.GithubUserOperationTypeAdd,
		User:      user,
		State:     state,
		Timestamp: metav1.NewTime(ts),
	}
}

// makeRepoOp builds a GithubRepoTeamOperation with the given state and timestamp.
func makeRepoOp(repo, team string, state v1.GithubRepoTeamOperationState, ts time.Time) v1.GithubRepoTeamOperation {
	return v1.GithubRepoTeamOperation{
		Operation: v1.GithubRepoTeamOperationTypeAdd,
		Repo:      repo,
		Team:      team,
		State:     state,
		Timestamp: metav1.NewTime(ts),
	}
}

// makeTeamOp builds a GithubTeamOperation with the given state and timestamp.
func makeTeamOp(team string, state v1.GithubUserOperationState, ts time.Time) v1.GithubTeamOperation {
	return v1.GithubTeamOperation{
		Operation: v1.GithubTeamOperationTypeAdd,
		Team:      team,
		State:     state,
		Timestamp: metav1.NewTime(ts),
	}
}

func TestApplyUserOpsTTL(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour) // older than 24h
	fresh := now.Add(-1 * time.Hour)

	tests := []struct {
		name        string
		ops         []v1.GithubUserOperation
		ttl         time.Duration
		state       v1.GithubUserOperationState
		wantOps     []string // user names that survive
		wantChanged bool
	}{
		{
			name: "non-matching state is preserved even when aged",
			ops: []v1.GithubUserOperation{
				makeUserOp("alice", v1.GithubUserOperationStateComplete, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{"alice"},
			wantChanged: false,
		},
		{
			name: "zero timestamp preserved even when matching state",
			ops: []v1.GithubUserOperation{
				{Operation: v1.GithubUserOperationTypeAdd, User: "alice", State: v1.GithubUserOperationStateFailed},
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{"alice"},
			wantChanged: false,
		},
		{
			name: "aged matching op is dropped",
			ops: []v1.GithubUserOperation{
				makeUserOp("alice", v1.GithubUserOperationStateFailed, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{},
			wantChanged: true,
		},
		{
			name: "fresh matching op is kept",
			ops: []v1.GithubUserOperation{
				makeUserOp("alice", v1.GithubUserOperationStateFailed, fresh),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{"alice"},
			wantChanged: false,
		},
		{
			name: "mixed aged and fresh: only aged dropped",
			ops: []v1.GithubUserOperation{
				makeUserOp("alice", v1.GithubUserOperationStateFailed, old),
				makeUserOp("bob", v1.GithubUserOperationStateFailed, fresh),
				makeUserOp("carol", v1.GithubUserOperationStateComplete, old),
				makeUserOp("dave", v1.GithubUserOperationStateFailed, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{"bob", "carol"},
			wantChanged: true,
		},
		{
			name:        "empty input slice",
			ops:         []v1.GithubUserOperation{},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantOps:     []string{},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := applyUserOpsTTL(tt.ops, tt.ttl, tt.state, now)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if len(got) != len(tt.wantOps) {
				t.Fatalf("got %d ops, want %d (got=%v want=%v)", len(got), len(tt.wantOps), got, tt.wantOps)
			}
			for i, op := range got {
				if op.User != tt.wantOps[i] {
					t.Errorf("ops[%d].User = %q, want %q", i, op.User, tt.wantOps[i])
				}
			}
		})
	}
}

func TestApplyRepoOpsTTL(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	tests := []struct {
		name        string
		ops         []v1.GithubRepoTeamOperation
		ttl         time.Duration
		state       v1.GithubRepoTeamOperationState
		wantRepos   []string
		wantChanged bool
	}{
		{
			name:        "non-matching state preserved",
			ops:         []v1.GithubRepoTeamOperation{makeRepoOp("r1", "t1", v1.GithubRepoTeamOperationStateComplete, old)},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoTeamOperationStateFailed,
			wantRepos:   []string{"r1"},
			wantChanged: false,
		},
		{
			name: "zero timestamp preserved",
			ops: []v1.GithubRepoTeamOperation{
				{Operation: v1.GithubRepoTeamOperationTypeAdd, Repo: "r1", Team: "t1", State: v1.GithubRepoTeamOperationStateFailed},
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoTeamOperationStateFailed,
			wantRepos:   []string{"r1"},
			wantChanged: false,
		},
		{
			name: "mixed aged and fresh: only aged dropped",
			ops: []v1.GithubRepoTeamOperation{
				makeRepoOp("r-old", "t1", v1.GithubRepoTeamOperationStateFailed, old),
				makeRepoOp("r-fresh", "t1", v1.GithubRepoTeamOperationStateFailed, fresh),
				makeRepoOp("r-other-state", "t1", v1.GithubRepoTeamOperationStateComplete, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoTeamOperationStateFailed,
			wantRepos:   []string{"r-fresh", "r-other-state"},
			wantChanged: true,
		},
		{
			name:        "empty input slice",
			ops:         []v1.GithubRepoTeamOperation{},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoTeamOperationStateFailed,
			wantRepos:   []string{},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := applyRepoOpsTTL(tt.ops, tt.ttl, tt.state, now)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if len(got) != len(tt.wantRepos) {
				t.Fatalf("got %d ops, want %d (got=%v want=%v)", len(got), len(tt.wantRepos), got, tt.wantRepos)
			}
			for i, op := range got {
				if op.Repo != tt.wantRepos[i] {
					t.Errorf("ops[%d].Repo = %q, want %q", i, op.Repo, tt.wantRepos[i])
				}
			}
		})
	}
}

// makeRepoUserOp builds a GithubRepoUserOperation with the given state and timestamp.
func makeRepoUserOp(repo, user string, state v1.GithubRepoUserOperationState, ts time.Time) v1.GithubRepoUserOperation {
	return v1.GithubRepoUserOperation{
		Operation: v1.GithubRepoUserOperationTypeRemove,
		Repo:      repo,
		User:      user,
		State:     state,
		Timestamp: metav1.NewTime(ts),
	}
}

func TestApplyRepoUserOpsTTL(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	tests := []struct {
		name        string
		ops         []v1.GithubRepoUserOperation
		ttl         time.Duration
		state       v1.GithubRepoUserOperationState
		wantRepos   []string
		wantChanged bool
	}{
		{
			name:        "non-matching state preserved",
			ops:         []v1.GithubRepoUserOperation{makeRepoUserOp("r1", "u1", v1.GithubRepoUserOperationStateComplete, old)},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoUserOperationStateFailed,
			wantRepos:   []string{"r1"},
			wantChanged: false,
		},
		{
			name: "zero timestamp preserved",
			ops: []v1.GithubRepoUserOperation{
				{Operation: v1.GithubRepoUserOperationTypeRemove, Repo: "r1", User: "u1", State: v1.GithubRepoUserOperationStateFailed},
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoUserOperationStateFailed,
			wantRepos:   []string{"r1"},
			wantChanged: false,
		},
		{
			name: "mixed aged and fresh: only aged dropped",
			ops: []v1.GithubRepoUserOperation{
				makeRepoUserOp("r-old", "u1", v1.GithubRepoUserOperationStateFailed, old),
				makeRepoUserOp("r-fresh", "u1", v1.GithubRepoUserOperationStateFailed, fresh),
				makeRepoUserOp("r-other-state", "u1", v1.GithubRepoUserOperationStateComplete, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoUserOperationStateFailed,
			wantRepos:   []string{"r-fresh", "r-other-state"},
			wantChanged: true,
		},
		{
			name:        "empty input slice",
			ops:         []v1.GithubRepoUserOperation{},
			ttl:         24 * time.Hour,
			state:       v1.GithubRepoUserOperationStateFailed,
			wantRepos:   []string{},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := applyRepoUserOpsTTL(tt.ops, tt.ttl, tt.state, now)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if len(got) != len(tt.wantRepos) {
				t.Fatalf("got %d ops, want %d (got=%v want=%v)", len(got), len(tt.wantRepos), got, tt.wantRepos)
			}
			for i, op := range got {
				if op.Repo != tt.wantRepos[i] {
					t.Errorf("ops[%d].Repo = %q, want %q", i, op.Repo, tt.wantRepos[i])
				}
			}
		})
	}
}

func TestApplyTeamOpsTTL(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	tests := []struct {
		name        string
		ops         []v1.GithubTeamOperation
		ttl         time.Duration
		state       v1.GithubUserOperationState
		wantTeams   []string
		wantChanged bool
	}{
		{
			name:        "non-matching state preserved",
			ops:         []v1.GithubTeamOperation{makeTeamOp("t1", v1.GithubUserOperationStateComplete, old)},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantTeams:   []string{"t1"},
			wantChanged: false,
		},
		{
			name: "zero timestamp preserved",
			ops: []v1.GithubTeamOperation{
				{Operation: v1.GithubTeamOperationTypeAdd, Team: "t1", State: v1.GithubUserOperationStateFailed},
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantTeams:   []string{"t1"},
			wantChanged: false,
		},
		{
			name: "mixed aged and fresh: only aged dropped",
			ops: []v1.GithubTeamOperation{
				makeTeamOp("t-old", v1.GithubUserOperationStateFailed, old),
				makeTeamOp("t-fresh", v1.GithubUserOperationStateFailed, fresh),
				makeTeamOp("t-other-state", v1.GithubUserOperationStateComplete, old),
			},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantTeams:   []string{"t-fresh", "t-other-state"},
			wantChanged: true,
		},
		{
			name:        "empty input slice",
			ops:         []v1.GithubTeamOperation{},
			ttl:         24 * time.Hour,
			state:       v1.GithubUserOperationStateFailed,
			wantTeams:   []string{},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := applyTeamOpsTTL(tt.ops, tt.ttl, tt.state, now)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if len(got) != len(tt.wantTeams) {
				t.Fatalf("got %d ops, want %d (got=%v want=%v)", len(got), len(tt.wantTeams), got, tt.wantTeams)
			}
			for i, op := range got {
				if op.Team != tt.wantTeams[i] {
					t.Errorf("ops[%d].Team = %q, want %q", i, op.Team, tt.wantTeams[i])
				}
			}
		})
	}
}
