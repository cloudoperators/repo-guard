// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"regexp"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
)

var OperatorNamespace = "repo-guard-greenhouse-system"

type dummyAssert struct{}

func (t dummyAssert) Errorf(string, ...any) {}

func elementsMatch(listA, listB any) bool {
	return assert.ElementsMatch(dummyAssert{}, listA, listB)
}

// isEtagCacheInconsistency returns true when the error is a stale-etag-cache
// transient condition: the transport sent If-None-Match and received 304 but
// the local in-process cache no longer holds the corresponding parsed value.
// The provider layer already invalidates the cache entry before returning this
// error, so the very next reconcile will succeed with a fresh 200 response.
// Callers should requeue without updating the status rather than marking the
// resource as failed.
func isEtagCacheInconsistency(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "etag cache inconsistency")
}

// parseGitHubRateLimitReset tries to extract a retry-after time from a GitHub rate-limit error string.
//
//   - Future reset (absolute):  "API rate limit ... still exceeded until 2025-12-05 02:02:13 +0000 UTC, ..."
//     → returns the parsed future reset time so callers can requeue with RequeueAfter.
//
//   - Future reset (relative):  "... timestamp 2026-07-06 19:14:42 UTC. [rate reset in 8m51s]"
//     → parses base+duration as an absolute reset time, stable across re-parses of the stored error.
//
//   - Already reset: "... [rate limit was reset 1s ago]"
//     → returns time.Now() so callers requeue immediately.
//
//   - Invitation rate limit (no timestamp): "exceeded the organization invitation rate limit …"
//     → returns a synthetic backoff of now+1h (the API does not provide a reset time for this case).
//
//   - GraphQL secondary rate limit: "You have exceeded a secondary rate limit" / "API rate limit exceeded for installation ID"
//     → treated as a rate-limit error and returns now+1h as a conservative backoff.
//
// Returns the retry-after time in UTC and true if the error is a recognisable rate-limit error;
// otherwise returns zero time and false.
func parseGitHubRateLimitReset(errStr string) (time.Time, bool) {
	if errStr == "" {
		return time.Time{}, false
	}
	lowered := strings.ToLower(errStr)
	// Special-case: GitHub organization invitation rate limit errors don't include a reset timestamp.
	// Example: "You have exceeded the organization invitation rate limit of 500 per 24 hours."
	// In such cases, return a conservative backoff window so callers can requeue as ratelimited.
	if strings.Contains(lowered, "organization invitation rate limit") ||
		strings.Contains(lowered, "invitation rate limit") ||
		strings.Contains(lowered, "exceeded the organization invitation rate limit") {
		return time.Now().UTC().Add(time.Hour), true
	}
	if !strings.Contains(lowered, "rate limit") {
		return time.Time{}, false
	}
	// Format 2: "[rate limit was reset N ago]" — the limit has already cleared.
	// Return now so callers requeue immediately rather than skipping the resource.
	if strings.Contains(lowered, "was reset") && strings.Contains(lowered, "ago") {
		return time.Now().UTC(), true
	}
	// Format 3: "[rate reset in 8m51s]" — relative duration until reset.
	// This format is emitted by the GitHub Enterprise go-github client and stored verbatim in
	// status. The error also contains an absolute base timestamp ("timestamp 2026-07-06 19:14:42 UTC"),
	// so compute the reset time as base+duration. This is stable when the stored error string is
	// re-parsed on subsequent reconciles — unlike now+duration, which re-arms the backoff on every
	// reconcile and keeps resources stuck in RateLimited indefinitely.
	if m := regexp.MustCompile(`\[rate reset in ([^\]]+)\]`).FindStringSubmatch(lowered); len(m) == 2 {
		d, derr := time.ParseDuration(m[1])
		if derr != nil || d <= 0 {
			// Duration absent or already elapsed — requeue immediately.
			return time.Now().UTC(), true
		}
		// Try to extract the absolute base timestamp ("timestamp <ts> UTC") and anchor to it.
		// Example: "... timestamp 2026-07-06 19:14:42 UTC. [rate reset in 8m51s]"
		if bm := regexp.MustCompile(`timestamp\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC)`).FindStringSubmatch(errStr); len(bm) == 2 {
			if base, perr := time.Parse("2006-01-02 15:04:05 MST", bm[1]); perr == nil {
				return base.UTC().Add(d), true
			}
		}
		// No base timestamp available — fall back to now+duration (best effort).
		return time.Now().UTC().Add(d), true
	}
	if !strings.Contains(lowered, "until ") {
		// GraphQL secondary rate limit strings (no timestamp): treat as rate-limited with 1h backoff.
		if strings.Contains(lowered, "secondary rate limit") ||
			strings.Contains(lowered, "api rate limit exceeded for installation") {
			return time.Now().UTC().Add(time.Hour), true
		}
		return time.Time{}, false
	}
	// Format 1: extract the future reset timestamp after "until ".
	// Example captured: 2025-12-05 02:02:13 +0000 UTC
	// Use case-insensitive flag so the regex matches the original errStr consistently
	// with the lowercased guard above (avoiding a mismatch if GitHub ever varies casing).
	re := regexp.MustCompile(`(?i)until\s+([^,\]]+)`)
	m := re.FindStringSubmatch(errStr)
	if len(m) < 2 {
		return time.Time{}, false
	}
	ts := strings.TrimSpace(m[1])
	layouts := []string{
		"2006-01-02 15:04:05 -0700 MST",
		time.RFC3339,
		"2006-01-02 15:04:05 MST",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
