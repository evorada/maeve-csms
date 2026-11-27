// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a clock that only moves when the test says so.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// countingRegistry is a mock registry that records its lookups and can be made to fail.
type countingRegistry struct {
	*MockRegistry
	mu      sync.Mutex
	err     error
	lookups []string
}

func newCountingRegistry() *countingRegistry {
	return &countingRegistry{MockRegistry: NewMockRegistry()}
}

func (r *countingRegistry) LookupChargeStation(ctx context.Context, host, clientId string) (*ChargeStation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, host+"/"+clientId)
	if r.err != nil {
		return nil, r.err
	}
	return r.MockRegistry.LookupChargeStation(ctx, host, clientId)
}

func (r *countingRegistry) LookupCertificate(ctx context.Context, certHash string) (*x509.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, "certificate/"+certHash)
	return r.MockRegistry.LookupCertificate(ctx, certHash)
}

func (r *countingRegistry) setError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *countingRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lookups)
}

func newCachingRegistry(inner DeviceRegistry, clock *fakeClock) *CachingRegistry {
	return &CachingRegistry{
		Inner:       inner,
		TTL:         10 * time.Second,
		NegativeTTL: 2 * time.Second,
		Clock:       clock.Now,
	}
}

func TestCachingRegistryKeepsAChargeStationForTheTTL(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa", Base64SHA256Password: "old"})
	reg := newCachingRegistry(inner, clock)

	first, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "aaaa.CP001", first.StationKey)
	assert.Equal(t, 1, inner.count())

	// what the caller does with the answer does not change what is kept
	first.StationKey = "changed"

	// the registration changes: not seen until the time of the answer is over
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa", Base64SHA256Password: "new"})

	clock.Advance(10*time.Second - time.Nanosecond)
	kept, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, kept)
	assert.Equal(t, "aaaa.CP001", kept.StationKey)
	assert.Equal(t, "old", kept.Base64SHA256Password)
	assert.Equal(t, 1, inner.count())

	clock.Advance(time.Nanosecond)
	fresh, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, fresh)
	assert.Equal(t, "new", fresh.Base64SHA256Password)
	assert.Equal(t, 2, inner.count())
}

func TestCachingRegistryKeepsAnUnknownChargeStationForTheNegativeTTL(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	reg := newCachingRegistry(inner, clock)

	got, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 1, inner.count())

	// the station is registered: not seen until the time of the answer is over
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})

	clock.Advance(2*time.Second - time.Nanosecond)
	got, err = reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 1, inner.count())

	// the negative TTL is over (the positive one is not)
	clock.Advance(time.Nanosecond)
	got, err = reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "aaaa.CP001", got.StationKey)
	assert.Equal(t, 2, inner.count())
}

func TestCachingRegistryDoesNotKeepErrors(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})
	reg := newCachingRegistry(inner, clock)

	lookupErr := errors.New("manager is down")
	inner.setError(lookupErr)

	got, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
	assert.ErrorIs(t, err, lookupErr)
	assert.Nil(t, got)

	// the next lookup asks again and its answer is kept
	inner.setError(nil)
	got, err = reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2, inner.count())

	// an answer whose time is over is not used when the lookup fails
	inner.setError(lookupErr)
	got, err = reg.LookupChargeStation(ctx, "a.example", "CP001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2, inner.count())

	clock.Advance(10 * time.Second)
	got, err = reg.LookupChargeStation(ctx, "a.example", "CP001")
	assert.ErrorIs(t, err, lookupErr)
	assert.Nil(t, got)
	assert.Equal(t, 3, inner.count())
}

func TestCachingRegistryKeepsAnswersByHostAndClientId(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})
	inner.Add("b.example", &ChargeStation{ClientId: "CP001", OrgKey: "bbbb"})
	inner.Add("a.example", &ChargeStation{ClientId: "CP002", OrgKey: "aaaa"})
	reg := newCachingRegistry(inner, clock)

	lookup := func(host, clientId string) string {
		t.Helper()
		cs, err := reg.LookupChargeStation(ctx, host, clientId)
		require.NoError(t, err)
		if cs == nil {
			return ""
		}
		return cs.StationKey
	}

	// twice: from the inner registry and from the cache
	for i := 0; i < 2; i++ {
		assert.Equal(t, "aaaa.CP001", lookup("a.example", "CP001"))
		assert.Equal(t, "bbbb.CP001", lookup("b.example", "CP001"))
		assert.Equal(t, "aaaa.CP002", lookup("a.example", "CP002"))
		assert.Equal(t, "", lookup("c.example", "CP001"))
		assert.Equal(t, "", lookup("b.example", "CP002"))
	}
	assert.Equal(t, 5, inner.count())

	// other spellings of a host are the same host
	assert.Equal(t, "aaaa.CP001", lookup("A.Example:443", "CP001"))
	assert.Equal(t, "bbbb.CP001", lookup("b.example.", "CP001"))
	assert.Equal(t, 5, inner.count())

	// the inner registry is asked for the host as it is kept
	assert.Equal(t, "", lookup("C.Example:80", "CP003"))
	assert.Equal(t, "c.example/CP003", inner.lookups[5])
}

