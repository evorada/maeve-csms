// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"slices"
	"time"

	"golang.org/x/exp/slog"
)

// ConfigProvider gives the config of a tenant to the code that applies it. A nil
// provider, and a provider without a resolver, give the defaults.
//
// The configs come from the tenant cache of the resolver: a change through the control
// plane is seen at once by the manager that it was sent to (Resolver.Invalidate), and
// after the cache TTL by the others.
type ConfigProvider struct {
	// Resolver finds the tenants. Its mode decides whether there are any: in single mode
	// every org has the defaults.
	Resolver *Resolver
}

// ConfigForOrg returns the config of the tenant of an org. An org that is not a tenant
// (always so in single mode) and a tenant whose stored config cannot be read have the
// zero config, which stands for the defaults. The config is shared with the cache of
// the resolver and must not be changed.
func (p *ConfigProvider) ConfigForOrg(ctx context.Context, orgId string) (*TenantConfig, error) {
	cfg, _, err := p.lookup(ctx, orgId)
	return cfg, err
}

// ConfigForStation returns the config of the tenant that owns a charge station (see
// Resolver.OrgIdForStation), like ConfigForOrg.
func (p *ConfigProvider) ConfigForStation(ctx context.Context, chargeStationId string) (*TenantConfig, error) {
	if p == nil || p.Resolver == nil || !p.Resolver.multi() {
		return &TenantConfig{}, nil
	}
	orgId, err := p.Resolver.OrgIdForStation(ctx, chargeStationId)
	if err != nil {
		return nil, err
	}
	return p.ConfigForOrg(ctx, orgId)
}

// HeartbeatIntervalForStation returns the heartbeat interval of a charge station: the
// one of the tenant that owns it, or global when that tenant has not set one. A charge
// station is never refused because the config of its tenant cannot be looked up: the
// failure is logged and the station gets global.
func (p *ConfigProvider) HeartbeatIntervalForStation(ctx context.Context, chargeStationId string, global time.Duration) time.Duration {
	cfg, err := p.ConfigForStation(ctx, chargeStationId)
	if err != nil {
		slog.Warn("looking up the tenant config of a charge station, using the default heartbeat interval",
			"chargeStationId", chargeStationId, "err", err)
		return global
	}
	return cfg.HeartbeatInterval(global)
}

// AcceptedCertOrgNames returns the organisation names that the tenant of an org accepts
// in the client certificate of a charge station. An empty list means that the tenant
// does not want the organisation checked.
//
// known is false when there is no such list: the org is not a tenant (always so in
// single mode), or the stored config of the tenant cannot be read. That is not the same
// as an empty list: whoever checks certificates must then use its own list.
func (p *ConfigProvider) AcceptedCertOrgNames(ctx context.Context, orgId string) (names []string, known bool, err error) {
	cfg, stored, err := p.lookup(ctx, orgId)
	if err != nil || !stored {
		return nil, false, err
	}
	names = slices.Clone(cfg.AcceptedCertOrgNames)
	if names == nil {
		names = []string{}
	}
	return names, true, nil
}

// lookup returns the config of the tenant of an org. stored is false when the config is
// not the one that is stored for a tenant: the defaults because the org is not a
// tenant, or because the stored document cannot be read.
func (p *ConfigProvider) lookup(ctx context.Context, orgId string) (cfg *TenantConfig, stored bool, err error) {
	if p == nil || p.Resolver == nil {
		return &TenantConfig{}, false, nil
	}
	entry, err := p.Resolver.entryByOrgId(ctx, orgId)
	if err != nil {
		return nil, false, err
	}
	if entry.tenant == nil || entry.config == nil {
		return &TenantConfig{}, false, nil
	}
	return entry.config, true, nil
}
