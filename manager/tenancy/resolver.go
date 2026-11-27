// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// DefaultCacheTTL is how long a Resolver keeps a tenant when its CacheTTL is not set.
const DefaultCacheTTL = 30 * time.Second

// ErrUnknownHost is returned in multi mode when no tenant has the host.
var ErrUnknownHost = errors.New("unknown host")

// StationOrgResolver gives the org that owns a station key.
type StationOrgResolver interface {
	OrgIdForStation(ctx context.Context, chargeStationId string) (string, error)
}

// Resolver maps the host and the id that a charge station connects with to its identity
// inside the manager, and a station key to the org that owns the station. It must not
// be copied after first use.
type Resolver struct {
	// Mode is ModeSingle when empty.
	Mode Mode
	// Tenants is only used in multi mode.
	Tenants  store.TenantStore
	Stations store.ChargeStationAuthStore
	// DefaultOrgId is the org of a charge station that has none. Empty is the legacy
	// org (store.LegacyOrgId).
	DefaultOrgId string
	// Clock times the cache. Nil is the real clock.
	Clock clock.PassiveClock
	// CacheTTL is how long a tenant that was found by host, by org key or by org id is
	// kept (DefaultCacheTTL when not positive). A lookup that finds no tenant is not kept.
	CacheTTL time.Duration

	mu sync.Mutex
	// generation counts the calls of Invalidate: a tenant that was read from the store
	// before an Invalidate is not put in the cache after it.
	generation uint64
	cache      map[string]cachedTenant
}

type cachedTenant struct {
	tenant *store.Tenant
	// config is the config document of the tenant as it was read when the tenant was
	// loaded; nil when the document cannot be read. It goes when the tenant goes.
	config  *TenantConfig
	expires time.Time
}

// StationResolution is the identity of a charge station inside the manager.
type StationResolution struct {
	// Tenant is the tenant that the host belongs to; nil in single mode. It is shared
	// with the cache of the resolver and must not be changed.
	Tenant *store.Tenant
	// OrgId is the org of the tenant; the default org in single mode.
	OrgId string
	// OrgKey is "" in single mode.
	OrgKey string
	// StationKey is "{org_key}.{stationId}"; the station id in single mode.
	StationKey string
}

// ResolveStation maps the Host header and the station id of the URL that a charge
// station connects to, to the identity of the station inside the manager.
//
// Single mode ignores the host: the station key is the station id and the org is the
// default org. Multi mode takes the tenant that has the host (normalized with
// store.NormalizeHost); ErrUnknownHost when there is none. The status of the tenant is
// not looked at: a suspended tenant still resolves.
//
// The station id is not checked here (see ValidateStationId) and the station does not
// have to exist.
func (r *Resolver) ResolveStation(ctx context.Context, host, stationId string) (*StationResolution, error) {
	if !r.multi() {
		return &StationResolution{OrgId: r.defaultOrgId(), StationKey: stationId}, nil
	}

	host = store.NormalizeHost(host)
	if host == "" || len(host) > maxHostnameLength {
		return nil, ErrUnknownHost
	}
	tenant, err := r.lookupTenant("host:"+host, func(tenants store.TenantStore) (*store.Tenant, error) {
		return tenants.GetTenantByHost(ctx, host)
	})
	if err != nil {
		return nil, fmt.Errorf("lookup tenant of host %s: %w", host, err)
	}
	if tenant == nil {
		return nil, ErrUnknownHost
	}
	// without a proper org key the station key would not be in the namespace of the tenant
	if !orgKeyPattern.MatchString(tenant.OrgKey) {
		return nil, fmt.Errorf("tenant %s of host %s has an invalid org key %q", tenant.OrgId, host, tenant.OrgKey)
	}
	return &StationResolution{
		Tenant:     tenant,
		OrgId:      tenant.OrgId,
		OrgKey:     tenant.OrgKey,
		StationKey: StationKey(tenant.OrgKey, stationId),
	}, nil
}

// TenantForStationKey returns the tenant that has the org key that the station key
// starts with. It returns nil in single mode, for a key that does not start with an org
// key and when no tenant has the org key. The tenant is shared with the cache of the resolver and must
// not be changed.
func (r *Resolver) TenantForStationKey(ctx context.Context, stationKey string) (*store.Tenant, error) {
	if !r.multi() {
		return nil, nil
	}
	orgKey, _, ok := SplitStationKey(stationKey)
	if !ok || !orgKeyPattern.MatchString(orgKey) {
		return nil, nil
	}
	tenant, err := r.lookupTenant("org_key:"+orgKey, func(tenants store.TenantStore) (*store.Tenant, error) {
		return tenants.GetTenantByOrgKey(ctx, orgKey)
	})
	if err != nil {
		return nil, fmt.Errorf("lookup tenant of station key %s: %w", stationKey, err)
	}
	return tenant, nil
}

// TenantByOrgId returns the tenant of an org. It returns nil in single mode and for an
// org that is not a tenant here. The tenant is kept like one that was found by host or
// by org key, so its status can be as old as CacheTTL unless Invalidate was called for
// the org. The tenant is shared with the cache of the resolver and must not be changed.
func (r *Resolver) TenantByOrgId(ctx context.Context, orgId string) (*store.Tenant, error) {
	entry, err := r.entryByOrgId(ctx, orgId)
	return entry.tenant, err
}

