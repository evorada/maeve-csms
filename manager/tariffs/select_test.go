// SPDX-License-Identifier: Apache-2.0

package tariffs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

const (
	selectOrgA    = "0193b2c4-0000-7000-8000-0000000000a1"
	selectOrgB    = "0193b2c4-0000-7000-8000-0000000000b2"
	selectStation = "k7f2.CP001"
	// a station of the org that has no EVSEs in the asset model
	selectBareStation = "k7f2.CP002"
)

// selectStart is the start of the transactions of the tests.
var selectStart = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

// selectFixture is an org with a charge station at a location. EVSE 1 of the station has
// a connector with a regular and an ad hoc tariff, EVSE 2 a connector without tariffs,
// and EVSE 3 two connectors of which the second has a tariff. The org has a token group
// with an override tariff, and tokens of every kind.
type selectFixture struct {
	t        *testing.T
	engine   *inmemory.Store
	selector *tariffs.Selector
	location *store.AssetLocation
	// the tariffs by what they are for
	employee, adhoc, regular, second, atLocation, ofTenant *store.Tariff
	group                                                  *store.TokenGroup
}

func (f *selectFixture) tariff(name string, v *store.TariffVersion) *store.Tariff {
	f.t.Helper()
	tariff := &store.Tariff{OrgId: selectOrgA, PartyRef: "party-a", Name: name}
	v.CreatedBy = "user:alice"
	require.NoError(f.t, f.engine.CreateTariff(context.Background(), tariff, v))
	return tariff
}

// simple returns the tariff version of a fixture of the simple tariff form.
func simple(t *testing.T, file string) *store.TariffVersion {
	t.Helper()
	fixture, ok := loadSimpleFixtures(t)[file]
	require.True(t, ok, file)
	v, err := tariffs.FromSimple(fixture.Simple)
	require.NoError(t, err)
	return v
}

func (f *selectFixture) token(tokenType, countryCode, partyId, uid string) {
	f.t.Helper()
	require.NoError(f.t, f.engine.SetToken(context.Background(), &store.Token{
		OrgId: selectOrgA, CountryCode: countryCode, PartyId: partyId, Type: tokenType, Uid: uid,
		ContractId: countryCode + partyId + "C" + uid, Issuer: "Issuer", Valid: true, CacheMode: store.CacheModeAlways,
	}))
}

func (f *selectFixture) member(uid string) {
	f.t.Helper()
	require.NoError(f.t, f.engine.SetTokenGroup(context.Background(), selectOrgA, uid, &f.group.Id))
}

// setConfig stores the config document of the tenant of the fixture.
func (f *selectFixture) setConfig(version int64, config map[string]any) {
	f.t.Helper()
	ctx := context.Background()
	config["version"] = version
	document, err := json.Marshal(config)
	require.NoError(f.t, err)
	require.NoError(f.t, tenancy.ValidateConfig(document))
	tenant, err := f.engine.GetTenant(ctx, selectOrgA)
	require.NoError(f.t, err)
	tenant.ConfigVersion = version
	tenant.Config = document
	require.NoError(f.t, f.engine.PutTenant(ctx, tenant))
	f.selector.Tenants.(*tenancy.ConfigProvider).Resolver.Invalidate(selectOrgA)
}

func (f *selectFixture) connector(evseId, connectorId int) *store.AssetConnector {
	f.t.Helper()
	c, err := f.engine.LookupAssetConnectorByOcpp(context.Background(), selectStation, evseId, connectorId)
	require.NoError(f.t, err)
	require.NotNil(f.t, c)
	return c
}

func (f *selectFixture) updateConnector(evseId, connectorId int, change func(c *store.AssetConnector)) {
	f.t.Helper()
	c := f.connector(evseId, connectorId)
	change(c)
	require.NoError(f.t, f.engine.UpdateAssetConnector(context.Background(), c))
}

func (f *selectFixture) setLocationDefault(ref *string) {
	f.t.Helper()
	f.location.DefaultTariffRef = ref
	require.NoError(f.t, f.engine.UpdateAssetLocation(context.Background(), f.location))
}

// in is a transaction of the org at the OCPP 1.6 connector with the token.
func in16(connectorId int, tokenUid string) tariffs.SelectInput {
	return tariffs.SelectInput{
		OrgId: selectOrgA, StationKey: selectStation, OcppVersion: "ocpp1.6",
		EvseId: connectorId, ConnectorId: connectorId, TokenUid: tokenUid, At: selectStart,
	}
}

