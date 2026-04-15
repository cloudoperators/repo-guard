// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"
	"time"

	v1 "github.com/cloudoperators/repo-guard/api/v1"
)

func TestTTLExpired(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		ttl     string
		since   time.Time
		now     time.Time
		want    bool
		wantErr bool
	}{
		{"valid_not_expired", "1h", base, base.Add(30 * time.Minute), false, false},
		{"valid_expired", "30m", base, base.Add(31 * time.Minute), true, false},
		{"zero_duration_not_after", "0s", base, base, false, false},
		{"zero_duration_after", "0s", base, base.Add(time.Nanosecond), true, false},
		{"negative_duration_now_equal_cutoff", "-1s", base, base.Add(-1 * time.Second), false, false},
		{"negative_duration_now_after_cutoff", "-1s", base, base, true, false},
		{"invalid", "not-a-duration", base, base, false, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := ttlExpired(tt.ttl, tt.since, tt.now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ttlExpired error=%v wantErr=%v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ttlExpired=%v want=%v ttl=%s", got, tt.want, tt.ttl)
			}
		})
	}
}

func TestUniquePendingOrFailedRepoNames(t *testing.T) {
	ops := []v1.GithubRepoTeamOperation{
		{Repo: "repo1", State: v1.GithubRepoTeamOperationStatePending},
		{Repo: "repo1", State: v1.GithubRepoTeamOperationStateFailed},
		{Repo: "repo2", State: v1.GithubRepoTeamOperationStateComplete},
		{Repo: "repo3", State: v1.GithubRepoTeamOperationStateSkipped},
		{Repo: "", State: v1.GithubRepoTeamOperationStateFailed},
		{Repo: "repo2", State: v1.GithubRepoTeamOperationStateFailed},
	}

	got := uniquePendingOrFailedRepoNames(ops)

	set := map[string]struct{}{}
	for _, r := range got {
		set[r] = struct{}{}
	}

	if _, ok := set[""]; ok {
		t.Fatalf("empty repo should be ignored")
	}
	if _, ok := set["repo1"]; !ok {
		t.Fatalf("repo1 missing")
	}
	if _, ok := set["repo2"]; !ok {
		t.Fatalf("repo2 missing")
	}
	if _, ok := set["repo3"]; ok {
		t.Fatalf("repo3 should not be included")
	}
}
