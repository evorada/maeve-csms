// SPDX-License-Identifier: Apache-2.0

package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

const (
	transferStation = "aaaa.CP001"
	// the tariffs of org A that the connectors of the station refer to
	tariffOfA      = "0193b2c6-0000-7000-8000-00000000aa01"
	adhocTariffOfA = "0193b2c6-0000-7000-8000-00000000aa02"
)

// transferFixture is a control plane that can transfer charge stations, on an in-memory
// store with two tenants: A (org key aaaa, CPO party cpo-a) and B (org key bbbb, CPO party
// cpo-b). The charge station aaaa.CP001 of A is the only one at the location plaza of A,
// with two EVSEs and three connectors, one of them with tariffs, and a charging profile.
type transferFixture struct {
	*fixture
	events *eventstest.Fixture
	plaza  *store.AssetLocation
	evses  []*store.AssetEvse
}

func newTransferFixture(t *testing.T) *transferFixture {
	t.Helper()
	ctx := context.Background()
	f := &transferFixture{fixture: newFixture(t)}
	for _, tenant := range []*store.Tenant{
		{OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example.com"},
			Parties: []store.TenantParty{{Id: "cpo-a", Role: "CPO", CountryCode: "ES", PartyId: "AAA"}}},
		{OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb", Hostnames: []string{"b.example.com"},
			Parties: []store.TenantParty{
				{Id: "emsp-b", Role: "EMSP", CountryCode: "ES", PartyId: "BBE"},
				{Id: "cpo-b", Role: "CPO", CountryCode: "ES", PartyId: "BBB"},
			}},
	} {
		require.NoError(t, f.engine.PutTenant(ctx, tenant))
	}

	f.events = eventstest.New(f.engine, orgA, clock.RealClock{})
	f.server.Stations = f.events.Engine(f.engine)
	f.server.Events = f.events.Events
	f.server.Entitlements = &entitlements.Enforcer{Tenants: f.engine, Counter: f.engine, Mode: tenancy.ModeMulti}
	f.server.Commands = &commands.Service{Store: f.engine, Tx: f.events.Tx, Events: f.events.Events, Clock: clock.RealClock{}}

	require.NoError(t, f.engine.SetChargeStationAuth(ctx, transferStation, &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "c2VjcmV0", OrgId: orgA,
	}))
	f.plaza = f.location(t, orgA, "cpo-a", "PLAZA")
	for ocppEvseId, connectors := range [][]int{{1}, {1, 2}} {
		evse := &store.AssetEvse{OrgId: orgA, LocationId: f.plaza.Id, StationKey: transferStation, OcppEvseId: ocppEvseId + 1}
		for _, id := range connectors {
			evse.Connectors = append(evse.Connectors, &store.AssetConnector{
				OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
			})
		}
		require.NoError(t, f.engine.CreateAssetEvse(ctx, evse))
		f.evses = append(f.evses, evse)
	}
	for _, id := range []string{tariffOfA, adhocTariffOfA} {
		require.NoError(t, f.engine.CreateTariff(ctx,
			&store.Tariff{Id: id, OrgId: orgA, PartyRef: "cpo-a", Name: "Tariff " + id},
			&store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentEnergy, Price: 3000, StepSize: 1},
			}}}, CreatedBy: "user:alice"}))
	}
	connector := f.evses[1].Connectors[1]
	connector.TariffRef = ptr(tariffOfA)
	connector.AdhocTariffRef = ptr(adhocTariffOfA)
	require.NoError(t, f.engine.UpdateAssetConnector(ctx, connector))
	require.NoError(t, f.engine.MoveChargeStationToLocation(ctx, transferStation, f.plaza.Id))
	require.NoError(t, f.engine.SetChargingProfile(ctx, &store.ChargingProfile{
		ChargeStationId: transferStation, ConnectorId: 1, ChargingProfileId: 3, StackLevel: 1,
		ChargingProfilePurpose: store.ChargingProfilePurposeTxDefaultProfile, ChargingProfileKind: store.ChargingProfileKindAbsolute,
		ChargingSchedule: store.ChargingSchedule{ChargingRateUnit: store.ChargingRateUnitA},
	}))
	// the events of the setup are not those of the tests
	f.events.Take(t)
	return f
}