// entryByOrgId is TenantByOrgId with what is kept next to the tenant. The entry has no
// tenant where TenantByOrgId returns nil.
func (r *Resolver) entryByOrgId(ctx context.Context, orgId string) (cachedTenant, error) {
	if !r.multi() {
		return cachedTenant{}, nil
	}
	orgId = normalizeOrgId(orgId)
	if orgId == "" {
		return cachedTenant{}, nil
	}
	entry, err := r.lookupEntry("org:"+orgId, func(tenants store.TenantStore) (*store.Tenant, error) {
		return tenants.GetTenant(ctx, orgId)
	})
	if err != nil {
		return cachedTenant{}, fmt.Errorf("lookup tenant of org %s: %w", orgId, err)
	}
	return entry, nil
}

// Invalidate drops what the resolver has cached for an org, so that the next lookup
// reads the tenant from the store.
func (r *Resolver) Invalidate(orgId string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generation++
	for key, entry := range r.cache {
		if strings.EqualFold(entry.tenant.OrgId, orgId) {
			delete(r.cache, key)
		}
	}
}

// OrgIdForStation returns the org that owns a charge station: ChargeStationAuth.OrgId
// when the station exists and has an org, otherwise (multi mode) the org of the tenant
// that has the org key of the station key, otherwise DefaultOrgId. A station of the
// legacy org has no org yet: the rows of the legacy org are given to the default org
// when the manager starts.
func (r *Resolver) OrgIdForStation(ctx context.Context, chargeStationId string) (string, error) {
	auth, err := r.Stations.LookupChargeStationAuth(ctx, chargeStationId)
	if err != nil {
		return "", fmt.Errorf("lookup org of charge station %s: %w", chargeStationId, err)
	}
	if orgId := StationOrgId(auth, ""); orgId != "" {
		return orgId, nil
	}
	tenant, err := r.TenantForStationKey(ctx, chargeStationId)
	if err != nil {
		return "", fmt.Errorf("lookup org of charge station %s: %w", chargeStationId, err)
	}
	if tenant != nil {
		return tenant.OrgId, nil
	}
	return r.defaultOrgId(), nil
}

// StationOrgId returns the org in the auth details of a charge station, or fallback
// when the station is not known (nil) or has no org. The legacy org counts as no org.
func StationOrgId(auth *store.ChargeStationAuth, fallback string) string {
	if auth != nil && auth.OrgId != "" && auth.OrgId != store.LegacyOrgId {
		return auth.OrgId
	}
	return fallback
}

func (r *Resolver) multi() bool {
	return r.Mode == ModeMulti
}

func (r *Resolver) defaultOrgId() string {
	if r.DefaultOrgId == "" {
		return store.LegacyOrgId
	}
	return r.DefaultOrgId
}

func (r *Resolver) now() time.Time {
	if r.Clock == nil {
		return time.Now()
	}
	return r.Clock.Now()
}

func (r *Resolver) cacheTTL() time.Duration {
	if r.CacheTTL <= 0 {
		return DefaultCacheTTL
	}
	return r.CacheTTL
}

// lookupTenant returns the tenant that is cached under cacheKey, or loads it from the
// tenant store and caches it when there is one.
func (r *Resolver) lookupTenant(cacheKey string, load func(store.TenantStore) (*store.Tenant, error)) (*store.Tenant, error) {
	entry, err := r.lookupEntry(cacheKey, load)
	return entry.tenant, err
}

// lookupEntry is lookupTenant with what is kept next to the tenant: its config is read
// once, when the tenant is loaded. The entry has no tenant when there is none.
func (r *Resolver) lookupEntry(cacheKey string, load func(store.TenantStore) (*store.Tenant, error)) (cachedTenant, error) {
	if r.Tenants == nil {
		return cachedTenant{}, errors.New("no tenant store")
	}
	now := r.now()

	r.mu.Lock()
	generation := r.generation
	entry, found := r.cache[cacheKey]
	if found && !now.Before(entry.expires) {
		delete(r.cache, cacheKey)
		found = false
	}
	r.mu.Unlock()
	if found {
		return entry, nil
	}

	tenant, err := load(r.Tenants)
	if err != nil || tenant == nil {
		return cachedTenant{}, err
	}
	entry = cachedTenant{tenant: tenant, config: storedConfig(tenant), expires: now.Add(r.cacheTTL())}

	r.mu.Lock()
	defer r.mu.Unlock()
	if generation == r.generation {
		if r.cache == nil {
			r.cache = make(map[string]cachedTenant)
		}
		r.cache[cacheKey] = entry
	}
	return entry, nil
}

// storedConfig reads the config document of a tenant. The control plane only stores a
// document that is valid, so one that cannot be read is an error of the stored data: it
// is logged and nil is returned, which makes the tenant fall back to the defaults.
func storedConfig(tenant *store.Tenant) *TenantConfig {
	cfg, err := ParseConfig(tenant.Config)
	if err != nil {
		slog.Error("tenant config cannot be read, falling back to the defaults",
			"org", tenant.OrgId, "configVersion", tenant.ConfigVersion, "err", err)
		return nil
	}
	return cfg
}
