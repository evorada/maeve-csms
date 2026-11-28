// SPDX-License-Identifier: Apache-2.0

package tariffs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// SelectTimeout is the most time that the start of a transaction gives to the selection
// of its tariff. A transaction whose tariff is not chosen in that time starts without
// one: charging does not wait for a tariff.
const SelectTimeout = 2 * time.Second

// TokenTypeAdhocUser is the OCPI token type of a token that was made for one session of
// ad hoc payment.
const TokenTypeAdhocUser = "AD_HOC_USER" // #nosec G101 -- the name of an OCPI token type, not a credential

// Source says which rule chose the tariff of a transaction. It is stored with the
// transaction (store.Transaction.TariffSource).
type Source string

const (
	// SourceTokenGroup is the override tariff of the token group of the token.
	SourceTokenGroup Source = "token_group"
	// SourceConnectorAdhoc is the ad hoc tariff of the connector, for an ad hoc session.
	SourceConnectorAdhoc Source = "connector_adhoc"
	// SourceConnector is the tariff of the connector.
	SourceConnector Source = "connector"
	// SourceLocation is the default tariff of the location.
	SourceLocation Source = "location"
	// SourceTenantDefault is the default tariff of the config of the tenant.
	SourceTenantDefault Source = "tenant_default"
	// SourceFallback is no tariff, where the manager has a price for the sessions without
	// one.
	SourceFallback Source = "fallback"
	// SourceNone is no tariff and no price.
	SourceNone Source = "none"
)

// Choice is the tariff that was selected for a transaction: a version of a tariff and the
// rule that chose it. TariffId is empty and Version 0 for the sources SourceFallback and
// SourceNone, which stand for no tariff.
type Choice struct {
	TariffId string
	Version  int
	Source   Source
}

// SelectInput is the transaction that a tariff is selected for.
type SelectInput struct {
	// OrgId is the org that the transaction belongs to. The tariff is one of its tariffs.
	OrgId       string
	StationKey  string
	OcppVersion string // "ocpp1.6" or "ocpp2.0.1"
	// EvseId and ConnectorId are the ids that the station gave, as they are stored with
	// the transaction: OCPP 1.6 has no EVSEs and gives the connector id for both.
	EvseId      int
	ConnectorId int
	// TokenUid is the token that the transaction was started with; "" when it has none.
	TokenUid string
	// At is the time for which the tariff must be valid: the start of the transaction.
	At time.Time
}

// TenantConfigs gives the config of the tenant of an org (a *tenancy.ConfigProvider).
type TenantConfigs interface {
	ConfigForOrg(ctx context.Context, orgId string) (*tenancy.TenantConfig, error)
}

// Selector selects the tariff that a transaction is charged with.
type Selector struct {
	Assets   store.AssetStore
	Registry store.ChargeStationRegistryStore
	Tariffs  store.TariffStore
	Groups   store.TokenGroupStore
	Tokens   store.TokenStore
	// Tenants gives the default tariff and the parties of ad hoc payment of a tenant.
	// Nil is no tenant settings.
	Tenants TenantConfigs
	// Fallback says that the manager has a price for the sessions without a tariff: such
	// a transaction gets the source SourceFallback instead of SourceNone.
	Fallback bool
}

// AdhocSession reports whether a session with the token is a session of ad hoc payment:
// the token has the OCPI type AD_HOC_USER, or its issuer (country code and party id) is
// one of the parties of ad hoc payment of the config of the tenant. A session without a
// stored token is not ad hoc.
func AdhocSession(token *store.Token, cfg *tenancy.TenantConfig) bool {
	if token == nil {
		return false
	}
	return token.Type == TokenTypeAdhocUser || cfg.AdhocEmspParty(token.CountryCode, token.PartyId)
}

// Select returns the tariff of a transaction: the current version of the first of these
// tariffs that is a tariff of the org, is not deleted and is valid at the start of the
// transaction (the start and end times of its current version; the end is exclusive).
//
//  1. the override tariff of the token group of the token (SourceTokenGroup): the group of
//     the token with exactly the UID, otherwise the group whose tokens are prefixes that
//     has the longest prefix of the UID;
//  2. for an ad hoc session (AdhocSession), the ad hoc tariff of the connector
//     (SourceConnectorAdhoc);
//  3. the tariff of the connector (SourceConnector);
//  4. the default tariff of the location of the EVSE or, when the station has no such
//     EVSE in the asset model, of the location of the station (SourceLocation);
//  5. the default tariff of the config of the tenant (SourceTenantDefault).
//
// When none of them applies the transaction has no tariff: the Choice has no tariff id
// and the source SourceFallback when the manager has a price for such sessions, and
// SourceNone otherwise. A storage backend without tariffs, token groups or assets (their
// stores return store.ErrNotImplemented) has no tariff for any transaction.
//
// An error means that the tariff could not be selected, because a store failed: no
// Choice is returned then, not even one without a tariff.
func (s *Selector) Select(ctx context.Context, in SelectInput) (Choice, error) {
	noTariff := Choice{Source: SourceNone}
	if s.Fallback {
		noTariff.Source = SourceFallback
	}
	// a backend that does not keep the org of a transaction has no tariffs either
	if in.OrgId == "" {
		return noTariff, nil
	}

	choice, err := s.selectTariff(ctx, in)
	switch {
	case errors.Is(err, store.ErrNotImplemented):
		// the backend does not have one of the stores that hold the tariffs and where
		// they apply
		return noTariff, nil
	case err != nil:
		return Choice{}, fmt.Errorf("select tariff: %w", err)
	case choice == nil:
		return noTariff, nil
	}
	return *choice, nil
}

