// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	githubAPI "github.com/google/go-github/v84/github"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
)

// ---- env/string helpers ----

func nonEmpty(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func generateUniqueName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "test"
	}
	return fmt.Sprintf("%s-%08x", base, testRand.Uint32())
}

func requireEnv(key string) string {
	if v := strings.TrimSpace(TEST_ENV[key]); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	Fail(fmt.Sprintf("missing required env var %q (expected in TEST_ENV or process env)", key))
	return ""
}

// requireEnvOr returns the first non-empty value among:
//  1. preferred
//  2. TEST_ENV[key]
//  3. fallback
//
// If all are empty, it fails the test.
func requireEnvOr(preferred, key, fallback string) string {
	if v := strings.TrimSpace(preferred); v != "" {
		return v
	}
	if v := strings.TrimSpace(TEST_ENV[key]); v != "" {
		return v
	}
	if v := strings.TrimSpace(fallback); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	Fail("missing required value: preferred empty, TEST_ENV[" + key + "] empty, fallback empty")
	return ""
}

// deleteIgnoreNotFound deletes the object and ignores NotFound errors.
func deleteIgnoreNotFound(ctx context.Context, c client.Client, obj client.Object) error {
	if obj == nil || strings.TrimSpace(obj.GetName()) == "" {
		return nil
	}
	err := c.Delete(ctx, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func updateObjectWithRetry[T client.Object](ctx context.Context, c client.Client, keyObj T, mutate func(T)) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		cur := keyObj.DeepCopyObject().(T)
		if err := c.Get(ctx, types.NamespacedName{Name: keyObj.GetName(), Namespace: keyObj.GetNamespace()}, cur); err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}

		mutate(cur)

		if err := c.Update(ctx, cur); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

func updateStatusWithRetry[T client.Object](ctx context.Context, c client.Client, keyObj T, mutate func(T)) error {
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error

	for time.Now().Before(deadline) {
		cur := keyObj.DeepCopyObject().(T)
		if err := c.Get(ctx, types.NamespacedName{Name: keyObj.GetName(), Namespace: keyObj.GetNamespace()}, cur); err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}

		mutate(cur)

		if err := c.Status().Update(ctx, cur); err != nil {
			if apierrors.IsConflict(err) {
				lastErr = err
				time.Sleep(200 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

// githubEnsureTeam ensures a team exists in the org (idempotent).
// If the team already exists, it returns nil.
func githubEnsureTeam(ctx context.Context, client *githubAPI.Client, org, teamSlugOrName string) error {
	org = strings.TrimSpace(org)
	teamSlugOrName = strings.TrimSpace(teamSlugOrName)
	if org == "" || teamSlugOrName == "" || client == nil {
		return nil
	}

	// Try get by slug first. If it exists, done.
	_, resp, err := client.Teams.GetTeamBySlug(ctx, org, teamSlugOrName)
	if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Create team. GitHub slug is derived from name; keeping name == slugOrName is fine for tests.
	newTeam := &githubAPI.NewTeam{
		Name:    teamSlugOrName,
		Privacy: githubAPI.Ptr("closed"),
	}
	_, resp, err = client.Teams.CreateTeam(ctx, org, *newTeam)
	if err == nil {
		return nil
	}

	// If creation failed because it already exists (race), treat as success.
	// GitHub may return 422 "Validation Failed" for duplicate.
	if resp != nil && resp.StatusCode == 422 {
		return nil
	}

	return err
}

// githubEnsureRepoWithVisibility ensures a repo exists with the requested visibility.
// If the repo exists already, it does not attempt to change visibility.
func githubEnsureRepoWithVisibility(ctx context.Context, client *githubAPI.Client, org, repo string, private bool) error {
	org = strings.TrimSpace(org)
	repo = strings.TrimSpace(repo)
	if org == "" || repo == "" || client == nil {
		return nil
	}

	_, resp, err := client.Repositories.Get(ctx, org, repo)
	if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	r := &githubAPI.Repository{
		Name:    githubAPI.Ptr(repo),
		Private: githubAPI.Ptr(private),
	}
	_, resp, err = client.Repositories.Create(ctx, org, r)
	if err == nil {
		return nil
	}
	if resp != nil && resp.StatusCode == 422 {
		return nil
	}
	return err
}
