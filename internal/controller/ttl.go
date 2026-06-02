// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	"github.com/go-logr/logr"
)

// applyUserOpsTTL filters out GithubUserOperations whose State equals the given
// state and whose own Timestamp is older than ttlStr. Operations with a zero
// Timestamp are preserved. If ttlStr is empty, the input is returned unchanged.
// On invalid TTL string, a log entry is emitted and the input is returned unchanged.
// Returns (newOps, changed) where changed is true iff any op was dropped.
func applyUserOpsTTL(
	l logr.Logger,
	ops []v1.GithubUserOperation,
	ttlStr string,
	state v1.GithubUserOperationState,
	label string,
	now time.Time,
) ([]v1.GithubUserOperation, bool) {
	if ttlStr == "" {
		return ops, false
	}
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		l.Info("invalid TTL duration label; skipping cleanup", "label", label, "value", ttlStr, "error", err)
		return ops, false
	}
	var out []v1.GithubUserOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Time.Add(ttl)) {
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

// applyRepoOpsTTL filters out GithubRepoTeamOperations whose State equals the
// given state and whose own Timestamp is older than ttlStr. See applyUserOpsTTL
// for semantics.
func applyRepoOpsTTL(
	l logr.Logger,
	ops []v1.GithubRepoTeamOperation,
	ttlStr string,
	state v1.GithubRepoTeamOperationState,
	label string,
	now time.Time,
) ([]v1.GithubRepoTeamOperation, bool) {
	if ttlStr == "" {
		return ops, false
	}
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		l.Info("invalid TTL duration label; skipping cleanup", "label", label, "value", ttlStr, "error", err)
		return ops, false
	}
	var out []v1.GithubRepoTeamOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Time.Add(ttl)) {
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

// applyTeamOpsTTL filters out GithubTeamOperations whose State equals the given
// state and whose own Timestamp is older than ttlStr. See applyUserOpsTTL for
// semantics.
func applyTeamOpsTTL(
	l logr.Logger,
	ops []v1.GithubTeamOperation,
	ttlStr string,
	state v1.GithubUserOperationState,
	label string,
	now time.Time,
) ([]v1.GithubTeamOperation, bool) {
	if ttlStr == "" {
		return ops, false
	}
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		l.Info("invalid TTL duration label; skipping cleanup", "label", label, "value", ttlStr, "error", err)
		return ops, false
	}
	var out []v1.GithubTeamOperation
	for i, op := range ops {
		if op.State == state && !op.Timestamp.IsZero() && now.After(op.Timestamp.Time.Add(ttl)) {
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
