// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// The tests in this file check that a transfer of a charge station through the control
// plane changes the station, its assets, its charging profiles, its queued commands and
// the events in one PostgreSQL transaction.

const (
	transferFromOrg = "0193b2c4-0000-7000-8000-0000000000a1"
	transferToOrg   = "0193b2c4-0000-7000-8000-0000000000b2"
	transferKey     = "aaaa.CP001"
	transferCommand = "0193b2c6-0000-7000-8000-0000000c0d01"
	transferTariff  = "0193b2c6-0000-7000-8000-00000000aa01"
)

// transferApi is the control plane on the PostgreSQL store, called by a platform service
// that may transfer charge stations.
func transferApi(fixture *eventstest.Fixture) http.Handler {
	handler := controlplane.NewHandler(&controlplane.Server{
		Tenants:  testStore,
		Mode:     tenancy.ModeMulti,
		Stations: fixture.Engine(testStore),
		Events:   fixture.Events,
		Commands: &commands.Service{Store: testStore, Tx: fixture.Tx, Events: fixture.Events, Clock: clock.RealClock{}},
	})
	caller := &tenancy.Principal{Subject: "svc:mover", Perms: []string{tenancy.PermTenantManage, tenancy.PermStationWrite}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(tenancy.WithPrincipal(r.Context(), caller)))
	})
}