func newSelectFixture(t *testing.T) *selectFixture {
	t.Helper()
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	f := &selectFixture{t: t, engine: engine}
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: selectOrgA, Path: store.OrgPathLabel(selectOrgA), Slug: "tenant-a", OrgKey: "k7f2"}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	f.selector = &tariffs.Selector{
		Assets: engine, Registry: engine, Tariffs: engine, Groups: engine, Tokens: engine,
		Tenants: &tenancy.ConfigProvider{Resolver: resolver},
	}

	f.employee = f.tariff("Employees", simple(t, "S8-free.json"))
	f.adhoc = f.tariff("Ad hoc", simple(t, "S6-ad-hoc.json"))
	f.regular = f.tariff("Regular", simple(t, "S1-energy-only.json"))
	f.second = f.tariff("Second connector", simple(t, "S2-start-fee-and-energy.json"))
	f.atLocation = f.tariff("Location", simple(t, "S3-energy-and-charging-time.json"))
	f.ofTenant = f.tariff("Tenant", simple(t, "S4-energy-and-parking.json"))

	for _, stationKey := range []string{selectStation, selectBareStation} {
		require.NoError(t, engine.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: selectOrgA}))
	}
	f.location = &store.AssetLocation{
		OrgId: selectOrgA, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, f.location))
	connector := func(id int, tariffRef, adhocTariffRef *string) *store.AssetConnector {
		return &store.AssetConnector{
			OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			TariffRef: tariffRef, AdhocTariffRef: adhocTariffRef,
		}
	}
	for ocppEvseId, connectors := range map[int][]*store.AssetConnector{
		1: {connector(1, &f.regular.Id, &f.adhoc.Id)},
		2: {connector(1, nil, nil)},
		3: {connector(1, nil, nil), connector(2, &f.second.Id, nil)},
	} {
		require.NoError(t, engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: selectOrgA, LocationId: f.location.Id, StationKey: selectStation, OcppEvseId: ocppEvseId, Connectors: connectors,
		}))
	}

	// a charge station is at the location of its EVSEs
	require.NoError(t, engine.MoveChargeStationToLocation(ctx, selectStation, f.location.Id))

	f.group = &store.TokenGroup{OrgId: selectOrgA, Name: "Employees", OverrideTariffRef: &f.employee.Id}
	require.NoError(t, engine.CreateTokenGroup(ctx, f.group))
	f.token("RFID", "ES", "CPO", "RFID0001")
	f.token("AD_HOC_USER", "ES", "CPO", "ADHOC001")
	f.token("APP_USER", "XX", "PAY", "PAY00001") // of the party that sells ad hoc sessions
	f.token("RFID", "ES", "CPO", "EMP00001")
	f.token("AD_HOC_USER", "ES", "CPO", "EMPADHOC")
	f.member("EMP00001")
	f.member("EMPADHOC")
	return f
}

func (f *selectFixture) assertChoice(want *store.Tariff, source tariffs.Source, in tariffs.SelectInput, msgAndArgs ...any) {
	f.t.Helper()
	choice, err := f.selector.Select(context.Background(), in)
	require.NoError(f.t, err, msgAndArgs...)
	if want == nil {
		assert.Equal(f.t, tariffs.Choice{Source: source}, choice, msgAndArgs...)
		return
	}
	assert.Equal(f.t, tariffs.Choice{TariffId: want.Id, Version: 1, Source: source}, choice, msgAndArgs...)
}

