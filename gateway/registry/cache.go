// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
	"sync"
	"time"
)

const (
	// maxCacheEntries is the most charge station lookups that are kept.
	maxCacheEntries = 100_000

	// maxCacheKeyLength is the longest host and client id (together, in bytes) that are
	// kept: a host name has at most 253 characters and a station id 48. Both come from
	// the request of whoever connects, so without a limit the size of the cache would be
	// up to the sender.
	maxCacheKeyLength = 253 + 48
)

// CachingRegistry keeps the charge station lookups of another registry for a short time:
// a charge station that was found for TTL, a host or station that is not known for
// NegativeTTL. A lookup that failed is never kept, and a kept answer is never used after
// its time is over, also not when the inner registry fails.
//
// The answers are kept by normalized host and client id, so an answer for one host is
// never used for another host. A TTL that is not positive turns that kind of caching off.
// A CachingRegistry must not be copied after first use.
type CachingRegistry struct {
	Inner       DeviceRegistry
	TTL         time.Duration
	NegativeTTL time.Duration
	// Clock returns the current time; nil is time.Now.
	Clock func() time.Time

	mu        sync.Mutex
	entries   map[cacheKey]cacheEntry
	nextSweep time.Time
	// maxEntries is the most entries that are kept; 0 is maxCacheEntries.
	maxEntries int
}

type cacheKey struct {
	host     string
	clientId string
}

type cacheEntry struct {
	cs      *ChargeStation // nil: not known
	expires time.Time
}

func (c *CachingRegistry) LookupChargeStation(ctx context.Context, host, clientId string) (*ChargeStation, error) {
	// the inner registry is asked with the host of the key, so that what is kept for a
	// key is always the answer to exactly that key
	key := cacheKey{host: NormalizeHost(host), clientId: clientId}
	cacheable := len(key.host)+len(key.clientId) <= maxCacheKeyLength

	now := c.now()
	if cacheable {
		if cs, ok := c.get(key, now); ok {
			return cs, nil
		}
	}

	cs, err := c.Inner.LookupChargeStation(ctx, key.host, key.clientId)
	if err != nil {
		return nil, err
	}

	ttl := c.TTL
	if cs == nil {
		ttl = c.NegativeTTL
	}
	if cacheable && ttl > 0 {
		// the answer is not newer than the time at which the lookup started
		c.put(key, cacheEntry{cs: copyOf(cs), expires: now.Add(ttl)})
	}
	return cs, nil
}

func (c *CachingRegistry) LookupCertificate(ctx context.Context, certHash string) (*x509.Certificate, error) {
	return c.Inner.LookupCertificate(ctx, certHash)
}

func (c *CachingRegistry) now() time.Time {
	if c.Clock != nil {
		return c.Clock()
	}
	return time.Now()
}

// get returns the answer that is kept for the key; ok is false when there is none or its
// time is over.
func (c *CachingRegistry) get(key cacheKey, now time.Time) (cs *ChargeStation, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sweep(now)

	entry, found := c.entries[key]
	if !found {
		return nil, false
	}
	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return copyOf(entry.cs), true
}

func (c *CachingRegistry) put(key cacheKey, entry cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = make(map[cacheKey]cacheEntry)
	}
	if _, found := c.entries[key]; !found {
		limit := c.maxEntries
		if limit <= 0 {
			limit = maxCacheEntries
		}
		// full: make room by dropping any entry
		for k := range c.entries {
			if len(c.entries) < limit {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[key] = entry
}

// sweep drops the entries whose time is over. It goes through all entries, so it does
// that at most once per sweep interval. The caller holds the lock.
func (c *CachingRegistry) sweep(now time.Time) {
	if now.Before(c.nextSweep) {
		return
	}
	for k, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, k)
		}
	}
	c.nextSweep = now.Add(c.sweepInterval())
}

// sweepInterval is the longer of the two TTLs: an entry is dropped at the latest one
// interval after its time is over.
func (c *CachingRegistry) sweepInterval() time.Duration {
	if c.NegativeTTL > c.TTL {
		return c.NegativeTTL
	}
	return c.TTL
}

// copyOf returns a copy, so that a caller cannot change what the cache keeps.
func copyOf(cs *ChargeStation) *ChargeStation {
	if cs == nil {
		return nil
	}
	cp := *cs
	if cs.AcceptedCertOrgNames != nil {
		cp.AcceptedCertOrgNames = append([]string{}, cs.AcceptedCertOrgNames...)
	}
	return &cp
}
