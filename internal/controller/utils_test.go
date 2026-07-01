// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"
	"time"
)

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
}