// The order of the selection: the override tariff of the token group, the ad hoc tariff
// of the connector for an ad hoc session, the tariff of the connector, the default
// tariff of the location, the default tariff of the tenant.
func TestSelectOrder(t *testing.T) {
	f := newSelectFixture(t)
	f.setLocationDefault(&f.atLocation.Id)
	f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id, "adhoc_emsp_parties": []string{"xx*pay"}})

	for name, tc := range map[string]struct {
		in     tariffs.SelectInput
		want   *store.Tariff
		source tariffs.Source
	}{
		// S7: a token group with an override tariff
		"S7: the token of a group with an override, at a connector with both tariffs": {in16(1, "EMP00001"), f.employee, tariffs.SourceTokenGroup},
		"S7: an ad hoc token of the group":                                            {in16(1, "EMPADHOC"), f.employee, tariffs.SourceTokenGroup},
		"S7: the token of the group at a connector without tariffs":                   {in16(2, "EMP00001"), f.employee, tariffs.SourceTokenGroup},
		// S6: a connector with a tariff for ad hoc payment
		"S6: an ad hoc token":                                          {in16(1, "ADHOC001"), f.adhoc, tariffs.SourceConnectorAdhoc},
		"S6: an RFID token":                                            {in16(1, "RFID0001"), f.regular, tariffs.SourceConnector},
		"S6: a token of a party of ad hoc payment":                     {in16(1, "PAY00001"), f.adhoc, tariffs.SourceConnectorAdhoc},
		"a token that is not stored":                                   {in16(1, "UNKNOWN1"), f.regular, tariffs.SourceConnector},
		"no token":                                                     {in16(1, ""), f.regular, tariffs.SourceConnector},
		"a connector without tariffs":                                  {in16(2, "RFID0001"), f.atLocation, tariffs.SourceLocation},
		"an ad hoc token at a connector without tariffs":               {in16(2, "ADHOC001"), f.atLocation, tariffs.SourceLocation},
		"an EVSE that the station does not have":                       {in16(9, "RFID0001"), f.atLocation, tariffs.SourceLocation},
		"the station as a whole (connector 0)":                         {in16(0, "RFID0001"), f.atLocation, tariffs.SourceLocation},
		"OCPP 2.0.1: the second connector of an EVSE":                  {in201(3, 2, "RFID0001"), f.second, tariffs.SourceConnector},
		"OCPP 2.0.1: the first connector of that EVSE has no tariff":   {in201(3, 1, "RFID0001"), f.atLocation, tariffs.SourceLocation},
		"OCPP 2.0.1: an EVSE without the connector":                    {in201(3, 0, "RFID0001"), f.atLocation, tariffs.SourceLocation},
		"OCPP 2.0.1: the connector of EVSE 1":                          {in201(1, 1, "ADHOC001"), f.adhoc, tariffs.SourceConnectorAdhoc},
		"OCPP 2.0.1: the ids are not swapped (EVSE 1 has 1 connector)": {in201(1, 2, "RFID0001"), f.atLocation, tariffs.SourceLocation},
	} {
		f.assertChoice(tc.want, tc.source, tc.in, name)
	}

	t.Run("S6: an ad hoc token at a connector without an ad hoc tariff gets the regular tariff", func(t *testing.T) {
		f.updateConnector(1, 1, func(c *store.AssetConnector) { c.AdhocTariffRef = nil })
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "ADHOC001"))
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "PAY00001"))
		f.updateConnector(1, 1, func(c *store.AssetConnector) { c.AdhocTariffRef = &f.adhoc.Id })
	})

	t.Run("a connector with only an ad hoc tariff", func(t *testing.T) {
		f.updateConnector(1, 1, func(c *store.AssetConnector) { c.TariffRef = nil })
		f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "ADHOC001"))
		f.assertChoice(f.atLocation, tariffs.SourceLocation, in16(1, "RFID0001"), "the ad hoc tariff is for ad hoc sessions only")
		f.updateConnector(1, 1, func(c *store.AssetConnector) { c.TariffRef = &f.regular.Id })
	})

	t.Run("a party is a party of ad hoc payment only while the tenant lists it", func(t *testing.T) {
		f.setConfig(2, map[string]any{"default_tariff_ref": f.ofTenant.Id})
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "PAY00001"))
		f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "ADHOC001"), "the type of the token still counts")
		f.setConfig(3, map[string]any{"default_tariff_ref": f.ofTenant.Id, "adhoc_emsp_parties": []string{"ES*OTH", "XX*PAY"}})
		f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "PAY00001"))
	})

	t.Run("a group without an override tariff changes nothing", func(t *testing.T) {
		f.group.OverrideTariffRef = nil
		require.NoError(t, f.engine.UpdateTokenGroup(context.Background(), f.group))
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "EMP00001"))
		f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "EMPADHOC"))
		f.group.OverrideTariffRef = &f.employee.Id
		require.NoError(t, f.engine.UpdateTokenGroup(context.Background(), f.group))
	})

	t.Run("down to the tenant default, and to no tariff", func(t *testing.T) {
		f.setLocationDefault(nil)
		f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, in16(2, "RFID0001"), "no location default")
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "RFID0001"), "the connector still has its own")

		f.setConfig(4, map[string]any{})
		f.assertChoice(nil, tariffs.SourceNone, in16(2, "RFID0001"), "no tenant default")
		f.selector.Fallback = true
		f.assertChoice(nil, tariffs.SourceFallback, in16(2, "RFID0001"), "the manager has a price for sessions without a tariff")
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "RFID0001"), "a tariff comes before the fallback")
	})
}

