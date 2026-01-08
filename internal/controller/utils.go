// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"regexp"
	"strings"
	"time"

	"github.com/stretchr/testify/assert"
)

type dummyAssert struct{}

func (t dummyAssert) Errorf(string, ...interface{}) {}

func elementsMatch(listA, listB interface{}) bool {
	return assert.ElementsMatch(dummyAssert{}, listA, listB)
}

// parseGitHubRateLimitReset tries to extract a reset timestamp from a GitHub rate-limit error string.
// Expected substrings look like:
// "API rate limit ... still exceeded until 2025-12-05 02:02:13 +0000 UTC, ..."
// Returns the parsed time in UTC and true if found; otherwise zero time and false.
func parseGitHubRateLimitReset(errStr string) (time.Time, bool) {
	if errStr == "" {
		return time.Time{}, false
	}
	// quick guard
	lowered := strings.ToLower(errStr)
	// Special-case: GitHub organization invitation rate limit errors don't include a reset timestamp.
	// Example: "You have exceeded the organization invitation rate limit of 500 per 24 hours."
	// In such cases, return a conservative backoff window so callers can requeue as ratelimited.
	if strings.Contains(lowered, "organization invitation rate limit") ||
		strings.Contains(lowered, "invitation rate limit") ||
		strings.Contains(lowered, "exceeded the organization invitation rate limit") {
		return time.Now().UTC().Add(time.Hour), true
	}
	if !strings.Contains(lowered, "rate limit") || !strings.Contains(lowered, "until ") {
		return time.Time{}, false
	}
	// Regex to capture the timestamp after "until " up to the next comma
	// Example captured: 2025-12-05 02:02:13 +0000 UTC
	re := regexp.MustCompile(`until\s+([^,\]]+)`)
	m := re.FindStringSubmatch(errStr)
	if len(m) < 2 {
		return time.Time{}, false
	}
	ts := strings.TrimSpace(m[1])
	// try common layout observed in error message
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