func TestCachingRegistryWithoutTTLsAlwaysAsks(t *testing.T) {
	ctx := context.Background()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})
	reg := &CachingRegistry{Inner: inner}

	for i := 0; i < 2; i++ {
		got, err := reg.LookupChargeStation(ctx, "a.example", "CP001")
		require.NoError(t, err)
		assert.NotNil(t, got)

		got, err = reg.LookupChargeStation(ctx, "a.example", "CP002")
		require.NoError(t, err)
		assert.Nil(t, got)
	}
	assert.Equal(t, 4, inner.count())
	assert.Empty(t, reg.entries)
}

func TestCachingRegistryDoesNotKeepOversizedKeys(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	reg := newCachingRegistry(inner, clock)

	host := strings.Repeat("a", maxCacheKeyLength)
	for i := 0; i < 2; i++ {
		got, err := reg.LookupChargeStation(ctx, host, "CP001")
		require.NoError(t, err)
		assert.Nil(t, got)
	}
	assert.Equal(t, 2, inner.count())
	assert.Empty(t, reg.entries)
}

func TestCachingRegistryDropsEntriesWhoseTimeIsOver(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})
	inner.Add("a.example", &ChargeStation{ClientId: "CP002", OrgKey: "aaaa"})
	reg := newCachingRegistry(inner, clock)

	lookup := func(clientId string) {
		t.Helper()
		_, err := reg.LookupChargeStation(ctx, "a.example", clientId)
		require.NoError(t, err)
	}

	lookup("CP001")   // kept for 10 seconds
	lookup("unknown") // kept for 2 seconds
	clock.Advance(6 * time.Second)
	lookup("CP002")
	assert.Len(t, reg.entries, 3)

	// a lookup of another station drops what is over
	clock.Advance(4 * time.Second)
	lookup("CP003")
	assert.Len(t, reg.entries, 2)
	assert.Contains(t, reg.entries, cacheKey{host: "a.example", clientId: "CP002"})
	assert.Contains(t, reg.entries, cacheKey{host: "a.example", clientId: "CP003"})
}

func TestCachingRegistryIsLimitedInSize(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	reg := newCachingRegistry(inner, clock)
	reg.maxEntries = 3

	hosts := []string{"a.example", "b.example", "c.example", "d.example", "e.example"}
	for _, host := range hosts {
		_, err := reg.LookupChargeStation(ctx, host, "CP001")
		require.NoError(t, err)
		assert.LessOrEqual(t, len(reg.entries), 3)

		// the latest answer is always kept
		assert.Contains(t, reg.entries, cacheKey{host: host, clientId: "CP001"})
	}
	assert.Len(t, reg.entries, 3)
	assert.Equal(t, 5, inner.count())

	// the latest answer is used
	_, err := reg.LookupChargeStation(ctx, "e.example", "CP001")
	require.NoError(t, err)
	assert.Equal(t, 5, inner.count())
}

func TestCachingRegistryPassesCertificateLookupsThrough(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Certificates["hash"] = &x509.Certificate{}
	reg := newCachingRegistry(inner, clock)

	for i := 0; i < 2; i++ {
		cert, err := reg.LookupCertificate(ctx, "hash")
		require.NoError(t, err)
		assert.Same(t, inner.Certificates["hash"], cert)
	}
	assert.Equal(t, []string{"certificate/hash", "certificate/hash"}, inner.lookups)
}

func TestCachingRegistryIsSafeForConcurrentUse(t *testing.T) {
	ctx := context.Background()
	clock := newFakeClock()
	inner := newCountingRegistry()
	inner.Add("a.example", &ChargeStation{ClientId: "CP001", OrgKey: "aaaa"})
	reg := newCachingRegistry(inner, clock)
	reg.maxEntries = 4

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hosts := []string{"a.example", "b.example", "c.example", "d.example", "e.example", "f.example"}
			for j := 0; j < 200; j++ {
				host := hosts[(i+j)%len(hosts)]
				cs, err := reg.LookupChargeStation(ctx, host, "CP001")
				assert.NoError(t, err)
				assert.Equal(t, host == "a.example", cs != nil)
				if j%50 == 0 {
					clock.Advance(time.Second)
				}
			}
		}(i)
	}
	wg.Wait()
}