// in201 is a transaction of the org at the OCPP 2.0.1 EVSE and connector with the token.
func in201(evseId, connectorId int, tokenUid string) tariffs.SelectInput {
	return tariffs.SelectInput{
		OrgId: selectOrgA, StationKey: selectStation, OcppVersion: "ocpp2.0.1",
		EvseId: evseId, ConnectorId: connectorId, TokenUid: tokenUid, At: selectStart,
	}
}

// A candidate that cannot be used is passed over for the next one: a tariff that is
// deleted, that belongs to another org, that is not valid yet or no longer, or that does
// not exist.
func TestSelectSkipsTariffsThatCannotBeUsed(t *testing.T) {
	ctx := context.Background()
	hour := time.Hour

	// each case makes one tariff unusable in its own way
	type spoil func(f *selectFixture, tariff **store.Tariff)
	withValidity := func(start, end *time.Time) spoil {
		return func(f *selectFixture, tariff **store.Tariff) {
			v := simple(f.t, "S1-energy-only.json")
			v.StartDateTime, v.EndDateTime = start, end
			*tariff = f.tariff("Replaced "+(*tariff).Name, v)
		}
	}
	at := func(offset time.Duration) *time.Time {
		instant := selectStart.Add(offset)
		return &instant
	}
	spoils := map[string]spoil{
		"deleted": func(f *selectFixture, tariff **store.Tariff) {
			// a tariff is deleted while nothing refers to it; the references come after
			replacement := f.tariff("Deleted "+(*tariff).Name, simple(f.t, "S1-energy-only.json"))
			require.NoError(f.t, f.engine.DeleteTariff(ctx, replacement.Id))
			*tariff = replacement
		},
		"of another org": func(f *selectFixture, tariff **store.Tariff) {
			other := &store.Tariff{OrgId: selectOrgB, PartyRef: "party-b", Name: "Of B " + (*tariff).Name}
			v := simple(f.t, "S1-energy-only.json")
			v.CreatedBy = "user:bob"
			require.NoError(f.t, f.engine.CreateTariff(ctx, other, v))
			*tariff = other
		},
		"not valid yet":       withValidity(at(time.Second), nil),
		"no longer valid":     withValidity(nil, at(-hour)),
		"ends at the start":   withValidity(at(-hour), at(0)),
		"valid from tomorrow": withValidity(at(24*hour), at(48*hour)),
	}

	for name, spoil := range spoils {
		t.Run(name, func(t *testing.T) {
			f := newSelectFixture(t)
			f.setLocationDefault(&f.atLocation.Id)
			f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id})
			f.assertChoice(f.employee, tariffs.SourceTokenGroup, in16(1, "EMPADHOC"))

			// the override of the group: on to the ad hoc tariff of the connector
			spoil(f, &f.employee)
			f.group.OverrideTariffRef = &f.employee.Id
			require.NoError(t, f.engine.UpdateTokenGroup(ctx, f.group))
			f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "EMPADHOC"))

			// the ad hoc tariff: on to the tariff of the connector
			spoil(f, &f.adhoc)
			f.updateConnector(1, 1, func(c *store.AssetConnector) { c.AdhocTariffRef = &f.adhoc.Id })
			f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "EMPADHOC"))

			// the tariff of the connector: on to the default of the location
			spoil(f, &f.regular)
			f.updateConnector(1, 1, func(c *store.AssetConnector) { c.TariffRef = &f.regular.Id })
			f.assertChoice(f.atLocation, tariffs.SourceLocation, in16(1, "EMPADHOC"))

			// the default of the location: on to the default of the tenant
			spoil(f, &f.atLocation)
			f.setLocationDefault(&f.atLocation.Id)
			f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, in16(1, "EMPADHOC"))

			// the default of the tenant: no tariff
			spoil(f, &f.ofTenant)
			f.setConfig(2, map[string]any{"default_tariff_ref": f.ofTenant.Id})
			f.assertChoice(nil, tariffs.SourceNone, in16(1, "EMPADHOC"))
		})
	}

	t.Run("a tariff is valid from its start and until its end", func(t *testing.T) {
		f := newSelectFixture(t)
		v := simple(t, "S1-energy-only.json")
		v.StartDateTime, v.EndDateTime = at(0), at(hour)
		window := f.tariff("Window", v)
		f.updateConnector(2, 1, func(c *store.AssetConnector) { c.TariffRef = &window.Id })

		for offset, valid := range map[time.Duration]bool{
			-time.Nanosecond:       false,
			0:                      true,
			30 * time.Minute:       true,
			hour - time.Nanosecond: true,
			hour:                   false,
			2 * hour:               false,
		} {
			in := in16(2, "RFID0001")
			in.At = selectStart.Add(offset)
			if valid {
				f.assertChoice(window, tariffs.SourceConnector, in, "%s", offset)
			} else {
				f.assertChoice(nil, tariffs.SourceNone, in, "%s", offset)
			}
		}
		// in another time zone the instant is the same
		in := in16(2, "RFID0001")
		in.At = selectStart.In(time.FixedZone("UTC+5", 5*3600))
		f.assertChoice(window, tariffs.SourceConnector, in)
	})

	t.Run("a tenant default that names no tariff", func(t *testing.T) {
		f := newSelectFixture(t)
		for i, ref := range []string{"tariff-standard", "0193b2c5-0000-7000-8000-000000000099", ""} {
			f.setConfig(int64(i+1), map[string]any{"default_tariff_ref": ref})
			f.assertChoice(nil, tariffs.SourceNone, in16(2, "RFID0001"), ref)
		}
	})

	t.Run("the version that is selected is the current one", func(t *testing.T) {
		f := newSelectFixture(t)
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "RFID0001"))
		newer := simple(t, "S2-start-fee-and-energy.json")
		newer.CreatedBy = "user:alice"
		require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 1, newer))

		choice, err := f.selector.Select(ctx, in16(1, "RFID0001"))
		require.NoError(t, err)
		assert.Equal(t, tariffs.Choice{TariffId: f.regular.Id, Version: 2, Source: tariffs.SourceConnector}, choice)

		// a new version that is not valid yet takes the tariff out of use until then
		later := simple(t, "S1-energy-only.json")
		later.CreatedBy = "user:alice"
		later.StartDateTime = at(24 * hour)
		require.NoError(t, f.engine.AddTariffVersion(ctx, f.regular.Id, 2, later))
		f.assertChoice(nil, tariffs.SourceNone, in16(1, "RFID0001"))
	})
}

