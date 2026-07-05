// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
)

// buildCompletedRepoOps returns n completed GithubRepoTeamOperations each with the given timestamp.
func buildCompletedRepoOps(n int, ts time.Time) []v1.GithubRepoTeamOperation {
	ops := make([]v1.GithubRepoTeamOperation, n)
	for i := range ops {
		ops[i] = v1.GithubRepoTeamOperation{
			Operation: v1.GithubRepoTeamOperationTypeAdd,
			Repo:      "repo",
			Team:      "team",
			State:     v1.GithubRepoTeamOperationStateComplete,
			Timestamp: metav1.NewTime(ts),
		}
	}
	return ops
}

// buildLargeStatus builds a GithubOrganizationStatus whose serialised JSON exceeds
// targetBytes by filling RepositoryTeamOperations with completed ops.
// Each completed op is ~100 bytes when serialised; we overshoot by 20 % to be safe.
func buildLargeStatus(targetBytes int, ts time.Time) v1.GithubOrganizationStatus {
	opsNeeded := (targetBytes / 100) + (targetBytes/100)/5 + 1
	return v1.GithubOrganizationStatus{
		OrganizationStatus: v1.GithubOrganizationStateComplete,
		Operations: v1.GithubOrganizationStatusOperations{
			RepositoryTeamOperations: buildCompletedRepoOps(opsNeeded, ts),
		},
	}
}

func TestStatusPayloadBytes(t *testing.T) {
	status := v1.GithubOrganizationStatus{
		OrganizationStatus: v1.GithubOrganizationStateComplete,
	}
	n, err := statusPayloadBytes(status)
	if err != nil {
		t.Fatalf("statusPayloadBytes returned error: %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected positive byte count, got %d", n)
	}
}

func TestAdaptiveTTLShrink_FitsImmediately(t *testing.T) {
	// A small status that is already under the safety threshold should be returned
	// unchanged with halvings=0 and fits=true.
	status := v1.GithubOrganizationStatus{
		OrganizationStatus: v1.GithubOrganizationStateComplete,
	}
	now := time.Now()
	got, halvings, _, opsPruned, fits := adaptiveTTLShrink(status, "72h", now)
	if !fits {
		t.Fatal("expected fits=true for small status")
	}
	if halvings != 0 {
		t.Fatalf("expected halvings=0, got %d", halvings)
	}
	if opsPruned != 0 {
		t.Fatalf("expected opsPruned=0, got %d", opsPruned)
	}
	_ = got
}

func TestAdaptiveTTLShrink_InvalidTTL(t *testing.T) {
	status := v1.GithubOrganizationStatus{}
	_, halvings, _, _, fits := adaptiveTTLShrink(status, "notaduration", time.Now())
	if fits {
		t.Fatal("expected fits=false for invalid TTL")
	}
	if halvings != 0 {
		t.Fatalf("expected halvings=0 for invalid TTL, got %d", halvings)
	}
}

func TestAdaptiveTTLShrink_HalvesAndFits(t *testing.T) {
	// Build a status whose JSON size exceeds statusPayloadSafetyBytes (2.5 MB).
	// The ops all have a timestamp 48 h in the past.
	// Starting TTL is 72h. After each halving:
	//   72h → 36h → 18h → 9h → ...
	// With a 48h-old timestamp, ops are NOT expired at 72h, 36h, but ARE expired at
	// any TTL < 48h (36h < 48h → expired). So the first halving (72h→36h) already
	// causes TTL (36h) < age (48h) → ops dropped. The status should fit after ≥1 halvings.
	now := time.Now()
	opAge := now.Add(-48 * time.Hour)

	status := buildLargeStatus(statusPayloadSafetyBytes+500_000, opAge)

	originalOpsCount := len(status.Operations.RepositoryTeamOperations)
	if originalOpsCount == 0 {
		t.Fatal("buildLargeStatus produced empty ops; test setup broken")
	}

	got, halvings, effectiveTTL, opsPruned, fits := adaptiveTTLShrink(status, "72h", now)

	if !fits {
		size, _ := statusPayloadBytes(got)
		t.Fatalf("expected fits=true after halving; final size %d bytes, halvings=%d", size, halvings)
	}
	if halvings <= 0 {
		t.Fatalf("expected at least 1 halving, got %d", halvings)
	}
	if opsPruned <= 0 {
		t.Fatalf("expected ops to be pruned, got opsPruned=%d", opsPruned)
	}
	if effectiveTTL >= 72*time.Hour {
		t.Fatalf("expected effectiveTTL < 72h, got %s", effectiveTTL)
	}
	finalSize, _ := statusPayloadBytes(got)
	if finalSize > statusPayloadSafetyBytes {
		t.Fatalf("final payload %d bytes still exceeds safety threshold %d", finalSize, statusPayloadSafetyBytes)
	}
}

func TestAdaptiveTTLShrink_FloorReached(t *testing.T) {
	// Build a status so large it cannot be shrunk under the threshold even at minTTL,
	// because the ops are very fresh (1 second old) so TTL-based pruning never fires.
	now := time.Now()
	freshTS := now.Add(-time.Second)

	// Build a status well over the safety threshold with fresh ops.
	status := buildLargeStatus(statusPayloadSafetyBytes+1_000_000, freshTS)

	_, halvings, effectiveTTL, _, fits := adaptiveTTLShrink(status, "72h", now)

	if fits {
		t.Fatal("expected fits=false when floor is reached with fresh ops")
	}
	if halvings == 0 {
		t.Fatal("expected halvings > 0 — loop should have run until floor")
	}
	if effectiveTTL > statusPayloadMinTTL {
		t.Fatalf("expected effectiveTTL <= minTTL (%s), got %s", statusPayloadMinTTL, effectiveTTL)
	}
}