func callTransfer(handler http.Handler, body string) *httptest.ResponseRecorder {
	urn := strings.ReplaceAll(url.PathEscape("urn:cpms:"+transferFromOrg+":station/"+transferKey), ":", "%3A")
	req := httptest.NewRequest(http.MethodPost, "/resources/"+urn+":transfer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// transferSetup stores the two tenants and the charge station of the first one, alone
// at its location, with an EVSE whose connector has a tariff, a charging profile and a
// queued command. It returns the location and the EVSE.
func transferSetup(t *testing.T) (*store.AssetLocation, *store.AssetEvse) {
	t.Helper()
	ctx := context.Background()
	for orgId, orgKey := range map[string]string{transferFromOrg: "aaaa", transferToOrg: "bbbb"} {
		require.NoError(t, testStore.PutTenant(ctx, &store.Tenant{
			OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey, Hostnames: []string{orgKey + ".example.com"},
			Parties: []store.TenantParty{{Id: "cpo-" + orgKey, Role: "CPO", CountryCode: "ES", PartyId: strings.ToUpper(orgKey[:3])}},
		}))
	}
	require.NoError(t, testStore.SetChargeStationAuth(ctx, transferKey, &store.ChargeStationAuth{
		SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "c2VjcmV0", OrgId: transferFromOrg,
	}))
	loc := &store.AssetLocation{OrgId: transferFromOrg, PartyRef: "cpo-aaaa", OcpiId: "PLAZA", Address: "Calle Mayor 1", City: "Madrid",
		Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true}
	require.NoError(t, testStore.CreateAssetLocation(ctx, loc))
	evse := &store.AssetEvse{OrgId: transferFromOrg, LocationId: loc.Id, StationKey: transferKey, OcppEvseId: 1,
		Connectors: []*store.AssetConnector{{OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE",
			MaxVoltage: 400, MaxAmperage: 32, TariffRef: ptr(transferTariff)}}}
	require.NoError(t, testStore.CreateAssetEvse(ctx, evse))
	require.NoError(t, testStore.MoveChargeStationToLocation(ctx, transferKey, loc.Id))
	require.NoError(t, testStore.SetChargingProfile(ctx, &store.ChargingProfile{
		ChargeStationId: transferKey, ConnectorId: 1, ChargingProfileId: 3,
		ChargingProfilePurpose: store.ChargingProfilePurposeTxDefaultProfile, ChargingProfileKind: store.ChargingProfileKindAbsolute,
		ChargingSchedule: store.ChargingSchedule{ChargingRateUnit: store.ChargingRateUnitA,
			ChargingSchedulePeriod: []store.ChargingSchedulePeriod{{StartPeriod: 0, Limit: 16}}},
	}))
	now := time.Now()
	require.NoError(t, testStore.CreateCommand(ctx, &store.Command{
		Id: transferCommand, OrgId: transferFromOrg, StationKey: transferKey, OcppVersion: "ocpp1.6", Action: "Reset",
		Payload: json.RawMessage(`{"type":"Soft"}`), Status: store.CommandStatusQueued, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	return loc, evse
}

func TestTransfer_IsOneTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	loc, evse := transferSetup(t)

	profileOrg := func(t *testing.T) string {
		t.Helper()
		profiles, err := testStore.GetChargingProfiles(ctx, transferKey, nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, profiles, 1)
		return profiles[0].OrgId
	}
	commandStatus := func(t *testing.T) store.CommandStatus {
		t.Helper()
		cmd, err := testStore.GetCommand(ctx, transferCommand)
		require.NoError(t, err)
		return cmd.Status
	}

	t.Run("a transfer whose events cannot be stored changes nothing", func(t *testing.T) {
		before := outboxRows(t)
		failing := eventstest.New(testStore, transferFromOrg, clock.RealClock{})
		failing.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{testStore}, Product: events.Product}
		rr := callTransfer(transferApi(failing), `{"to_org_id":"`+transferToOrg+`"}`)
		require.Equal(t, http.StatusInternalServerError, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)

		cs, err := testStore.LookupChargeStation(ctx, transferKey)
		require.NoError(t, err)
		assert.Equal(t, transferFromOrg, cs.OrgId)
		storedLoc, err := testStore.LookupAssetLocation(ctx, loc.Id)
		require.NoError(t, err)
		assert.Equal(t, loc, storedLoc)
		storedEvse, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		assert.Equal(t, evse, storedEvse, "the EVSE keeps its org, version and the tariff of its connector")
		assert.Equal(t, transferFromOrg, profileOrg(t))
		assert.Equal(t, store.CommandStatusQueued, commandStatus(t))
		assert.Equal(t, before, outboxRows(t))
	})

	t.Run("a refused transfer leaves no transaction open", func(t *testing.T) {
		fixture := eventstest.New(testStore, transferFromOrg, clock.RealClock{})
		rr := callTransfer(transferApi(fixture), `{"to_org_id":"`+transferFromOrg+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		rr = callTransfer(transferApi(fixture), `{"to_org_id":"0193b2c4-0000-7000-8000-0000000000c3"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)
		assert.Empty(t, fixture.Take(t))
	})

	t.Run("a charge station moves with its location, its EVSE and its profiles", func(t *testing.T) {
		fixture := eventstest.New(testStore, transferFromOrg, clock.RealClock{})
		rr := callTransfer(transferApi(fixture), `{"to_org_id":"`+transferToOrg+`","reason":"sold"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		requireNoOpenTransaction(t)
		assert.JSONEq(t, `{"urn":"urn:cpms:`+transferFromOrg+`:station/aaaa.CP001","new_urn":"urn:cpms:`+transferToOrg+`:station/aaaa.CP001",
			"from_org_id":"`+transferFromOrg+`","to_org_id":"`+transferToOrg+`","location_id":"`+loc.Id+`","location_moved":true,
			"evses_moved":1,"connectors_moved":1,"cleared_tariff_refs":["`+transferTariff+`"]}`, rr.Body.String())

		cs, err := testStore.LookupChargeStation(ctx, transferKey)
		require.NoError(t, err)
		assert.Equal(t, transferToOrg, cs.OrgId)
		storedLoc, err := testStore.LookupAssetLocation(ctx, loc.Id)
		require.NoError(t, err)
		assert.Equal(t, transferToOrg, storedLoc.OrgId)
		assert.Equal(t, "cpo-bbbb", storedLoc.PartyRef)
		storedEvse, err := testStore.LookupAssetEvse(ctx, evse.Id)
		require.NoError(t, err)
		assert.Equal(t, transferToOrg, storedEvse.OrgId)
		assert.Equal(t, transferToOrg, storedEvse.Connectors[0].OrgId)
		assert.Nil(t, storedEvse.Connectors[0].TariffRef)
		assert.Equal(t, transferToOrg, profileOrg(t))
		assert.Equal(t, store.CommandStatusCancelled, commandStatus(t))
		auth, err := testStore.LookupChargeStationAuth(ctx, transferKey)
		require.NoError(t, err)
		assert.Equal(t, "c2VjcmV0", auth.Base64SHA256Password)

		stored := fixture.Take(t)
		require.Equal(t, []string{
			events.TypeStationTransferred, events.TypeStationTransferred, events.TypeLocationUpdated, events.TypeLocationUpdated,
			events.TypeCommandCompleted,
		}, eventstest.Types(stored))
		assert.Equal(t, []string{transferFromOrg, transferToOrg, transferFromOrg, transferToOrg, transferFromOrg},
			[]string{stored[0].OrgId, stored[1].OrgId, stored[2].OrgId, stored[3].OrgId, stored[4].OrgId})
		assert.Equal(t, "urn:cpms:"+transferToOrg+":station/aaaa.CP001", stored[1].Subject)
	})
}