// The tokens of a group with prefix match are prefixes: a presented UID that starts with
// one of them belongs to the group.
func TestSelectPrefixGroup(t *testing.T) {
	ctx := context.Background()
	f := newSelectFixture(t)
	fleetTariff := f.tariff("Fleet", simple(t, "S2-start-fee-and-energy.json"))
	fleet := &store.TokenGroup{OrgId: selectOrgA, Name: "Fleet", PrefixMatch: true, OverrideTariffRef: &fleetTariff.Id}
	require.NoError(t, f.engine.CreateTokenGroup(ctx, fleet))
	f.token("RFID", "ES", "CPO", "04A1")
	require.NoError(t, f.engine.SetTokenGroup(ctx, selectOrgA, "04A1", &fleet.Id))

	f.assertChoice(fleetTariff, tariffs.SourceTokenGroup, in16(1, "04A1B2C3D4"), "a UID that is not stored")
	f.assertChoice(fleetTariff, tariffs.SourceTokenGroup, in16(1, "04a1b2c3d4"), "in any case")
	f.assertChoice(fleetTariff, tariffs.SourceTokenGroup, in16(2, "04A1B2C3D4"), "at a connector without tariffs")
	f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "05A1B2C3D4"), "another prefix")

	// a stored token with the prefix: the group is still found, and what the token is
	// still decides whether the session is ad hoc when the override cannot be used
	f.token("AD_HOC_USER", "ES", "CPO", "04A1ADHOC1")
	f.assertChoice(fleetTariff, tariffs.SourceTokenGroup, in16(1, "04A1ADHOC1"))
	require.NoError(t, f.engine.DeleteTokenGroup(ctx, fleet.Id))
	f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "04A1ADHOC1"))
	f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "04A1B2C3D4"), "the group is gone")

	// the token of a group comes before a prefix
	fleet = &store.TokenGroup{OrgId: selectOrgA, Name: "Fleet", PrefixMatch: true, OverrideTariffRef: &fleetTariff.Id}
	require.NoError(t, f.engine.CreateTokenGroup(ctx, fleet))
	require.NoError(t, f.engine.SetTokenGroup(ctx, selectOrgA, "04A1", &fleet.Id))
	f.token("RFID", "ES", "CPO", "04A1EMP001")
	f.member("04A1EMP001")
	f.assertChoice(f.employee, tariffs.SourceTokenGroup, in16(1, "04A1EMP001"))
}