func ptr[T any](v T) *T {
	return &v
}

// location creates a location of the org and party.
func (f *transferFixture) location(t *testing.T, orgId, partyRef, ocpiId string) *store.AssetLocation {
	t.Helper()
	loc := &store.AssetLocation{
		OrgId: orgId, PartyRef: partyRef, OcpiId: ocpiId, Name: ptr(ocpiId), Address: "Calle Mayor 1", City: "Madrid",
		Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, f.engine.CreateAssetLocation(context.Background(), loc))
	return loc
}

// mover returns the token of a platform service that may transfer charge stations.
func (f *transferFixture) mover() string {
	return f.token("svc:mover", tenancy.PermTenantManage, tenancy.PermStationWrite)
}

// stationURN is the URN of a charge station of the org.
func stationURN(orgId, stationKey string) string {
	return "urn:cpms:" + orgId + ":station/" + stationKey
}

// transferPath is the path of the transfer of the resource, with the URN encoded as the
// spec says: all of it.
func transferPath(urn string) string {
	return "/resources/" + strings.ReplaceAll(url.PathEscape(urn), ":", "%3A") + ":transfer"
}

func (f *transferFixture) transfer(urn, body string) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, transferPath(urn), f.mover(), body)
}

func (f *transferFixture) station(t *testing.T, stationKey string) *store.ChargeStation {
	t.Helper()
	cs, err := f.engine.LookupChargeStation(context.Background(), stationKey)
	require.NoError(t, err)
	require.NotNil(t, cs)
	return cs
}

func (f *transferFixture) assetLocation(t *testing.T, id string) *store.AssetLocation {
	t.Helper()
	loc, err := f.engine.LookupAssetLocation(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, loc)
	return loc
}

