// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"errors"
	"testing"
	"time"
)

func TestIsEtagCacheInconsistency(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "exact etag cache inconsistency prefix matches",
			err:  errors.New("etag cache inconsistency for /orgs/foo/teams/bar/members?per_page=100: 304 received but no valid cached value"),
			want: true,
		},
		{
			name: "error containing etag cache inconsistency substring matches",
			err:  errors.New("wrapped: etag cache inconsistency for /orgs/foo/members: 304 received but no valid cached value"),
			want: true,
		},
		{
			name: "unrelated error returns false",
			err:  errors.New("404 Not Found"),
			want: false,
		},
		{
			name: "rate limit error does not match",
			err:  errors.New("API rate limit exceeded for installation ID 123"),
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isEtagCacheInconsistency(tc.err)
			if got != tc.want {
				t.Fatalf("isEtagCacheInconsistency(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestParseGitHubRateLimitReset(t *testing.T) {
	t.Run("future reset timestamp via 'until' format", func(t *testing.T) {
		future := time.Now().UTC().Add(5 * time.Minute)
		errStr := "GET https://api.github.com/orgs/foo/members: 403 API rate limit exceeded for installation ID 123. still exceeded until " + future.Format("2006-01-02 15:04:05 +0000 UTC") + ", reset in 5m"
		got, ok := parseGitHubRateLimitReset(errStr)
		if !ok {
			t.Fatal("expected ok=true for 'until' format, got false")
		}
		diff := got.Sub(future)
		if diff < -2*time.Second || diff > 2*time.Second {
			t.Fatalf("expected parsed time ~%v, got %v (diff %v)", future, got, diff)
		}
	})

	t.Run("already-reset format returns now", func(t *testing.T) {
		// This is the format that caused the bug: rate limit already cleared.
		errStr := "GET https://github.wdf.sap.corp/api/v3/orgs/cc/members?per_page=100&role=admin: 403 API rate limit exceeded for installation ID 5668. If you reach out to GitHub Support for help, please include the request ID abc123 and timestamp 2026-06-06 23:08:46 UTC. [rate limit was reset 1s ago]"
		before := time.Now().UTC()
		got, ok := parseGitHubRateLimitReset(errStr)
		after := time.Now().UTC()
		if !ok {
			t.Fatal("expected ok=true for 'was reset N ago' format, got false")
		}
		// The returned time must be approximately now and must not be in the future.
		// Callers use t.After(now) to decide between RequeueAfter and Requeue: true;
		// for the already-reset case t.After(now) must be false so we get Requeue: true.
		if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
			t.Fatalf("expected returned time to be approximately now (%v..%v), got %v", before, after, got)
		}
	})

	t.Run("invitation rate limit returns conservative future backoff", func(t *testing.T) {
		errStr := "You have exceeded the organization invitation rate limit of 500 per 24 hours."
		before := time.Now().UTC()
		got, ok := parseGitHubRateLimitReset(errStr)
		if !ok {
			t.Fatal("expected ok=true for invitation rate limit, got false")
		}
		if !got.After(before) {
			t.Fatalf("expected future backoff time, got %v (before=%v)", got, before)
		}
	})

	t.Run("non-rate-limit error returns false", func(t *testing.T) {
		_, ok := parseGitHubRateLimitReset("404 Not Found")
		if ok {
			t.Fatal("expected ok=false for non-rate-limit error")
		}
	})

	t.Run("empty string returns false", func(t *testing.T) {
		_, ok := parseGitHubRateLimitReset("")
		if ok {
			t.Fatal("expected ok=false for empty string")
		}
	})

	t.Run("graphql secondary rate limit returns conservative backoff", func(t *testing.T) {
		errStr := "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."
		before := time.Now().UTC()
		got, ok := parseGitHubRateLimitReset(errStr)
		if !ok {
			t.Fatal("expected ok=true for GraphQL secondary rate limit, got false")
		}
		if !got.After(before) {
			t.Fatalf("expected future backoff time, got %v (before=%v)", got, before)
		}
	})

	t.Run("graphql installation rate limit without timestamp returns backoff", func(t *testing.T) {
		// GraphQL errors: no "until" timestamp, just a generic installation rate limit.
		errStr := "API rate limit exceeded for installation ID 99999."
		before := time.Now().UTC()
		got, ok := parseGitHubRateLimitReset(errStr)
		if !ok {
			t.Fatal("expected ok=true for GraphQL installation rate limit, got false")
		}
		if !got.After(before) {
			t.Fatalf("expected future backoff time, got %v (before=%v)", got, before)
		}
	})

	t.Run("relative 'rate reset in' format returns absolute base+duration", func(t *testing.T) {
		// The GHE error contains an absolute base timestamp; the parser must return base+duration
		// so re-parsing the same stored error string gives a stable (non-advancing) reset time.
		errStr := "GET https://github.tools.sap/api/v3/orgs/cloudoperators/members?per_page=100&role=admin: 403 API rate limit exceeded for installation ID 13780. If you reach out to GitHub Support for help, please include the request ID b3f33437-996f-4cee-888e-580c1f7cc593 and timestamp 2026-07-06 19:14:42 UTC. [rate reset in 8m51s]"
		got, ok := parseGitHubRateLimitReset(errStr)
		if !ok {
			t.Fatal("expected ok=true for 'rate reset in' format, got false")
		}
		// Expected: 2026-07-06 19:14:42 UTC + 8m51s = 2026-07-06 19:23:33 UTC
		expected, err := time.Parse("2006-01-02 15:04:05 MST", "2026-07-06 19:23:33 UTC")
		if err != nil {
			t.Fatalf("failed to parse expected time: %v", err)
		}
		if got.Before(expected.Add(-time.Second)) || got.After(expected.Add(time.Second)) {
			t.Fatalf("expected absolute reset ~%v (base+8m51s), got %v", expected, got)
		}
	})

	t.Run("relative 'rate reset in' is stable across re-parses", func(t *testing.T) {
		// Simulate the controller re-parsing the same stored error string on a later reconcile.
		// The returned time must not advance between calls.
		errStr := "GET https://github.tools.sap/api/v3/orgs/cloudoperators/members?per_page=100&role=admin: 403 API rate limit exceeded for installation ID 13780. If you reach out to GitHub Support for help, please include the request ID b3f33437-996f-4cee-888e-580c1f7cc593 and timestamp 2026-07-06 19:14:42 UTC. [rate reset in 8m51s]"
		got1, ok1 := parseGitHubRateLimitReset(errStr)
		got2, ok2 := parseGitHubRateLimitReset(errStr)
		if !ok1 || !ok2 {
			t.Fatal("expected ok=true for both calls")
		}
		if !got1.Equal(got2) {
			t.Fatalf("re-parsing the same stored error returned different times: %v vs %v", got1, got2)
		}
	})

	t.Run("relative 'rate reset in' stored error recovers after duration elapsed", func(t *testing.T) {
		// Simulate the error being re-read from status after the window has passed:
		// the stored string says "0s" (already expired). Expect immediate requeue (now).
		errStr := "GET https://github.tools.sap/api/v3/orgs/cc/members?per_page=100&role=admin: 403 API rate limit exceeded for installation ID 5668. [rate reset in 0s]"
		before := time.Now().UTC()
		got, ok := parseGitHubRateLimitReset(errStr)
		after := time.Now().UTC()
		if !ok {
			t.Fatal("expected ok=true, got false")
		}
		// d==0 is not > 0, so falls to the "requeue immediately" path — result must be ~now.
		if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
			t.Fatalf("expected ~now for zero duration, got %v", got)
		}
	})
}