// A station that has no EVSEs in the asset model gets the default tariff of its own
// location, and the default of the tenant when it has no location.
func TestSelectForAStationWithoutAssets(t *testing.T) {
	ctx := context.Background()
	f := newSelectFixture(t)
	f.setLocationDefault(&f.atLocation.Id)
	f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id})
	in := in16(1, "RFID0001")
	in.StationKey = selectBareStation

	f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, in, "a station without a location")

	require.NoError(t, f.engine.MoveChargeStationToLocation(ctx, selectBareStation, f.location.Id))
	f.assertChoice(f.atLocation, tariffs.SourceLocation, in, "the location of the station")
	employee := in
	employee.TokenUid = "EMP00001"
	f.assertChoice(f.employee, tariffs.SourceTokenGroup, employee, "the group of the token does not need a connector")

	f.setLocationDefault(nil)
	f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, in, "a location without a default")

	unknown := in
	unknown.StationKey = "k7f2.UNKNOWN"
	f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, unknown, "a station that is not registered")
}

// An EVSE can be at another location than its charge station: the default tariff is the
// one of the location of the EVSE.
func TestSelectTheLocationOfTheEvse(t *testing.T) {
	ctx := context.Background()
	f := newSelectFixture(t)
	f.setLocationDefault(&f.atLocation.Id)
	elsewhere := &store.AssetLocation{
		OrgId: selectOrgA, PartyRef: "party-a", OcpiId: "ELSEWHERE", Address: "Gran Via 1", City: "Madrid", Country: "ESP",
		Latitude: "40.420000", Longitude: "-3.705000", TimeZone: "Europe/Madrid", Publish: true, DefaultTariffRef: &f.second.Id,
	}
	require.NoError(t, f.engine.CreateAssetLocation(ctx, elsewhere))
	// the station is registered at the other location, its EVSEs stay where they are
	station, err := f.engine.LookupChargeStation(ctx, selectStation)
	require.NoError(t, err)
	station.LocationId = &elsewhere.Id
	require.NoError(t, f.engine.UpdateChargeStation(ctx, station))

	f.assertChoice(f.atLocation, tariffs.SourceLocation, in16(2, "RFID0001"), "the location of the EVSE")
	f.assertChoice(f.second, tariffs.SourceLocation, in16(9, "RFID0001"), "without the EVSE, the location of the station")
}

// The tariff is a tariff of the org of the transaction: a transaction of another org at
// the same connector does not get the tariffs of the org that owns the connector now.
func TestSelectForTheOrgOfTheTransaction(t *testing.T) {
	f := newSelectFixture(t)
	f.setLocationDefault(&f.atLocation.Id)
	f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id})

	in := in16(1, "EMP00001")
	in.OrgId = selectOrgB
	f.assertChoice(nil, tariffs.SourceNone, in, "the tariffs, the tokens and the groups of the other org are not its own")

	// a backend that does not keep the org of a transaction has no tariff for it
	in.OrgId = ""
	f.assertChoice(nil, tariffs.SourceNone, in)
	f.selector.Fallback = true
	f.assertChoice(nil, tariffs.SourceFallback, in)
}