func (f *transferFixture) chargingProfileOrg(t *testing.T) string {
	t.Helper()
	profiles, err := f.engine.GetChargingProfiles(context.Background(), transferStation, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	return profiles[0].OrgId
}

// assertNotTransferred checks that the charge station of the fixture is in org A and
// that no event was stored.
func (f *transferFixture) assertNotTransferred(t *testing.T) {
	t.Helper()
	cs := f.station(t, transferStation)
	assert.Equal(t, orgA, cs.OrgId)
	assert.Equal(t, orgA, f.assetLocation(t, f.plaza.Id).OrgId)
	assert.Equal(t, orgA, f.chargingProfileOrg(t))
	assert.Empty(t, f.events.Take(t))
}

func TestTransferMovesTheStationWithTheLocationThatOnlyItUses(t *testing.T) {
	f := newTransferFixture(t)
	ctx := context.Background()

	rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","reason":"sold"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{
		"urn": "urn:cpms:`+orgA+`:station/aaaa.CP001",
		"new_urn": "urn:cpms:`+orgB+`:station/aaaa.CP001",
		"from_org_id": "`+orgA+`",
		"to_org_id": "`+orgB+`",
		"location_id": "`+f.plaza.Id+`",
		"location_moved": true,
		"evses_moved": 2,
		"connectors_moved": 3,
		"cleared_tariff_refs": ["`+tariffOfA+`", "`+adhocTariffOfA+`"]
	}`, rr.Body.String())

	cs := f.station(t, transferStation)
	assert.Equal(t, orgB, cs.OrgId)
	assert.Equal(t, &f.plaza.Id, cs.LocationId)
	plaza := f.assetLocation(t, f.plaza.Id)
	assert.Equal(t, orgB, plaza.OrgId)
	assert.Equal(t, "cpo-b", plaza.PartyRef, "the only CPO party of B publishes the location")
	for _, before := range f.evses {
		evse, err := f.engine.LookupAssetEvse(ctx, before.Id)
		require.NoError(t, err)
		assert.Equal(t, orgB, evse.OrgId)
		for _, c := range evse.Connectors {
			assert.Equal(t, orgB, c.OrgId)
			assert.Nil(t, c.TariffRef)
			assert.Nil(t, c.AdhocTariffRef)
		}
	}
	assert.Equal(t, orgB, f.chargingProfileOrg(t))
	// the station connects as before
	auth, err := f.engine.LookupChargeStationAuth(ctx, transferStation)
	require.NoError(t, err)
	assert.Equal(t, "c2VjcmV0", auth.Base64SHA256Password)
	assert.Equal(t, store.TLSWithBasicAuth, auth.SecurityProfile)

	stored := f.events.Take(t)
	require.Equal(t, []string{
		events.TypeStationTransferred, events.TypeStationTransferred, events.TypeLocationUpdated, events.TypeLocationUpdated,
	}, eventstest.Types(stored))
	assert.Equal(t, orgA, stored[0].OrgId)
	assert.Equal(t, "urn:cpms:"+orgA+":station/aaaa.CP001", stored[0].Subject)
	assert.Equal(t, orgB, stored[1].OrgId)
	assert.Equal(t, "urn:cpms:"+orgB+":station/aaaa.CP001", stored[1].Subject)
	for _, ev := range stored[:2] {
		assert.Equal(t, "svc:mover", ev.Actor)
		assert.JSONEq(t, `{
			"station_key": "aaaa.CP001", "station_id": "CP001", "from_org_id": "`+orgA+`", "to_org_id": "`+orgB+`",
			"location_id": "`+f.plaza.Id+`", "location_moved": true, "evses_moved": 2, "connectors_moved": 3,
			"cleared_tariff_refs": ["`+tariffOfA+`", "`+adhocTariffOfA+`"], "actor": "svc:mover", "reason": "sold"
		}`, string(ev.Data))
	}
	// the location leaves the feed of the party of A and joins the one of the party of B
	for i, want := range []struct{ orgId, partyRef string }{{orgA, "cpo-a"}, {orgB, "cpo-b"}} {
		ev := stored[2+i]
		assert.Equal(t, want.orgId, ev.OrgId)
		assert.Equal(t, "urn:cpms:"+want.orgId+":location/"+f.plaza.Id, ev.Subject)
		data := eventstest.Data[events.LocationUpdatedData](t, ev)
		assert.Equal(t, events.LocationUpdatedData{LocationId: f.plaza.Id, OcpiId: "PLAZA", PartyRef: want.partyRef,
			Change: events.LocationChangeTransferred, Publish: true}, data)
	}

	t.Run("the station is no longer in the old org", func(t *testing.T) {
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.JSONEq(t, `{"status":"Not Found","error":"not_found"}`, rr.Body.String())

		// and goes back with its location, to the party that is named
		rr = f.transfer(stationURN(orgB, transferStation), `{"to_org_id":"`+orgA+`","move_location":"always","target_party_ref":"cpo-a"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, orgA, f.station(t, transferStation).OrgId)
		assert.Equal(t, "cpo-a", f.assetLocation(t, f.plaza.Id).PartyRef)
	})
}

