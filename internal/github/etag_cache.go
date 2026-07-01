// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package github

import "sync"

// etagEntry stores an ETag header value and the last-parsed result for a cached resource.
type etagEntry struct {
	etag  string
	value any
}

// etagCache is a per-organisation cache of ETag entries keyed by an endpoint string.
type etagCache struct {
	mu      sync.RWMutex
	entries map[string]etagEntry
}

// orgCaches is the package-level map from org name to its *etagCache.
// It survives across reconcile cycles since providers are re-created on every Reconcile.
var orgCaches sync.Map // key: string, value: *etagCache

// getOrCreateOrgCache returns the *etagCache for the given org, creating it if necessary.
func getOrCreateOrgCache(org string) *etagCache {
	v, _ := orgCaches.LoadOrStore(org, &etagCache{entries: make(map[string]etagEntry)})
	return v.(*etagCache) //nolint:forcetypeassert
}

// getEtag returns the stored ETag for the given cache key, if any.
func (c *etagCache) getEtag(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	return e.etag, true
}

// getValue returns the stored parsed value for the given cache key, if any.
func (c *etagCache) getValue(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	return e.value, true
}

// set stores the ETag and parsed value for the given cache key.
func (c *etagCache) set(key string, etag string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = etagEntry{etag: etag, value: value}
}

// invalidate removes the entry for the given cache key.
func (c *etagCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