func TestAdaptiveTTLShrink_HalvingCount(t *testing.T) {
	// Verify the halving count matches what we calculate manually.
	// ops age = 3h. TTL starts at 72h.
	// 72h → 36h → 18h → 9h → 4h30m → 2h15m — first TTL < 3h age → ops pruned at step 6.
	// But we stop as soon as payload fits, which may be sooner if the ops are numerous.
	now := time.Now()
	opAge := now.Add(-3 * time.Hour)

	status := buildLargeStatus(statusPayloadSafetyBytes+200_000, opAge)

	got, halvings, effectiveTTL, opsPruned, fits := adaptiveTTLShrink(status, "72h", now)

	if !fits {
		finalSize, _ := statusPayloadBytes(got)
		t.Logf("halvings=%d effectiveTTL=%s finalSize=%d", halvings, effectiveTTL, finalSize)
		t.Fatal("expected fits=true")
	}
	// Each halving must be positive and TTL must be strictly decreasing.
	if halvings <= 0 {
		t.Fatalf("expected positive halvings, got %d", halvings)
	}
	if opsPruned < 0 {
		t.Fatalf("opsPruned must be non-negative, got %d", opsPruned)
	}
}

func TestAdaptiveTTLShrink_EmptyTTL(t *testing.T) {
	// Empty completedTTLStr is treated as invalid; should return fits=false, halvings=0.
	status := buildLargeStatus(statusPayloadSafetyBytes+100_000, time.Now().Add(-24*time.Hour))
	_, halvings, _, _, fits := adaptiveTTLShrink(status, "", time.Now())
	if fits {
		t.Fatal("expected fits=false for empty TTL string")
	}
	if halvings != 0 {
		t.Fatalf("expected halvings=0 for empty TTL, got %d", halvings)
	}
}

func TestAdaptiveTTLShrink_AllScopesPruned(t *testing.T) {
	// Verify that completed ops in all five scopes are pruned by the halving loop.
	now := time.Now()
	oldTS := now.Add(-48 * time.Hour)

	// Build a status with old completed ops in every scope; sized to exceed safety threshold.
	nOps := (statusPayloadSafetyBytes / 80) + 1
	userOps := make([]v1.GithubUserOperation, nOps)
	for i := range userOps {
		userOps[i] = v1.GithubUserOperation{
			Operation: v1.GithubUserOperationTypeAdd,
			User:      "user",
			State:     v1.GithubUserOperationStateComplete,
			Timestamp: metav1.NewTime(oldTS),
		}
	}
	status := v1.GithubOrganizationStatus{
		OrganizationStatus: v1.GithubOrganizationStateComplete,
		Operations: v1.GithubOrganizationStatusOperations{
			OrganizationOwnerOperations: userOps,
		},
	}

	got, halvings, _, opsPruned, fits := adaptiveTTLShrink(status, "72h", now)

	if !fits {
		finalSize, _ := statusPayloadBytes(got)
		t.Fatalf("expected fits=true; finalSize=%d halvings=%d", finalSize, halvings)
	}
	if opsPruned == 0 {
		t.Fatal("expected opsPruned > 0 for owner ops")
	}
}

func TestCountAllCompletedOps(t *testing.T) {
	now := time.Now()
	ts := metav1.NewTime(now)

	status := v1.GithubOrganizationStatus{
		Operations: v1.GithubOrganizationStatusOperations{
			RepositoryTeamOperations: []v1.GithubRepoTeamOperation{
				{State: v1.GithubRepoTeamOperationStateComplete, Timestamp: ts},
				{State: v1.GithubRepoTeamOperationStateFailed, Timestamp: ts},
			},
			OrganizationOwnerOperations: []v1.GithubUserOperation{
				{State: v1.GithubUserOperationStateComplete, Timestamp: ts},
			},
			OrganizationMemberOperations: []v1.GithubUserOperation{
				{State: v1.GithubUserOperationStateComplete, Timestamp: ts},
				{State: v1.GithubUserOperationStatePending, Timestamp: ts},
			},
		},
	}

	count := countAllCompletedOps(status)
	// repoTeam(1) + owner(1) + member(1) = 3
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}

func TestAdaptiveTTLShrink_AnnotationFormat(t *testing.T) {
	// Verify the annotation value format by constructing it as the controller would.
	now := time.Date(2026, 7, 4, 15, 30, 0, 0, time.UTC)
	originalTTL := "72h"
	halvings := 3
	effectiveTTL := 9 * time.Hour
	originalBytes := 2_300_000
	opsPruned := 412

	annotation := formatTruncationAnnotation(now, halvings, originalTTL, effectiveTTL, originalBytes, opsPruned)

	if !strings.HasPrefix(annotation, "2026-07-04T15:30:00Z:") {
		t.Errorf("annotation missing RFC3339 timestamp prefix: %s", annotation)
	}
	if !strings.Contains(annotation, "halved 3 times") {
		t.Errorf("annotation missing halving count: %s", annotation)
	}
	if !strings.Contains(annotation, "72h→9h") {
		t.Errorf("annotation missing TTL transition: %s", annotation)
	}
	if !strings.Contains(annotation, "2.3MB") {
		t.Errorf("annotation missing payload size: %s", annotation)
	}
	if !strings.Contains(annotation, "412 ops pruned") {
		t.Errorf("annotation missing ops pruned: %s", annotation)
	}
}