// selectTariff returns the first tariff of the order of Select that the transaction can
// be charged with; nil when there is none.
func (s *Selector) selectTariff(ctx context.Context, in SelectInput) (*Choice, error) {
	connector, evse, err := assets.ResolveConnector(ctx, s.Assets, in.StationKey, in.OcppVersion, in.EvseId, in.ConnectorId)
	if err != nil {
		return nil, err
	}
	location, err := s.location(ctx, in.StationKey, evse)
	if err != nil {
		return nil, err
	}
	token, group, err := s.token(ctx, in.OrgId, in.TokenUid)
	if err != nil {
		return nil, err
	}
	cfg, err := s.config(ctx, in.OrgId)
	if err != nil {
		return nil, err
	}

	type candidate struct {
		ref    *string
		source Source
	}
	var candidates []candidate
	if group != nil {
		candidates = append(candidates, candidate{group.OverrideTariffRef, SourceTokenGroup})
	}
	if connector != nil {
		if AdhocSession(token, cfg) {
			candidates = append(candidates, candidate{connector.AdhocTariffRef, SourceConnectorAdhoc})
		}
		candidates = append(candidates, candidate{connector.TariffRef, SourceConnector})
	}
	if location != nil {
		candidates = append(candidates, candidate{location.DefaultTariffRef, SourceLocation})
	}
	if ref := cfg.DefaultTariff(); ref != "" {
		candidates = append(candidates, candidate{&ref, SourceTenantDefault})
	}

	for _, c := range candidates {
		if c.ref == nil {
			continue
		}
		tariff, err := s.Tariffs.LookupTariff(ctx, *c.ref)
		if err != nil {
			return nil, err
		}
		if usable(tariff, in.OrgId, in.At) {
			return &Choice{TariffId: tariff.Id, Version: tariff.CurrentVersion, Source: c.source}, nil
		}
	}
	return nil, nil
}

// usable reports whether a transaction of the org that starts at the time can be charged
// with the tariff: the tariff exists, belongs to the org, is not deleted, and its current
// version is valid at the time.
func usable(tariff *store.Tariff, orgId string, at time.Time) bool {
	if tariff == nil || tariff.DeletedAt != nil || tariff.Current == nil || !strings.EqualFold(tariff.OrgId, orgId) {
		return false
	}
	if start := tariff.Current.StartDateTime; start != nil && start.After(at) {
		return false
	}
	if end := tariff.Current.EndDateTime; end != nil && !end.After(at) {
		return false
	}
	return true
}

// location returns the location whose default tariff applies: the location of the EVSE,
// or of the station when the station has no such EVSE in the asset model. It returns nil
// when there is none.
func (s *Selector) location(ctx context.Context, stationKey string, evse *store.AssetEvse) (*store.AssetLocation, error) {
	locationId := ""
	if evse != nil {
		locationId = evse.LocationId
	} else {
		station, err := stations.Lookup(ctx, s.Registry, stationKey)
		if err != nil {
			return nil, err
		}
		if station.LocationId != nil {
			locationId = *station.LocationId
		}
	}
	if locationId == "" {
		return nil, nil
	}
	return s.Assets.LookupAssetLocation(ctx, locationId)
}

// token returns the stored token of the org with the UID and the token group that the
// UID belongs to; nil for what there is not.
func (s *Selector) token(ctx context.Context, orgId, uid string) (*store.Token, *store.TokenGroup, error) {
	if uid == "" {
		return nil, nil, nil
	}
	token, err := s.Tokens.LookupTokenForOrg(ctx, orgId, uid)
	if err != nil {
		return nil, nil, err
	}
	group, err := s.Groups.FindTokenGroupForUid(ctx, orgId, uid)
	if err != nil {
		return nil, nil, err
	}
	return token, group, nil
}

// config returns the config of the tenant of the org: the zero config, which has no
// tariff settings, when there is no source of tenant configs.
func (s *Selector) config(ctx context.Context, orgId string) (*tenancy.TenantConfig, error) {
	if s.Tenants == nil {
		return &tenancy.TenantConfig{}, nil
	}
	cfg, err := s.Tenants.ConfigForOrg(ctx, orgId)
	if err != nil {
		return nil, fmt.Errorf("tenant config of org %s: %w", orgId, err)
	}
	return cfg, nil
}