func TestTransferOfAStationAtALocationThatIsShared(t *testing.T) {
	f := newTransferFixture(t)
	ctx := context.Background()
	require.NoError(t, f.engine.SetChargeStationAuth(ctx, "aaaa.CP002", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	require.NoError(t, f.engine.MoveChargeStationToLocation(ctx, "aaaa.CP002", f.plaza.Id))
	ofB := f.location(t, orgB, "cpo-b", "DEPOT")
	otherOfA := f.location(t, orgA, "cpo-a", "GARAGE")

	t.Run("needs a location of the target", func(t *testing.T) {
		for _, body := range []string{`{"to_org_id":"` + orgB + `"}`, `{"to_org_id":"` + orgB + `","move_location":"auto"}`} {
			rr := f.transfer(stationURN(orgA, transferStation), body)
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Unprocessable Entity","error":"invalid_field","field":"target_location_id",
				"message":"is required: the location of the charge station is shared with other charge stations"}`, rr.Body.String())
		}
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","move_location":"never"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"field":"target_location_id"`)
		f.assertNotTransferred(t)
	})

	t.Run("cannot take the location along", func(t *testing.T) {
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","move_location":"always"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"location_shared","field":"move_location",
			"message":"the location of the charge station is shared with other charge stations"}`, rr.Body.String())
		f.assertNotTransferred(t)
	})

	t.Run("a location of another org than the target is not a target", func(t *testing.T) {
		for _, target := range []string{otherOfA.Id, "0193b2c6-0000-7000-8000-0000000000ff"} {
			rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","target_location_id":"`+target+`"}`)
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Unprocessable Entity","error":"invalid_field","field":"target_location_id",
				"message":"must be a location of the org to_org_id"}`, rr.Body.String())
		}
		f.assertNotTransferred(t)
	})

	t.Run("goes to a location of the target, and the location stays", func(t *testing.T) {
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","move_location":"never","target_location_id":"`+ofB.Id+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var result struct {
			LocationId    string `json:"location_id"`
			LocationMoved bool   `json:"location_moved"`
			EvsesMoved    int    `json:"evses_moved"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
		assert.Equal(t, ofB.Id, result.LocationId)
		assert.False(t, result.LocationMoved)
		assert.Equal(t, 2, result.EvsesMoved)

		cs := f.station(t, transferStation)
		assert.Equal(t, orgB, cs.OrgId)
		assert.Equal(t, &ofB.Id, cs.LocationId)
		plaza := f.assetLocation(t, f.plaza.Id)
		assert.Equal(t, orgA, plaza.OrgId)
		assert.Equal(t, "cpo-a", plaza.PartyRef)
		moved, err := f.engine.ListAssetEvsesForLocation(ctx, ofB.Id)
		require.NoError(t, err)
		assert.Len(t, moved, 2)
		assert.Equal(t, orgA, f.station(t, "aaaa.CP002").OrgId)

		stored := f.events.Take(t)
		require.Equal(t, []string{
			events.TypeStationTransferred, events.TypeStationTransferred, events.TypeLocationUpdated, events.TypeLocationUpdated,
		}, eventstest.Types(stored))
		// the location of A loses the EVSEs, the one of B gets them
		assert.Equal(t, orgA, stored[2].OrgId)
		assert.Equal(t, events.LocationUpdatedData{LocationId: f.plaza.Id, OcpiId: "PLAZA", PartyRef: "cpo-a",
			Change: events.LocationChangeUpdated, Publish: true}, eventstest.Data[events.LocationUpdatedData](t, stored[2]))
		assert.Equal(t, orgB, stored[3].OrgId)
		assert.Equal(t, events.LocationUpdatedData{LocationId: ofB.Id, OcpiId: "DEPOT", PartyRef: "cpo-b",
			Change: events.LocationChangeUpdated, Publish: true}, eventstest.Data[events.LocationUpdatedData](t, stored[3]))
	})
}

func TestTransferOfALocationNeedsAPartyOfTheTarget(t *testing.T) {
	f := newTransferFixture(t)
	ctx := context.Background()
	two := []store.TenantParty{
		{Id: "cpo-b", Role: "CPO", CountryCode: "ES", PartyId: "BBB"},
		{Id: "cpo-b2", Role: "CPO", CountryCode: "ES", PartyId: "BB2"},
	}
	require.NoError(t, f.engine.PutTenant(ctx, &store.Tenant{OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb",
		Hostnames: []string{"b.example.com"}, Parties: two}))

	rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"status":"Unprocessable Entity","error":"target_party_required","field":"target_party_ref",
		"message":"is required: the location moves and the tenant to_org_id has no single CPO party"}`, rr.Body.String())
	for _, party := range []string{"cpo-a", "emsp-b", "unknown"} {
		rr = f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","target_party_ref":"`+party+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Unprocessable Entity","error":"invalid_field","field":"target_party_ref",
			"message":"must be a CPO party of the tenant to_org_id"}`, rr.Body.String())
	}
	f.assertNotTransferred(t)

	rr = f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","target_party_ref":"cpo-b2"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "cpo-b2", f.assetLocation(t, f.plaza.Id).PartyRef)

	t.Run("whose OCPI id is free", func(t *testing.T) {
		f := newTransferFixture(t)
		f.location(t, orgB, "cpo-b", "PLAZA")
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"error":"conflict"`)
		f.assertNotTransferred(t)
	})
}

func TestTransferWhileATransactionIsRunning(t *testing.T) {
	f := newTransferFixture(t)
	ctx := context.Background()
	require.NoError(t, f.engine.CreateTransaction(ctx, transferStation, "41", "TAG1", "ISO14443", nil, 0, false))

	for _, body := range []string{`{"to_org_id":"` + orgB + `"}`, `{"to_org_id":"` + orgB + `","force":false}`} {
		rr := f.transfer(stationURN(orgA, transferStation), body)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"active_transaction",
			"message":"a transaction is running on the charge station; force transfers it all the same, and the session keeps the old org"}`, rr.Body.String())
	}
	f.assertNotTransferred(t)

	t.Run("a current transaction of a connector", func(t *testing.T) {
		f := newTransferFixture(t)
		require.NoError(t, f.engine.SetConnectorStatus(ctx, transferStation, 1, &store.ConnectorStatus{
			ConnectorId: 1, Status: store.ConnectorStatusCharging, CurrentTransactionId: ptr("42"),
		}))
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"error":"active_transaction"`)
	})

	t.Run("force transfers it, and the session keeps the old org", func(t *testing.T) {
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`","force":true}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, orgB, f.station(t, transferStation).OrgId)
		transaction, err := f.engine.FindTransaction(ctx, transferStation, "41")
		require.NoError(t, err)
		require.NotNil(t, transaction)
		assert.Equal(t, orgA, transaction.OrgId)
		assert.Zero(t, transaction.EndedSeqNo, "the session goes on")
	})

	t.Run("an ended transaction does not count", func(t *testing.T) {
		f := newTransferFixture(t)
		require.NoError(t, f.engine.CreateTransaction(ctx, transferStation, "40", "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, f.engine.EndTransaction(ctx, transferStation, "40", "TAG1", "ISO14443", nil, 1))
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		transaction, err := f.engine.FindTransaction(ctx, transferStation, "40")
		require.NoError(t, err)
		assert.Equal(t, orgA, transaction.OrgId, "the history keeps the old org")
	})
}

func TestTransferToATargetThatCannotTakeTheStation(t *testing.T) {
	ctx := context.Background()
	const orgC = "0193b2c4-0000-7000-8000-0000000000c3"

	t.Run("a target that is not an active tenant", func(t *testing.T) {
		f := newTransferFixture(t)
		_, err := f.engine.SetTenantStatus(ctx, orgB, store.TenantStatusSuspended)
		require.NoError(t, err)
		for _, target := range []string{orgB, orgC} {
			rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+target+`"}`)
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Conflict","error":"target_not_active","field":"to_org_id","message":"must be a tenant that is active"}`, rr.Body.String())
		}
		_, err = f.engine.SetTenantStatus(ctx, orgB, store.TenantStatusCancelled)
		require.NoError(t, err)
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		assert.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		f.assertNotTransferred(t)
	})

	t.Run("the org of the station", func(t *testing.T) {
		f := newTransferFixture(t)
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+strings.ToUpper(orgA)+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Unprocessable Entity","error":"invalid_field","field":"to_org_id","message":"must not be the org of the charge station"}`, rr.Body.String())
		f.assertNotTransferred(t)
	})

	t.Run("a target whose connector limit is used up", func(t *testing.T) {
		f := newTransferFixture(t)
		limit := func(value int) {
			t.Helper()
			require.NoError(t, f.engine.PutTenant(ctx, &store.Tenant{OrgId: orgB, Path: store.OrgPathLabel(orgB), Slug: "tenant-b", OrgKey: "bbbb",
				Hostnames: []string{"b.example.com"}, Parties: []store.TenantParty{{Id: "cpo-b", Role: "CPO", CountryCode: "ES", PartyId: "BBB"}},
				EntitlementsVersion: int64(value),
				Entitlements:        json.RawMessage(`{"version":` + strconv.Itoa(value) + `,"limits":{"cpms.connectors.max":{"value":` + strconv.Itoa(value) + `,"enforcement":"hard","scope":"org"}}}`),
			}))
		}
		// B has a station with two connectors
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, "bbbb.CP001", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgB}))
		for _, id := range []int{1, 2} {
			require.NoError(t, f.engine.SetConnectorStatus(ctx, "bbbb.CP001", id, &store.ConnectorStatus{ConnectorId: id, Status: store.ConnectorStatusAvailable}))
		}

		limit(4)
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"entitlement_exceeded","field":"to_org_id",
			"message":"entitlement_limit_exceeded: cpms.connectors.max (limit 4, current 2, requested 3)"}`, rr.Body.String())
		f.assertNotTransferred(t)

		// a station that reports more connectors than its EVSEs have brings those
		for _, id := range []int{1, 2, 3, 4} {
			require.NoError(t, f.engine.SetConnectorStatus(ctx, transferStation, id, &store.ConnectorStatus{ConnectorId: id, Status: store.ConnectorStatusAvailable}))
		}
		limit(5)
		rr = f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "requested 4")

		limit(6)
		rr = f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})
}

func TestTransferOfAStationThatIsNotActive(t *testing.T) {
	ctx := context.Background()

	t.Run("decommissioned", func(t *testing.T) {
		f := newTransferFixture(t)
		require.NoError(t, f.engine.DecommissionChargeStation(ctx, transferStation, nil))
		rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"invalid_state","message":"the charge station is decommissioned"}`, rr.Body.String())
	})

	t.Run("pending", func(t *testing.T) {
		f := newTransferFixture(t)
		_, err := f.engine.RecordOnboardingConnection(ctx, "aaaa.NEW01", orgA, store.OnboardingTransportWss,
			store.OnboardingCredentialOnboardingPassword, 50, time.Now())
		require.NoError(t, err)

		rr := f.transfer(stationURN(orgA, "aaaa.NEW01"), `{"to_org_id":"`+orgB+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
		assert.Nil(t, result["location_id"], "a pending station has no location")
		cs := f.station(t, "aaaa.NEW01")
		assert.Equal(t, orgB, cs.OrgId)
		assert.Equal(t, store.StationStatePending, cs.State, "it is approved in its new org")
	})

	t.Run("pending with an approval that is not finished", func(t *testing.T) {
		f := newTransferFixture(t)
		_, err := f.engine.RecordOnboardingConnection(ctx, "aaaa.NEW01", orgA, store.OnboardingTransportWss,
			store.OnboardingCredentialOnboardingPassword, 50, time.Now())
		require.NoError(t, err)
		cs := f.station(t, "aaaa.NEW01")
		for _, status := range []store.OnboardingStatus{store.OnboardingApproving, store.OnboardingCredentialsFailed} {
			onboarding := *cs.Onboarding
			onboarding.Status = status
			onboarding.CommandId = ptr("0193b2c6-0000-7000-8000-0000000c0de1")
			onboarding.Approval = &store.PendingApproval{LocationId: f.plaza.Id, RequestedBy: "user:alice", RequestedAt: time.Now()}
			require.NoError(t, f.engine.SetOnboarding(ctx, "aaaa.NEW01", &onboarding, ptr("cGVuZGluZw==")))

			rr := f.transfer(stationURN(orgA, "aaaa.NEW01"), `{"to_org_id":"`+orgB+`"}`)
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Conflict","error":"invalid_state",
				"message":"the approval of the pending charge station is not finished: approve it again or reject it first"}`, rr.Body.String())
			assert.Equal(t, orgA, f.station(t, "aaaa.NEW01").OrgId)
		}
	})
}

func TestTransferCancelsTheQueuedCommandsOfTheStation(t *testing.T) {
	f := newTransferFixture(t)
	ctx := context.Background()
	now := time.Now()
	command := func(id, stationKey string) {
		t.Helper()
		require.NoError(t, f.engine.CreateCommand(ctx, &store.Command{
			Id: id, OrgId: orgA, StationKey: stationKey, OcppVersion: "ocpp1.6", Action: "Reset",
			Payload: json.RawMessage(`{"type":"Soft"}`), Status: store.CommandStatusQueued, Actor: ptr("user:alice"),
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}))
	}
	command("0193b2c6-0000-7000-8000-0000000c0d01", transferStation)
	command("0193b2c6-0000-7000-8000-0000000c0d02", transferStation)
	command("0193b2c6-0000-7000-8000-0000000c0d03", "aaaa.CP002")

	rr := f.transfer(stationURN(orgA, transferStation), `{"to_org_id":"`+orgB+`"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	for id, status := range map[string]store.CommandStatus{
		"0193b2c6-0000-7000-8000-0000000c0d01": store.CommandStatusCancelled,
		"0193b2c6-0000-7000-8000-0000000c0d02": store.CommandStatusCancelled,
		"0193b2c6-0000-7000-8000-0000000c0d03": store.CommandStatusQueued,
	} {
		cmd, err := f.engine.GetCommand(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, status, cmd.Status, id)
	}
	stored := f.events.Take(t)
	var completed []*store.OutboxEvent
	for _, ev := range stored {
		if ev.Type == events.TypeCommandCompleted {
			completed = append(completed, ev)
		}
	}
	require.Len(t, completed, 2)
	for _, ev := range completed {
		assert.Equal(t, orgA, ev.OrgId, "the commands were asked for by the old org")
		assert.Equal(t, "cancelled", eventstest.Data[events.CommandCompletedData](t, ev).Status)
	}
}

func TestTransferRouting(t *testing.T) {
	urn := stationURN(orgA, transferStation)
	body := `{"to_org_id":"` + orgB + `"}`

	t.Run("a URN that is encoded as a whole reaches the transfer", func(t *testing.T) {
		f := newTransferFixture(t)
		rr := f.do(http.MethodPost, transferPath(urn), f.mover(), body)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("a URN whose colons are not encoded too", func(t *testing.T) {
		f := newTransferFixture(t)
		target := "/resources/" + url.PathEscape(urn) + ":transfer"
		require.Contains(t, target, "urn:cpms:")
		rr := f.do(http.MethodPost, target, f.mover(), body)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("another action", func(t *testing.T) {
		f := newTransferFixture(t)
		for _, target := range []string{
			strings.TrimSuffix(transferPath(urn), ":transfer") + ":foo",
			"/resources/" + url.PathEscape(urn) + ":foo",
			strings.TrimSuffix(transferPath(urn), ":transfer"),
			"/resources/" + url.PathEscape(urn) + "%3Atransfer",
			"/resources/",
		} {
			rr := f.do(http.MethodPost, target, f.mover(), body)
			assert.Equal(t, http.StatusNotFound, rr.Code, target)
			assert.JSONEq(t, `{"status":"Not Found","error":"not_found"}`, rr.Body.String(), target)
		}
		rr := f.do(http.MethodGet, transferPath(urn), f.mover(), "")
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
		f.assertNotTransferred(t)
	})

	t.Run("the URN", func(t *testing.T) {
		f := newTransferFixture(t)
		for target, want := range map[string]string{
			transferPath("urn:ocpi:" + orgA + ":station/" + transferStation):      `{"status":"Bad Request","error":"invalid_field","field":"urn","message":"must be a URN of the form urn:cpms:<org_id>:<type>/<id>"}`,
			transferPath("urn:cpms:" + orgA + ":" + transferStation):              `{"status":"Bad Request","error":"invalid_field","field":"urn","message":"must be a URN of the form urn:cpms:<org_id>:<type>/<id>"}`,
			transferPath("urn:cpms:" + orgA + ":location/" + f.plaza.Id):          `{"status":"Unprocessable Entity","error":"unsupported_resource_type","field":"urn","message":"only a charge station (station) can be transferred"}`,
			transferPath(stationURN(orgB, transferStation)):                       `{"status":"Not Found","error":"not_found"}`,
			transferPath(stationURN(orgA, "aaaa.UNKNOWN")):                        `{"status":"Not Found","error":"not_found"}`,
			transferPath(stationURN("0193b2c4-0000-7000-8000-0000000000c3", "x")): `{"status":"Not Found","error":"not_found"}`,
		} {
			rr := f.do(http.MethodPost, target, f.mover(), body)
			assert.JSONEq(t, want, rr.Body.String(), target)
		}
		f.assertNotTransferred(t)

		// the org of the URN in upper case
		rr := f.do(http.MethodPost, transferPath(stationURN(strings.ToUpper(orgA), transferStation)), f.mover(), body)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("the body", func(t *testing.T) {
		f := newTransferFixture(t)
		for body, want := range map[string]string{
			``:                     `{"status":"Bad Request","error":"invalid_field","field":"body","message":"must be a JSON document"}`,
			`[]`:                   `{"status":"Bad Request","error":"invalid_field","field":"body","message":"has the wrong JSON type"}`,
			`{}`:                   `{"status":"Bad Request","error":"invalid_field","field":"to_org_id","message":"must be a UUID"}`,
			`{"to_org_id":"acme"}`: `{"status":"Bad Request","error":"invalid_field","field":"to_org_id","message":"must be a UUID"}`,
			`{"to_org_id":1}`:      `{"status":"Bad Request","error":"invalid_field","field":"to_org_id","message":"has the wrong JSON type"}`,
			`{"to_org_id":"` + orgB + `","move_location":"sometimes"}`:                                        `{"status":"Bad Request","error":"invalid_field","field":"move_location","message":"must be auto, always or never"}`,
			`{"to_org_id":"` + orgB + `","target_location_id":"plaza"}`:                                       `{"status":"Bad Request","error":"invalid_field","field":"target_location_id","message":"must be a UUID"}`,
			`{"to_org_id":"` + orgB + `","move_location":"always","target_location_id":"` + f.plaza.Id + `"}`: `{"status":"Bad Request","error":"invalid_field","field":"target_location_id","message":"must not be given when the location moves (move_location always)"}`,
			`{"to_org_id":"` + orgB + `","target_party_ref":""}`:                                              `{"status":"Bad Request","error":"invalid_field","field":"target_party_ref","message":"must be 1 to 64 characters, without control characters"}`,
			`{"to_org_id":"` + orgB + `","reason":"` + strings.Repeat("x", 256) + `"}`:                        `{"status":"Bad Request","error":"invalid_field","field":"reason","message":"must be at most 255 characters"}`,
			`{"to_org_id":"` + orgB + `","force":"yes"}`:                                                      `{"status":"Bad Request","error":"invalid_field","field":"force","message":"has the wrong JSON type"}`,
		} {
			rr := f.transfer(urn, body)
			assert.JSONEq(t, want, rr.Body.String(), body)
		}
		rr := f.transfer(urn, `{"to_org_id":"`+orgB+`","reason":"`+strings.Repeat("x", 70<<10)+`"}`)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
		f.assertNotTransferred(t)
	})

	t.Run("the caller", func(t *testing.T) {
		f := newTransferFixture(t)
		for name, token := range map[string]string{
			"a service that may manage tenants": f.provisioner(),
			"a service that may write stations": f.token("svc:mover", tenancy.PermStationWrite),
			"a person with both permissions":    f.token("user:alice", tenancy.PermTenantManage, tenancy.PermStationWrite),
		} {
			rr := f.do(http.MethodPost, transferPath(urn), token, body)
			assert.Equal(t, http.StatusForbidden, rr.Code, name)
			assert.JSONEq(t, `{"status":"Forbidden","error":"forbidden"}`, rr.Body.String(), name)
		}
		rr := f.do(http.MethodPost, transferPath(urn), "", body)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		f.assertNotTransferred(t)
	})
}

// A storage engine that keeps no charge stations, or no events.
func TestTransferWithoutTheStorageItNeeds(t *testing.T) {
	urn := stationURN(orgA, transferStation)
	body := `{"to_org_id":"` + orgB + `"}`

	f := newTransferFixture(t)
	f.server.Stations = nil
	rr := f.transfer(urn, body)
	assert.Equal(t, http.StatusNotImplemented, rr.Code)
	assert.JSONEq(t, `{"status":"Not Implemented","error":"not_implemented"}`, rr.Body.String())

	f = newTransferFixture(t)
	f.server.Stations = notImplementedStations{f.events.Engine(f.engine)}
	rr = f.transfer(urn, body)
	assert.Equal(t, http.StatusNotImplemented, rr.Code, rr.Body.String())

	f = newTransferFixture(t)
	f.server.Events = nil
	rr = f.transfer(urn, body)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error","error":"internal_error"}`, rr.Body.String())
	f.assertNotTransferred(t)

	f = newTransferFixture(t)
	f.events.Events.Err = errors.New("the outbox is full")
	rr = f.transfer(urn, body)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.JSONEq(t, `{"status":"Internal Server Error","error":"internal_error"}`, rr.Body.String(), "the reason is not shown")
	assert.Empty(t, f.events.Take(t))
}

// notImplementedStations is a storage engine that cannot transfer charge stations.
type notImplementedStations struct {
	store.Engine
}

func (notImplementedStations) TransferChargeStation(context.Context, store.TransferParams) (*store.TransferResult, error) {
	return nil, store.ErrNotImplemented
}
