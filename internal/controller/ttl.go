// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
)

// applyUserOpsTTL drops GithubUserOperations whose State equals state and
// whose own Timestamp is older than ttl. Operations with a zero Timestamp are
// preserved. Returns (newOps, changed) where changed is true iff any op was
// dropped. The caller is responsible for parsing and validating ttl.
func applyUserOpsTTL(
	ops []v1.GithubUserOperation,
	ttl time.Duration,
	state v1.GithubUserOperationState,
	now time.Time,
) ([]v1.GithubUserOperation, bool) {
	var out []v1.GithubUserOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Add(ttl)) {
			if out == nil {
				out = make([]v1.GithubUserOperation, i, len(ops))
				copy(out, ops[:i])
			}
			continue
		}
		if out != nil {
			out = append(out, op)
		}
	}
	if out == nil {
		return ops, false
	}
	return out, true
}

// applyRepoOpsTTL drops GithubRepoTeamOperations whose State equals state and
// whose own Timestamp is older than ttl. See applyUserOpsTTL for semantics.
func applyRepoOpsTTL(
	ops []v1.GithubRepoTeamOperation,
	ttl time.Duration,
	state v1.GithubRepoTeamOperationState,
	now time.Time,
) ([]v1.GithubRepoTeamOperation, bool) {
	var out []v1.GithubRepoTeamOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Add(ttl)) {
			if out == nil {
				out = make([]v1.GithubRepoTeamOperation, i, len(ops))
				copy(out, ops[:i])
			}
			continue
		}
		if out != nil {
			out = append(out, op)
		}
	}
	if out == nil {
		return ops, false
	}
	return out, true
}

// applyTeamOpsTTL drops GithubTeamOperations whose State equals state and
// whose own Timestamp is older than ttl. See applyUserOpsTTL for semantics.
func applyTeamOpsTTL(
	ops []v1.GithubTeamOperation,
	ttl time.Duration,
	state v1.GithubUserOperationState,
	now time.Time,
) ([]v1.GithubTeamOperation, bool) {
	var out []v1.GithubTeamOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Add(ttl)) {
			if out == nil {
				out = make([]v1.GithubTeamOperation, i, len(ops))
				copy(out, ops[:i])
			}
			continue
		}
		if out != nil {
			out = append(out, op)
		}
	}
	if out == nil {
		return ops, false
	}
	return out, true
}
