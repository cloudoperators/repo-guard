// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
	ghmetrics "github.com/cloudoperators/repo-guard/internal/metrics"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	statusPayloadWarnBytes   = 1_000_000 // 1 MB — emit metric + log warning
	statusPayloadSafetyBytes = 2_500_000 // 2.5 MB — apply adaptive TTL shrinking
	statusPayloadMinTTL      = 5 * time.Minute
)

// statusPayloadBytes returns the JSON byte count of the status subresource.
func statusPayloadBytes(status v1.GithubOrganizationStatus) (int, error) {
	b, err := json.Marshal(status)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// adaptiveTTLShrink applies halving of completedTTL in-memory until the payload
// fits under statusPayloadSafetyBytes or the TTL floor (statusPayloadMinTTL) is reached.
// Returns (shrunkStatus, halvingCount, effectiveTTL, opsPruned, fits).
// opsPruned is the total number of completed operations removed across all scopes.
func adaptiveTTLShrink(
	status v1.GithubOrganizationStatus,
	completedTTLStr string,
	now time.Time,
) (v1.GithubOrganizationStatus, int, time.Duration, int, bool) {
	ttl, err := time.ParseDuration(completedTTLStr)
	if err != nil {
		// Cannot shrink without a valid TTL; return original status unchanged.
		return status, 0, 0, 0, false
	}

	halvings := 0
	current := *status.DeepCopy()
	originalCount := countAllCompletedOps(status)

	for {
		size, err := statusPayloadBytes(current)
		if err != nil {
			return status, halvings, ttl, 0, false
		}
		if size <= statusPayloadSafetyBytes {
			pruned := originalCount - countAllCompletedOps(current)
			return current, halvings, ttl, pruned, true
		}
		if ttl <= statusPayloadMinTTL {
			pruned := originalCount - countAllCompletedOps(current)
			return current, halvings, ttl, pruned, false
		}
		ttl /= 2
		halvings++
		// Apply the tightened TTL to all completed-op buckets.
		if updated, changed := applyRepoOpsTTL(current.Operations.RepositoryTeamOperations, ttl, v1.GithubRepoTeamOperationStateComplete, now); changed {
			current.Operations.RepositoryTeamOperations = updated
		}
		if updated, changed := applyUserOpsTTL(current.Operations.OrganizationOwnerOperations, ttl, v1.GithubUserOperationStateComplete, now); changed {
			current.Operations.OrganizationOwnerOperations = updated
		}
		if updated, changed := applyUserOpsTTL(current.Operations.OrganizationMemberOperations, ttl, v1.GithubUserOperationStateComplete, now); changed {
			current.Operations.OrganizationMemberOperations = updated
		}
		if updated, changed := applyTeamOpsTTL(current.Operations.GithubTeamOperations, ttl, v1.GithubUserOperationStateComplete, now); changed {
			current.Operations.GithubTeamOperations = updated
		}
		if updated, changed := applyRepoUserOpsTTL(current.Operations.RepositoryCollaboratorOperations, ttl, v1.GithubRepoUserOperationStateComplete, now); changed {
			current.Operations.RepositoryCollaboratorOperations = updated
		}
	}
}

// formatTruncationAnnotation formats the human-readable value for the
// GITHUB_ORG_ANNOTATION_STATUS_PAYLOAD_TRUNCATED annotation.
func formatTruncationAnnotation(now time.Time, halvings int, originalTTL string, effectiveTTL time.Duration, originalBytes int, opsPruned int) string {
	return fmt.Sprintf(
		"%s: completedTTL halved %d times (%s→%s) to fit %.1fMB payload; %d ops pruned",
		now.UTC().Format(time.RFC3339),
		halvings,
		originalTTL,
		effectiveTTL,
		float64(originalBytes)/1e6,
		opsPruned,
	)
}

// countAllCompletedOps returns the total number of completed operations across all scopes.
func countAllCompletedOps(s v1.GithubOrganizationStatus) int {
	count := 0
	for _, op := range s.Operations.RepositoryTeamOperations {
		if op.State == v1.GithubRepoTeamOperationStateComplete {
			count++
		}
	}
	for _, op := range s.Operations.OrganizationOwnerOperations {
		if op.State == v1.GithubUserOperationStateComplete {
			count++
		}
	}
	for _, op := range s.Operations.OrganizationMemberOperations {
		if op.State == v1.GithubUserOperationStateComplete {
			count++
		}
	}
	for _, op := range s.Operations.GithubTeamOperations {
		if op.State == v1.GithubUserOperationStateComplete {
			count++
		}
	}
	for _, op := range s.Operations.RepositoryCollaboratorOperations {
		if op.State == v1.GithubRepoUserOperationStateComplete {
			count++
		}
	}
	return count
}

// safeStatusUpdate marshals the status, updates the payload-size metric, applies
// adaptive TTL shrinking if the payload is too large, writes the truncation
// annotation if shrinking occurred, and finally calls r.Client.Status().Update
// wrapped in a RetryOnConflict loop.
func (r *GithubOrganizationReconciler) safeStatusUpdate(
	ctx context.Context,
	req ctrl.Request,
	status *v1.GithubOrganizationStatus,
	org *v1.GithubOrganization,
	githubName string,
) error {
	l := log.FromContext(ctx)
	githubLabel := strings.TrimSpace(githubName)
	orgLabel := strings.TrimSpace(org.Spec.Organization)

	originalBytes, err := statusPayloadBytes(*status)
	if err != nil {
		l.Error(err, "safeStatusUpdate: failed to marshal status for size check")
		// Proceed with the write anyway — better to attempt than to silently drop.
	} else {
		ghmetrics.OrgStatusPayloadBytes.WithLabelValues(githubLabel, orgLabel).Set(float64(originalBytes))

		if originalBytes > statusPayloadWarnBytes {
			l.Info("status payload exceeds warn threshold",
				"bytes", originalBytes,
				"warnThreshold", statusPayloadWarnBytes,
			)
		}

		if originalBytes > statusPayloadSafetyBytes {
			now := time.Now()
			completedTTLStr := ""
			if org.Labels != nil {
				completedTTLStr = org.Labels[GITHUB_ORG_LABEL_COMPLETED_TTL]
			}

			originalTTL := completedTTLStr
			shrunk, halvings, effectiveTTL, opsPruned, fits := adaptiveTTLShrink(*status, completedTTLStr, now)
			if halvings > 0 {
				*status = shrunk

				// Update the metric to reflect the actual bytes that will be written.
				if shrunkBytes, merr := statusPayloadBytes(*status); merr == nil {
					ghmetrics.OrgStatusPayloadBytes.WithLabelValues(githubLabel, orgLabel).Set(float64(shrunkBytes))
				}

				annotationValue := formatTruncationAnnotation(now, halvings, originalTTL, effectiveTTL, originalBytes, opsPruned)
				// Write the annotation to the metadata (not the status subresource)
				// so the record persists even if the status write fails.
				// Non-fatal if it fails.
				_ = retry.RetryOnConflict(retry.DefaultRetry, func() error {
					live := &v1.GithubOrganization{}
					if err := r.Get(ctx, req.NamespacedName, live); err != nil {
						return err
					}
					if live.Annotations == nil {
						live.Annotations = map[string]string{}
					}
					live.Annotations[GITHUB_ORG_ANNOTATION_STATUS_PAYLOAD_TRUNCATED] = annotationValue
					return r.Update(ctx, live)
				})
			}

			if !fits {
				afterBytes, _ := statusPayloadBytes(*status)
				l.Info("status payload still exceeds safety threshold after adaptive TTL shrinking; skipping write to avoid etcd 422",
					"originalBytes", originalBytes,
					"bytesAfterShrink", afterBytes,
					"safetyThreshold", statusPayloadSafetyBytes,
					"halvings", halvings,
				)
				return nil
			}
		}
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1.GithubOrganization{}
		if err := r.Get(ctx, req.NamespacedName, latest); err != nil {
			return err
		}
		latest.Status = *status
		return r.Client.Status().Update(ctx, latest)
	})
}
