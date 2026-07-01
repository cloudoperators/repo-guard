// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package v1

import (
	"testing"
)

func TestGithubTeamChangeCalculator(t *testing.T) {
	tests := []struct {
		name            string
		existingMembers []Member
		existingOps     []GithubUserOperation
		desiredMembers  []Member
		wantChanged     bool
		wantOpsLen      int
		wantAddUsers    []string
	}{
		{
			name:           "desired member not yet present — add op queued",
			desiredMembers: []Member{{GithubUsername: "alice"}},
			wantChanged:    true,
			wantOpsLen:     1,
			wantAddUsers:   []string{"alice"},
		},
		{
			name:            "desired member already in status.Members — no new op",
			existingMembers: []Member{{GithubUsername: "alice"}},
			desiredMembers:  []Member{{GithubUsername: "alice"}},
			wantChanged:     false,
			wantOpsLen:      0,
		},
		{
			name: "desired member has existing failed add op — skipped, no new op",
			existingOps: []GithubUserOperation{
				{User: "bob", Operation: GithubUserOperationTypeAdd, State: GithubUserOperationStateFailed},
			},
			desiredMembers: []Member{{GithubUsername: "bob"}},
			wantChanged:    false,
			wantOpsLen:     1, // existing op preserved, no new one added
		},
		{
			name: "desired member has existing notfound add op — skipped, no new op",
			existingOps: []GithubUserOperation{
				{User: "carol", Operation: GithubUserOperationTypeAdd, State: GithubUserOperationStateNotFound},
			},
			desiredMembers: []Member{{GithubUsername: "carol"}},
			wantChanged:    false,
			wantOpsLen:     1,
		},
		{
			name: "desired member has existing pending add op — no duplicate",
			existingOps: []GithubUserOperation{
				{User: "dave", Operation: GithubUserOperationTypeAdd, State: GithubUserOperationStatePending},
			},
			desiredMembers: []Member{{GithubUsername: "dave"}},
			wantChanged:    false,
			wantOpsLen:     1,
		},
		{
			name: "case-insensitive: failed op stored as uppercase — still skipped",
			existingOps: []GithubUserOperation{
				{User: "Eve", Operation: GithubUserOperationTypeAdd, State: GithubUserOperationStateFailed},
			},
			desiredMembers: []Member{{GithubUsername: "eve"}},
			wantChanged:    false,
			wantOpsLen:     1,
		},
		{
			name:            "current member not in desired — remove op queued",
			existingMembers: []Member{{GithubUsername: "frank"}},
			desiredMembers:  []Member{},
			wantChanged:     true,
			wantOpsLen:      1,
		},
		{
			name: "multiple desired: one failed, one new — only new gets add op",
			existingOps: []GithubUserOperation{
				{User: "grace", Operation: GithubUserOperationTypeAdd, State: GithubUserOperationStateFailed},
			},
			desiredMembers: []Member{
				{GithubUsername: "grace"},
				{GithubUsername: "henry"},
			},
			wantChanged:  true,
			wantOpsLen:   2, // grace's failed op + henry's new pending op
			wantAddUsers: []string{"henry"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			team := GithubTeam{}
			team.Status.Members = tt.existingMembers
			team.Status.Operations = tt.existingOps

			changed, newStatus := team.ChangeCalculator(tt.desiredMembers)

			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if len(newStatus.Operations) != tt.wantOpsLen {
				t.Errorf("ops len = %d, want %d; ops = %v", len(newStatus.Operations), tt.wantOpsLen, newStatus.Operations)
			}
			for _, wantUser := range tt.wantAddUsers {
				found := false
				for _, op := range newStatus.Operations {
					if op.User == wantUser && op.Operation == GithubUserOperationTypeAdd && op.State == GithubUserOperationStatePending {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected pending add op for user %q, not found in ops: %v", wantUser, newStatus.Operations)
				}
			}
		})
	}
}
