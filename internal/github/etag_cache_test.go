// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"sync"
	"testing"
)

func TestEtagCache_SetAndGet(t *testing.T) {
	c := &etagCache{entries: make(map[string]etagEntry)}

	// Nothing stored yet.
	if _, ok := c.getEtag("k"); ok {
		t.Fatal("expected no etag before set")
	}
	if _, ok := c.getValue("k"); ok {
		t.Fatal("expected no value before set")
	}

	c.set("k", `"abc"`, []string{"a", "b"})

	etag, ok := c.getEtag("k")
	if !ok {
		t.Fatal("expected etag after set")
	}
	if etag != `"abc"` {
		t.Errorf("etag: got %q, want %q", etag, `"abc"`)
	}

	val, ok := c.getValue("k")
	if !ok {
		t.Fatal("expected value after set")
	}
	s, _ := val.([]string)
	if len(s) != 2 || s[0] != "a" {
		t.Errorf("value: got %v", val)
	}
}

func TestEtagCache_Invalidate(t *testing.T) {
	c := &etagCache{entries: make(map[string]etagEntry)}
	c.set("k", `"x"`, "some-value")

	c.invalidate("k")

	if _, ok := c.getEtag("k"); ok {
		t.Error("expected no etag after invalidate")
	}
}

func TestEtagCache_ConcurrentAccess(t *testing.T) {
	c := &etagCache{entries: make(map[string]etagEntry)}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "k"
			c.set(key, `"etag"`, n)
			c.getEtag(key)  //nolint:errcheck
			c.getValue(key) //nolint:errcheck
		}(i)
	}
	wg.Wait()
}

func TestGetOrCreateOrgCache_SameInstance(t *testing.T) {
	// Two calls for the same github+org must return the same pointer.
	a := getOrCreateOrgCache("gh1", "org-cache-test")
	b := getOrCreateOrgCache("gh1", "org-cache-test")
	if a != b {
		t.Error("expected same *etagCache for same github+org")
	}
}

func TestGetOrCreateOrgCache_DifferentOrgs(t *testing.T) {
	a := getOrCreateOrgCache("gh1", "org-cache-test-alpha")
	b := getOrCreateOrgCache("gh1", "org-cache-test-beta")
	if a == b {
		t.Error("expected different *etagCache for different orgs")
	}
}

func TestGetOrCreateOrgCache_DifferentGithubInstances(t *testing.T) {
	// Same org name under different GitHub instances must not share cache.
	a := getOrCreateOrgCache("gh-instance-a", "shared-org")
	b := getOrCreateOrgCache("gh-instance-b", "shared-org")
	if a == b {
		t.Error("expected different *etagCache for same org on different github instances")
	}
}