// A storage backend that does not have one of the stores that the selection reads has no
// tariff for any transaction, and that is not an error.
func TestSelectWithABackendWithoutTariffs(t *testing.T) {
	ctx := context.Background()
	notImplemented := fmt.Errorf("the backend: %w", store.ErrNotImplemented)

	for _, method := range []string{
		"LookupAssetEvseByOcpp", "LookupAssetLocation", "LookupTokenForOrg", "FindTokenGroupForUid", "LookupTariff", "GetTenant",
	} {
		t.Run("without "+method, func(t *testing.T) {
			f := newSelectFixture(t)
			f.setLocationDefault(&f.atLocation.Id)
			f.assertChoice(f.employee, tariffs.SourceTokenGroup, in16(1, "EMP00001"))
			backend := &failing{Store: f.engine, method: method, err: notImplemented}
			selector := &tariffs.Selector{
				Assets: backend, Registry: backend, Tariffs: backend, Groups: backend, Tokens: backend,
				Tenants: &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: backend, Stations: backend}},
			}

			choice, err := selector.Select(ctx, in16(1, "EMP00001"))
			require.NoError(t, err)
			assert.Equal(t, tariffs.Choice{Source: tariffs.SourceNone}, choice)

			selector.Fallback = true
			choice, err = selector.Select(ctx, in16(1, "EMP00001"))
			require.NoError(t, err)
			assert.Equal(t, tariffs.Choice{Source: tariffs.SourceFallback}, choice)
		})
	}

	t.Run("a backend without a station registry still has the tariffs of its connectors", func(t *testing.T) {
		f := newSelectFixture(t)
		backend := &failing{Store: f.engine, method: "LookupChargeStation", err: notImplemented}
		f.selector.Registry = backend
		f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "RFID0001"))
		// a station without the EVSE is then a station without a location
		f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id})
		f.assertChoice(f.ofTenant, tariffs.SourceTenantDefault, in16(9, "RFID0001"))
	})

	t.Run("the first store that is not there ends the selection", func(t *testing.T) {
		f := newSelectFixture(t)
		backend := &failing{Store: f.engine, method: "LookupAssetEvseByOcpp", err: notImplemented}
		f.selector.Assets = backend
		tokens := &failing{Store: f.engine, method: "LookupTokenForOrg", err: errStoreDown}
		f.selector.Tokens = tokens
		f.assertChoice(nil, tariffs.SourceNone, in16(1, "EMP00001"), "the token store is not asked")
	})

	t.Run("without tenant configs", func(t *testing.T) {
		f := newSelectFixture(t)
		f.setConfig(1, map[string]any{"default_tariff_ref": f.ofTenant.Id, "adhoc_emsp_parties": []string{"XX*PAY"}})
		for name, tenants := range map[string]tariffs.TenantConfigs{
			"no source of tenant configs":  nil,
			"a provider that is not there": (*tenancy.ConfigProvider)(nil),
			"single tenant":                &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Stations: f.engine}},
		} {
			f.selector.Tenants = tenants
			f.assertChoice(f.regular, tariffs.SourceConnector, in16(1, "PAY00001"), "%s: no parties of ad hoc payment", name)
			f.assertChoice(f.adhoc, tariffs.SourceConnectorAdhoc, in16(1, "ADHOC001"), name)
			f.assertChoice(nil, tariffs.SourceNone, in16(2, "RFID0001"), "%s: no tenant default", name)
		}
	})
}

// failing is a store of which one lookup fails with err (errStoreDown when not set).
type failing struct {
	*inmemory.Store
	method string
	err    error
}

var errStoreDown = errors.New("the store is down")

// fails reports whether the lookup is the one that fails, and with what.
func (s *failing) fails(method string) (bool, error) {
	if s.method != method {
		return false, nil
	}
	if s.err != nil {
		return true, s.err
	}
	return true, errStoreDown
}

func (s *failing) LookupAssetEvseByOcpp(ctx context.Context, stationKey string, ocppEvseId int) (*store.AssetEvse, error) {
	if failed, err := s.fails("LookupAssetEvseByOcpp"); failed {
		return nil, err
	}
	return s.Store.LookupAssetEvseByOcpp(ctx, stationKey, ocppEvseId)
}

func (s *failing) LookupAssetLocation(ctx context.Context, locationId string) (*store.AssetLocation, error) {
	if failed, err := s.fails("LookupAssetLocation"); failed {
		return nil, err
	}
	return s.Store.LookupAssetLocation(ctx, locationId)
}

func (s *failing) LookupChargeStation(ctx context.Context, stationKey string) (*store.ChargeStation, error) {
	if failed, err := s.fails("LookupChargeStation"); failed {
		return nil, err
	}
	return s.Store.LookupChargeStation(ctx, stationKey)
}

func (s *failing) LookupTokenForOrg(ctx context.Context, orgId, tokenUid string) (*store.Token, error) {
	if failed, err := s.fails("LookupTokenForOrg"); failed {
		return nil, err
	}
	return s.Store.LookupTokenForOrg(ctx, orgId, tokenUid)
}

