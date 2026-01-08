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
		{"negative_duration_always_expired_when_now_after_base", "-1s", base, base, true, false},
		// Above: since.Add(-1s) is before base; now=base is After(since-1s) => true. But time.After requires strictly after. base.After(base.Add(-1s)) is true.
		{"negative_duration_now_equal_to_cutoff", "-1s", base, base.Add(-1 * time.Second), false, false},
		{"invalid", "not-a-duration", base, base, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ttlExpired(tt.ttl, tt.since, tt.now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ttlExpired error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ttlExpired = %v, want %v (ttl=%s, since=%s, now=%s)", got, tt.want, tt.ttl, tt.since, tt.now)
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
		{Repo: "", State: v1.GithubRepoTeamOperationStateFailed}, // empty repo should be ignored
		{Repo: "repo2", State: v1.GithubRepoTeamOperationStateFailed},
	}

	got := uniquePendingOrFailedRepoNames(ops)

	// Convert to set for stable assertions
	gotSet := map[string]struct{}{}
	for _, r := range got {
		gotSet[r] = struct{}{}
	}

	if _, ok := gotSet[""]; ok {
		t.Fatalf("expected empty repo to be ignored, but found in result: %v", got)
	}
	if _, ok := gotSet["repo1"]; !ok {
		t.Fatalf("expected repo1 to be present, got %v", got)
	}
	if _, ok := gotSet["repo2"]; !ok {
		t.Fatalf("expected repo2 to be present due to failed op, got %v", got)
	}
	if _, ok := gotSet["repo3"]; ok {
		t.Fatalf("did not expect repo3 to be present (only skipped op), got %v", got)
	}

	// Ensure uniqueness
	count := 0
	for range gotSet {
		count++
	}
	if count != len(got) {
		t.Fatalf("expected unique list, but duplicates found: %v", got)
	}
}