func (s *failing) FindTokenGroupForUid(ctx context.Context, orgId, uid string) (*store.TokenGroup, error) {
	if failed, err := s.fails("FindTokenGroupForUid"); failed {
		return nil, err
	}
	return s.Store.FindTokenGroupForUid(ctx, orgId, uid)
}

func (s *failing) LookupTariff(ctx context.Context, tariffId string) (*store.Tariff, error) {
	if failed, err := s.fails("LookupTariff"); failed {
		return nil, err
	}
	return s.Store.LookupTariff(ctx, tariffId)
}

func (s *failing) GetTenant(ctx context.Context, orgId string) (*store.Tenant, error) {
	if failed, err := s.fails("GetTenant"); failed {
		return nil, err
	}
	return s.Store.GetTenant(ctx, orgId)
}

// A store that fails gives no tariff at all, not a tariff from the stores that still
// answer: a transaction is not charged with a tariff that a rule further up would have
// replaced.
func TestSelectFailsWhenAStoreFails(t *testing.T) {
	for _, method := range []string{
		"LookupAssetEvseByOcpp", "LookupAssetLocation", "LookupChargeStation", "LookupTokenForOrg", "FindTokenGroupForUid", "LookupTariff", "GetTenant",
	} {
		t.Run(method, func(t *testing.T) {
			f := newSelectFixture(t)
			f.setLocationDefault(&f.atLocation.Id)
			broken := &failing{Store: f.engine, method: method}
			selector := &tariffs.Selector{
				Assets: broken, Registry: broken, Tariffs: broken, Groups: broken, Tokens: broken,
				Tenants: &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: broken, Stations: broken}},
			}
			in := in16(1, "EMP00001")
			if method == "LookupChargeStation" {
				// the registry is only read for a station without the EVSE
				in.StationKey = selectBareStation
			}

			choice, err := selector.Select(context.Background(), in)
			require.ErrorIs(t, err, errStoreDown)
			assert.Equal(t, tariffs.Choice{}, choice)
		})
	}

	t.Run("an OCPP version that is not known", func(t *testing.T) {
		f := newSelectFixture(t)
		in := in16(1, "RFID0001")
		in.OcppVersion = "ocpp3.0"
		_, err := f.selector.Select(context.Background(), in)
		assert.Error(t, err)
	})
}

// A session is ad hoc when its token has the OCPI type AD_HOC_USER, or when the issuer of
// its token is a party that the tenant lists.
func TestAdhocSession(t *testing.T) {
	cfg := &tenancy.TenantConfig{AdhocEmspParties: []string{"XX*PAY", "nl*abc"}}
	for name, tc := range map[string]struct {
		token *store.Token
		cfg   *tenancy.TenantConfig
		want  bool
	}{
		"no token":                              {nil, cfg, false},
		"an RFID token":                         {&store.Token{Type: "RFID", CountryCode: "ES", PartyId: "CPO"}, cfg, false},
		"an ad hoc token":                       {&store.Token{Type: "AD_HOC_USER", CountryCode: "ES", PartyId: "CPO"}, cfg, true},
		"an ad hoc token without a config":      {&store.Token{Type: "AD_HOC_USER"}, nil, true},
		"the type in lower case is not a type":  {&store.Token{Type: "ad_hoc_user", CountryCode: "ES", PartyId: "CPO"}, cfg, false},
		"a token of a listed party":             {&store.Token{Type: "APP_USER", CountryCode: "XX", PartyId: "PAY"}, cfg, true},
		"the party in another case":             {&store.Token{Type: "RFID", CountryCode: "NL", PartyId: "ABC"}, cfg, true},
		"a listed party id of another country":  {&store.Token{Type: "APP_USER", CountryCode: "ES", PartyId: "PAY"}, cfg, false},
		"a listed party without a config":       {&store.Token{Type: "APP_USER", CountryCode: "XX", PartyId: "PAY"}, nil, false},
		"a listed party with an empty config":   {&store.Token{Type: "APP_USER", CountryCode: "XX", PartyId: "PAY"}, &tenancy.TenantConfig{}, false},
		"a token without a party, and no party": {&store.Token{Type: "RFID"}, &tenancy.TenantConfig{AdhocEmspParties: []string{}}, false},
	} {
		assert.Equal(t, tc.want, tariffs.AdhocSession(tc.token, tc.cfg), name)
	}
}
