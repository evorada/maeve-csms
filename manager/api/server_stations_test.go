// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// stationKeys are the cs_id of the items of a page, in its order.
func stationKeys(page api.ChargeStationPage) []string {
	keys := make([]string, len(page.Items))
	for i, item := range page.Items {
		keys[i] = item.CsId
	}
	return keys
}

// listStations lists the charge stations with the query and returns their keys.
func listStations(t *testing.T, r http.Handler, caller *tenancy.Principal, query string) []string {
	t.Helper()
	rr := serve(r, as(caller, http.MethodGet, "/cs?"+query, ""))
	require.Equal(t, http.StatusOK, rr.Code, "%s: %s", query, rr.Body.String())
	return stationKeys(decode[api.ChargeStationPage](t, rr))
}

// stationFleet is the multi-tenant manager with more charge stations: aaaa.CP001 (booted,
// connected, connector 0 Available), aaaa.CP002 ("Gent Zuid", booted, a connector
// Charging), aaaa.CP003 (pending, at the location of A), bbbb.CP002 and cccc.CP001.
type stationFleet struct {
	*multiTenant
	locationOfA *store.AssetLocation
}

func newStationFleet(t *testing.T) *stationFleet {
	t.Helper()
	ctx := context.Background()
	m := newMultiTenant(t)
	for key, orgId := range map[string]string{"aaaa.CP002": orgA, "aaaa.CP003": orgA, "bbbb.CP002": orgB, "cccc.CP001": orgC} {
		require.NoError(t, m.engine.SetChargeStationAuth(ctx, key, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
	}
	now := m.clock.Now()
	require.NoError(t, m.engine.SetChargeStationRuntimeDetails(ctx, stationOfA, &store.ChargeStationRuntimeDetails{
		OcppVersion: "1.6", Vendor: ptr("Alfen"), Model: ptr("Eve"), SerialNumber: ptr("SN-0001"), FirmwareVersion: ptr("5.8.1"),
		Iccid: ptr("8931080000000000001"), Imsi: ptr("204080000000001"), MeterSerialNumber: ptr("M-1"), MeterType: ptr("AC"),
		LastBootAt: ptr(now.Add(-time.Hour)),
	}))
	require.NoError(t, m.engine.SetChargeStationStatus(ctx, stationOfA, &store.ChargeStationStatus{
		ChargeStationId: stationOfA, Connected: true, LastHeartbeat: ptr(now.Add(-time.Minute)),
	}))
	require.NoError(t, m.engine.SetConnectorStatus(ctx, stationOfA, 0, &store.ConnectorStatus{ChargeStationId: stationOfA, ConnectorId: 0, Status: store.ConnectorStatusAvailable}))
	require.NoError(t, m.engine.SetChargeStationRuntimeDetails(ctx, "aaaa.CP002", &store.ChargeStationRuntimeDetails{
		OcppVersion: "2.0.1", Vendor: ptr("ABB"), Model: ptr("Terra AC"),
	}))
	require.NoError(t, m.engine.SetChargeStationStatus(ctx, "aaaa.CP002", &store.ChargeStationStatus{ChargeStationId: "aaaa.CP002"}))
	require.NoError(t, m.engine.SetConnectorStatus(ctx, "aaaa.CP002", 1, &store.ConnectorStatus{ChargeStationId: "aaaa.CP002", ConnectorId: 1, Status: store.ConnectorStatusCharging}))
	require.NoError(t, m.engine.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: "aaaa.CP002", DisplayName: ptr("Gent Zuid")}))

	fleet := &stationFleet{multiTenant: m}
	fleet.locationOfA = &store.AssetLocation{
		OrgId: orgA, PartyRef: partyOfA, Name: ptr("Depot"), Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, m.engine.CreateAssetLocation(ctx, fleet.locationOfA))
	require.NoError(t, m.engine.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: "aaaa.CP003", LocationId: &fleet.locationOfA.Id}))
	require.NoError(t, m.engine.SetChargeStationState(ctx, "aaaa.CP003", store.StationStatePending, ptr("waits for approval")))
	return fleet
}

func TestListChargeStations(t *testing.T) {
	f := newStationFleet(t)
	r := f.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)

	t.Run("the charge stations of the org of the caller", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodGet, "/cs", ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		page := decode[api.ChargeStationPage](t, rr)
		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002", "aaaa.CP003"}, stationKeys(page))
		assert.Nil(t, page.NextCursor)

		at := func(d time.Duration) string { return f.clock.Now().Add(d).UTC().Format(time.RFC3339) }
		assert.JSONEq(t, `{
			"cs_id": "aaaa.CP001",
			"org_id": "`+orgA+`",
			"location_id": null,
			"display_name": null,
			"max_capacity_kw": null,
			"allow_any_token": false,
			"state": "active",
			"state_changed_at": "`+at(0)+`",
			"state_reason": null,
			"ocpp_version": "1.6",
			"vendor": "Alfen",
			"model": "Eve",
			"serial_number": "SN-0001",
			"firmware_version": "5.8.1",
			"iccid": "8931080000000000001",
			"imsi": "204080000000001",
			"meter_serial_number": "M-1",
			"meter_type": "AC",
			"last_boot_at": "`+at(-time.Hour)+`",
			"connected": true,
			"last_heartbeat": "`+at(-time.Minute)+`"
		}`, jsonBody(t, page.Items[0]))

		pending := page.Items[2]
		assert.Equal(t, api.ChargeStationStatePending, pending.State)
		assert.Equal(t, ptr("waits for approval"), pending.StateReason)
		assert.Equal(t, &f.locationOfA.Id, pending.LocationId)
		assert.Nil(t, pending.OcppVersion, "never booted")
		assert.False(t, pending.Connected, "never connected")
		assert.Nil(t, pending.LastHeartbeat)
		assert.Nil(t, pending.Evses, "the EVSEs are only in the detail")
	})

	t.Run("filters", func(t *testing.T) {
		for query, want := range map[string][]string{
			"location_id=" + f.locationOfA.Id:  {"aaaa.CP003"},
			"state=pending":                    {"aaaa.CP003"},
			"state=active&state=pending":       {"aaaa.CP001", "aaaa.CP002", "aaaa.CP003"},
			"state=decommissioned":             {},
			"connected=true":                   {"aaaa.CP001"},
			"connected=false":                  {"aaaa.CP002", "aaaa.CP003"},
			"status=Charging":                  {"aaaa.CP002"},
			"status=Available&status=Charging": {"aaaa.CP001", "aaaa.CP002"},
			"status=Faulted":                   {},
			"vendor=abb":                       {"aaaa.CP002"},
			"model=EVE":                        {"aaaa.CP001"},
			"q=gent":                           {"aaaa.CP002"},
			"q=sn-0001":                        {"aaaa.CP001"},
			"q=CP00":                           {"aaaa.CP001", "aaaa.CP002", "aaaa.CP003"},
			"q=CP00&state=active&connected=false&vendor=ABB": {"aaaa.CP002"},
		} {
			assert.Equal(t, want, listStations(t, r, callerOfA, query), query)
		}
	})

	t.Run("the charge stations of another org are never listed", func(t *testing.T) {
		for _, query := range []string{
			"", "q=aaaa", "q=CP001", "location_id=" + f.locationOfA.Id, "vendor=Alfen", "connected=true",
			"status=Available", "state=pending", "limit=200",
		} {
			for _, key := range listStations(t, r, callerOfB, query) {
				assert.True(t, strings.HasPrefix(key, "bbbb."), "%s: %s", query, key)
			}
		}
		assert.Equal(t, []string{"bbbb.CP001", "bbbb.CP002"}, listStations(t, r, callerOfB, ""))
		assert.Equal(t, []string{"cccc.CP001"}, listStations(t, r, callerOf(orgC, tenancy.OrgScopeSubtree), ""))

		// a partner sees the tenants below it, and nothing with the scope self
		partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002", "aaaa.CP003", "bbbb.CP001", "bbbb.CP002"}, listStations(t, r, partner, ""))
		assert.Equal(t, []string{"bbbb.CP001", "bbbb.CP002"}, listStations(t, r, partner, "q=bbbb"))
		assert.Empty(t, listStations(t, r, callerOf(orgPartner, tenancy.OrgScopeSelf), ""))
		assert.Empty(t, listStations(t, r, callerOf(orgA, tenancy.OrgScope("all")), ""))

		rr := serve(r, as(callerOfB, http.MethodGet, "/cs?state=pending", ""))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `{"items":[],"next_cursor":null}`, rr.Body.String())
	})

	t.Run("pages", func(t *testing.T) {
		partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
		var seen []string
		cursor := ""
		pages := 0
		for {
			query := "limit=2"
			if cursor != "" {
				query += "&cursor=" + url.QueryEscape(cursor)
			}
			rr := serve(r, as(partner, http.MethodGet, "/cs?"+query, ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			page := decode[api.ChargeStationPage](t, rr)
			pages++
			seen = append(seen, stationKeys(page)...)
			if page.NextCursor == nil {
				assert.Len(t, page.Items, 1, "the last page")
				break
			}
			assert.Len(t, page.Items, 2)
			require.Less(t, pages, 4, "too many pages")
			cursor = *page.NextCursor
		}
		assert.Equal(t, 3, pages)
		assert.Equal(t, []string{"aaaa.CP001", "aaaa.CP002", "aaaa.CP003", "bbbb.CP001", "bbbb.CP002"}, seen, "ordered, no overlap")

		// a page that ends at the last charge station has no next page
		rr := serve(r, as(callerOfA, http.MethodGet, "/cs?limit=3", ""))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Nil(t, decode[api.ChargeStationPage](t, rr).NextCursor)
	})

	t.Run("parameters that are not known", func(t *testing.T) {
		for _, query := range []string{
			"limit=0", "limit=201", "limit=many", "cursor=%25%25", "state=retired", "state=active&state=Active",
			"status=Broken", "status=available", "location_id=depot", "connected=maybe",
		} {
			rr := serve(r, as(callerOfA, http.MethodGet, "/cs?"+query, ""))
			assert.Equal(t, http.StatusBadRequest, rr.Code, "%s: %s", query, rr.Body.String())
		}
	})
}

// In single mode the charge stations from before orgs belong to the default org.
func TestListChargeStationsInSingleMode(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clockTest.NewFakePassiveClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP001", &store.ChargeStationAuth{OrgId: testOrgId}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "CP002", &store.ChargeStationAuth{}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.CP003", &store.ChargeStationAuth{OrgId: orgA}))
	r := newSingleTenantRouter(t, engine, clockTest.NewFakePassiveClock(time.Now()), nil)

	rr := serve(r, as(nil, http.MethodGet, "/cs", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	page := decode[api.ChargeStationPage](t, rr)
	assert.Equal(t, []string{"CP001", "CP002"}, stationKeys(page))
	for _, item := range page.Items {
		assert.Equal(t, testOrgId, item.OrgId, item.CsId)
	}

	rr = serve(r, as(nil, http.MethodGet, "/cs/CP002", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, testOrgId, decode[api.ChargeStationRecord](t, rr).OrgId)
}

func TestGetChargeStation(t *testing.T) {
	ctx := context.Background()
	f := newStationFleet(t)
	r := f.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	ofA := f.assetsOf(t, orgA, stationOfA)
	require.NoError(t, f.engine.SetConnectorStatus(ctx, stationOfA, 1, &store.ConnectorStatus{ChargeStationId: stationOfA, ConnectorId: 1, Status: store.ConnectorStatusCharging}))

	rr := serve(r, as(callerOfA, http.MethodGet, "/cs/"+stationOfA, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got := decode[api.ChargeStationRecord](t, rr)
	assert.Equal(t, stationOfA, got.CsId)
	assert.Equal(t, orgA, got.OrgId)
	assert.Equal(t, ptr("Alfen"), got.Vendor)
	assert.True(t, got.Connected)
	assert.Equal(t, ptr(f.clock.Now().Add(-time.Minute).UTC()), got.LastHeartbeat)
	require.NotNil(t, got.Evses)
	require.Len(t, *got.Evses, 1)
	evse := (*got.Evses)[0]
	assert.Equal(t, ofA.evse.Id, evse.Uid)
	assert.Equal(t, api.AssetEvseLiveStatusCHARGING, evse.LiveStatus)
	assert.Len(t, evse.Connectors, 2)

	t.Run("a charge station without EVSEs, boot and status", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodGet, "/cs/aaaa.CP003", ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"evses":[]`)
		got := decode[api.ChargeStationRecord](t, rr)
		assert.False(t, got.Connected)
		assert.Nil(t, got.Vendor)
	})

	t.Run("another org", func(t *testing.T) {
		other := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodGet, "/cs/"+stationOfA, ""))
		unknown := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodGet, "/cs/bbbb.UNKNOWN", ""))
		require.Equal(t, http.StatusNotFound, other.Code)
		assert.JSONEq(t, notFoundBody, other.Body.String())
		assert.Equal(t, unknown.Body.String(), other.Body.String())
	})
}

// patchStation sends the merge patch to the charge station.
func patchStation(r http.Handler, caller *tenancy.Principal, csId, body string) *httptest.ResponseRecorder {
	return serve(r, as(caller, http.MethodPatch, "/cs/"+csId, body))
}

func lookupStation(t *testing.T, engine store.Engine, key string) *store.ChargeStation {
	t.Helper()
	cs, err := engine.LookupChargeStation(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, cs)
	return cs
}

func TestUpdateChargeStation(t *testing.T) {
	f := newStationFleet(t)
	r := f.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)

	t.Run("a merge patch changes what it has", func(t *testing.T) {
		rr := patchStation(r, callerOfA, stationOfA, `{"display_name":"Plaza 1","max_capacity_kw":22.5,"allow_any_token":true}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		got := decode[api.ChargeStationRecord](t, rr)
		assert.Equal(t, ptr("Plaza 1"), got.DisplayName)
		assert.Equal(t, ptr(22.5), got.MaxCapacityKw)
		assert.True(t, got.AllowAnyToken)
		assert.Equal(t, ptr("Alfen"), got.Vendor, "with what the charge station reported")
		assert.True(t, got.Connected)
		assert.Nil(t, got.Evses)

		// a field that the patch leaves out stays as it is, null clears it
		for _, step := range []struct {
			patch         string
			displayName   *string
			maxCapacityKw *float64
			allowAnyToken bool
		}{
			{`{"max_capacity_kw":11}`, ptr("Plaza 1"), ptr(11.0), true},
			{`{"allow_any_token":false}`, ptr("Plaza 1"), ptr(11.0), false},
			{`{"display_name":null}`, nil, ptr(11.0), false},
			{`{"display_name":"Plaza 2","max_capacity_kw":null}`, ptr("Plaza 2"), nil, false},
			{`{"allow_any_token":true}`, ptr("Plaza 2"), nil, true},
		} {
			rr = patchStation(r, callerOfA, stationOfA, step.patch)
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			cs := lookupStation(t, f.engine, stationOfA)
			assert.Equal(t, step.displayName, cs.DisplayName, step.patch)
			assert.Equal(t, step.maxCapacityKw, cs.MaxCapacityKw, step.patch)
			assert.Equal(t, step.allowAnyToken, cs.AllowAnyToken, step.patch)
			assert.Equal(t, store.StationStateActive, cs.State)
			assert.Nil(t, cs.LocationId)
		}

		before := lookupStation(t, f.engine, stationOfA)
		rr = patchStation(r, callerOfA, stationOfA, `{}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, before, lookupStation(t, f.engine, stationOfA), "an empty patch changes nothing")
	})

	t.Run("a pending charge station can be changed", func(t *testing.T) {
		rr := patchStation(r, callerOfA, "aaaa.CP003", `{"display_name":"Not yet"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, ptr("Not yet"), lookupStation(t, f.engine, "aaaa.CP003").DisplayName)
	})

	t.Run("fields that break a rule", func(t *testing.T) {
		before := lookupStation(t, f.engine, stationOfA)
		tooLong := strings.Repeat("é", 256)
		for body, want := range map[string]string{
			`{"state":"active"}`:                       `[{"field":"state","reason":"is not a field of a charge station that can be changed"}]`,
			`{"display_name":5}`:                       `[{"field":"display_name","reason":"must be a string or null"}]`,
			`{"display_name":"  "}`:                    `[{"field":"display_name","reason":"must be 1 to 255 characters, or null to clear it"}]`,
			`{"display_name":"` + tooLong + `"}`:       `[{"field":"display_name","reason":"must be 1 to 255 characters, or null to clear it"}]`,
			`{"max_capacity_kw":0}`:                    `[{"field":"max_capacity_kw","reason":"must be above 0 and at most 9999999.999 kW, or null to clear it"}]`,
			`{"max_capacity_kw":-1}`:                   `[{"field":"max_capacity_kw","reason":"must be above 0 and at most 9999999.999 kW, or null to clear it"}]`,
			`{"max_capacity_kw":10000000}`:             `[{"field":"max_capacity_kw","reason":"must be above 0 and at most 9999999.999 kW, or null to clear it"}]`,
			`{"max_capacity_kw":"22"}`:                 `[{"field":"max_capacity_kw","reason":"must be a number or null"}]`,
			`{"allow_any_token":null}`:                 `[{"field":"allow_any_token","reason":"must be true or false"}]`,
			`{"allow_any_token":"yes"}`:                `[{"field":"allow_any_token","reason":"must be true or false"}]`,
			`{"location_id":"depot"}`:                  `[{"field":"location_id","reason":"must be the id (a UUID) of a location or null"}]`,
			`{"location_id":"` + unknownAssetId + `"}`: `[{"field":"location_id","reason":"is not a location of the org of the charge station"}]`,
			`{"display_name":"ok","state":"x","max_capacity_kw":0,"allow_any_token":1}`: `[` +
				`{"field":"allow_any_token","reason":"must be true or false"},` +
				`{"field":"max_capacity_kw","reason":"must be above 0 and at most 9999999.999 kW, or null to clear it"},` +
				`{"field":"state","reason":"is not a field of a charge station that can be changed"}]`,
		} {
			rr := patchStation(r, callerOfA, stationOfA, body)
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, "%s: %s", body, rr.Body.String())
			assert.JSONEq(t, `{"status":"invalid","errors":`+want+`}`, rr.Body.String(), body)
		}
		assert.Equal(t, before, lookupStation(t, f.engine, stationOfA), "nothing is changed")
	})

	t.Run("a body that is not a JSON object", func(t *testing.T) {
		for _, body := range []string{`[]`, `null`, `"name"`, `{`, ``} {
			rr := patchStation(r, callerOfA, stationOfA, body)
			assert.Equal(t, http.StatusBadRequest, rr.Code, "%s: %s", body, rr.Body.String())
		}
	})

	t.Run("a location of another org is answered like one that does not exist", func(t *testing.T) {
		ofB := f.assetsOf(t, orgB, stationOfB)
		other := patchStation(r, callerOfA, stationOfA, `{"location_id":"`+ofB.location.Id+`"}`)
		unknown := patchStation(r, callerOfA, stationOfA, `{"location_id":"`+unknownAssetId+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, other.Code, other.Body.String())
		assert.Equal(t, unknown.Body.String(), other.Body.String())
		assert.Nil(t, lookupStation(t, f.engine, stationOfA).LocationId)
	})

	t.Run("the location is cleared only without EVSEs", func(t *testing.T) {
		rr := patchStation(r, callerOfA, "aaaa.CP003", `{"location_id":null}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Nil(t, lookupStation(t, f.engine, "aaaa.CP003").LocationId)

		ofA := f.assetsOf(t, orgA, "aaaa.CP002")
		require.NoError(t, f.engine.UpdateChargeStation(context.Background(), &store.ChargeStation{
			StationKey: "aaaa.CP002", DisplayName: ptr("Gent Zuid"), LocationId: &ofA.location.Id,
		}))
		rr = patchStation(r, callerOfA, "aaaa.CP002", `{"location_id":null}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"location_id","reason":"cannot be cleared while the charge station has EVSEs: move it to another location"}]}`, rr.Body.String())
		assert.Equal(t, &ofA.location.Id, lookupStation(t, f.engine, "aaaa.CP002").LocationId)
	})

	t.Run("a decommissioned charge station is not changed", func(t *testing.T) {
		require.NoError(t, f.engine.DecommissionChargeStation(context.Background(), "aaaa.CP003", nil))
		before := lookupStation(t, f.engine, "aaaa.CP003")
		rr := patchStation(r, callerOfA, "aaaa.CP003", `{"display_name":"Back"}`)
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"station_decommissioned: the charge station is decommissioned"}`, rr.Body.String())
		assert.Equal(t, before, lookupStation(t, f.engine, "aaaa.CP003"))
	})
}

// A charge station that moves takes its EVSEs with it, and the locations that lose or gain
// an EVSE are updated.
func TestMoveChargeStation(t *testing.T) {
	m := newMultiTenant(t)
	r, fixture := m.withEvents(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	plaza := createLocation(t, r, callerOfA, partyOfA)
	garage := createLocation(t, r, callerOfA, partyOfA)
	rr := serve(r, as(callerOfA, http.MethodPost, "/locations/"+plaza.Id+"/evses", jsonBody(t, evseInput(stationOfA, 1, 1))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	evse := decode[api.AssetEvse](t, rr)
	require.Equal(t, &plaza.Id, lookupStation(t, m.engine, stationOfA).LocationId)
	fixture.Take(t)

	rr = patchStation(r, callerOfA, stationOfA, `{"location_id":"`+strings.ToUpper(garage.Id)+`","display_name":"Moved"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got := decode[api.ChargeStationRecord](t, rr)
	assert.Equal(t, &garage.Id, got.LocationId)
	assert.Equal(t, ptr("Moved"), got.DisplayName)
	rr = serve(r, as(callerOfA, http.MethodGet, "/evses/"+evse.Uid, ""))
	require.Equal(t, http.StatusOK, rr.Code)
	moved := decode[api.AssetEvse](t, rr)
	assert.Equal(t, garage.Id, moved.LocationId)
	assert.Equal(t, int64(2), moved.Version)

	evs := fixture.Take(t)
	require.Len(t, evs, 2, "one event for each location")
	for i, loc := range []api.AssetLocation{garage, plaza} {
		assert.Equal(t, events.TypeLocationUpdated, evs[i].Type)
		assert.Equal(t, orgA, evs[i].OrgId)
		assert.Equal(t, "user:test", evs[i].Actor)
		assert.JSONEq(t, fmt.Sprintf(`{"location_id":%q,"ocpi_id":%q,"party_ref":"cpo-a","change":"updated","publish":true,"publish_changed":false}`,
			loc.Id, loc.OcpiId), string(evs[i].Data))
	}

	t.Run("to where it is", func(t *testing.T) {
		rr := patchStation(r, callerOfA, stationOfA, `{"location_id":"`+garage.Id+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Empty(t, fixture.Take(t), "nothing moved")
	})

	t.Run("without EVSEs", func(t *testing.T) {
		rr := patchStation(r, callerOfA, stationOfA, `{"display_name":"Renamed"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		require.NoError(t, m.engine.SetChargeStationAuth(context.Background(), "aaaa.CP002", &store.ChargeStationAuth{OrgId: orgA}))
		rr = patchStation(r, callerOfA, "aaaa.CP002", `{"location_id":"`+plaza.Id+`"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, &plaza.Id, lookupStation(t, m.engine, "aaaa.CP002").LocationId)
		assert.Empty(t, fixture.Take(t), "what the locations publish did not change")
	})

	t.Run("a refused move emits nothing", func(t *testing.T) {
		ofB := m.assetsOf(t, orgB, stationOfB)
		rr := patchStation(r, callerOfA, stationOfA, `{"location_id":"`+ofB.location.Id+`"}`)
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Empty(t, fixture.Take(t))
	})
}

func TestDecommissionChargeStation(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r, fixture := m.withEvents(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	ofA := m.assetsOf(t, orgA, stationOfA)
	require.NoError(t, m.engine.UpdateChargeStation(ctx, &store.ChargeStation{StationKey: stationOfA, LocationId: &ofA.location.Id}))
	removed := &store.AssetEvse{OrgId: orgA, LocationId: ofA.location.Id, StationKey: stationOfA, OcppEvseId: 2, Status: store.AssetEvseStatusRemoved,
		Connectors: []*store.AssetConnector{{OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32}}}
	require.NoError(t, m.engine.CreateAssetEvse(ctx, removed))
	authBefore, err := m.engine.LookupChargeStationAuth(ctx, stationOfA)
	require.NoError(t, err)
	fixture.Take(t)

	rr := serve(r, as(callerOfA, http.MethodGet, "/cs/"+stationOfA+"/auth", ""))
	require.Equal(t, http.StatusOK, rr.Code, "it may connect before")

	m.clock.SetTime(m.clock.Now().Add(time.Hour))
	rr = serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", `{"reason":"replaced by CP009"}`))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got := decode[api.ChargeStationRecord](t, rr)
	assert.Equal(t, api.ChargeStationStateDecommissioned, got.State)
	assert.Equal(t, ptr("replaced by CP009"), got.StateReason)
	assert.Equal(t, m.clock.Now().UTC(), got.StateChangedAt)
	assert.Equal(t, &ofA.location.Id, got.LocationId, "it is kept with its location")

	t.Run("its EVSEs are removed", func(t *testing.T) {
		evse, err := m.engine.LookupAssetEvse(ctx, ofA.evse.Id)
		require.NoError(t, err)
		assert.Equal(t, store.AssetEvseStatusRemoved, evse.Status)
		assert.Equal(t, int64(2), evse.Version)
		rr := serve(r, as(callerOfA, http.MethodGet, "/cs/"+stationOfA, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		detail := decode[api.ChargeStationRecord](t, rr)
		require.Len(t, *detail.Evses, 2)
		for _, evse := range *detail.Evses {
			assert.Equal(t, api.AssetEvseLiveStatusREMOVED, evse.LiveStatus)
		}
	})

	t.Run("the events", func(t *testing.T) {
		evs := fixture.Take(t)
		require.Len(t, evs, 2, "the EVSE that was not removed yet, and the charge station")
		assert.Equal(t, events.TypeLocationUpdated, evs[0].Type)
		assert.JSONEq(t, fmt.Sprintf(`{"location_id":%q,"ocpi_id":%q,"party_ref":"cpo-a","change":"updated","evse_uid":%q,"publish":true,"publish_changed":false}`,
			ofA.location.Id, ofA.location.OcpiId, ofA.evse.Id), string(evs[0].Data))

		ev := evs[1]
		assert.Equal(t, events.TypeStationDecommissioned, ev.Type)
		assert.Equal(t, orgA, ev.OrgId)
		assert.Equal(t, "urn:cpms:"+orgA+":station/"+stationOfA, ev.Subject)
		assert.Equal(t, "user:test", ev.Actor)
		assert.JSONEq(t, `{"station_key":"aaaa.CP001","station_id":"CP001","location_id":"`+ofA.location.Id+`","reason":"replaced by CP009","actor":"user:test"}`, string(ev.Data))
	})

	t.Run("it can no longer connect", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodGet, "/cs/"+stationOfA+"/auth", ""))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		assert.JSONEq(t, notFoundBody, rr.Body.String())
	})

	t.Run("its id cannot be registered again", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA, `{"securityProfile":1,"base64SHA256Password":"NEW"}`))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"station_decommissioned: the charge station is decommissioned; its id cannot be registered again"}`, rr.Body.String())
		auth, err := m.engine.LookupChargeStationAuth(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, authBefore, auth)
		assert.Equal(t, store.StationStateDecommissioned, lookupStation(t, m.engine, stationOfA).State)
	})

	t.Run("it is listed as decommissioned", func(t *testing.T) {
		assert.Equal(t, []string{stationOfA}, listStations(t, r, callerOfA, "state=decommissioned"))
		assert.Empty(t, listStations(t, r, callerOfA, "state=active"))
	})

	t.Run("not twice", func(t *testing.T) {
		before := lookupStation(t, m.engine, stationOfA)
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", `{"reason":"again","force":true}`))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"station_decommissioned: the charge station is decommissioned already"}`, rr.Body.String())
		assert.Equal(t, before, lookupStation(t, m.engine, stationOfA))
		assert.Empty(t, fixture.Take(t))
	})

	t.Run("another org", func(t *testing.T) {
		rr := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.CP002/decommission", ""))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		require.NoError(t, m.engine.SetChargeStationAuth(ctx, "aaaa.CP002", &store.ChargeStationAuth{OrgId: orgA}))
		other := serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/aaaa.CP002/decommission", ""))
		require.Equal(t, http.StatusNotFound, other.Code, other.Body.String())
		assert.Equal(t, rr.Body.String(), other.Body.String())
		assert.Equal(t, store.StationStateActive, lookupStation(t, m.engine, "aaaa.CP002").State)
	})
}

// Decommissioning never stops a charge: while a transaction runs it is refused, unless
// it is forced, and then the transaction goes on.
func TestDecommissionChargeStationWhileCharging(t *testing.T) {
	ctx := context.Background()
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	const conflict = `{"status":"Conflict","error":"active_transaction: a transaction is running on the charge station; force decommissions it all the same"}`

	activeTransactions := func(t *testing.T, engine store.Engine) int64 {
		_, active, err := engine.ListTransactionsForChargeStation(ctx, stationOfA, "active", nil, nil, 10, 0)
		require.NoError(t, err)
		return active
	}
	currentTransaction := func(t *testing.T, engine store.Engine) *string {
		status, err := engine.GetConnectorStatus(ctx, stationOfA, 2)
		require.NoError(t, err)
		if status == nil {
			return nil
		}
		return status.CurrentTransactionId
	}
	for name, tc := range map[string]struct {
		charge   func(t *testing.T, engine store.Engine)
		charging func(t *testing.T, engine store.Engine) bool
	}{
		"a transaction that has not ended": {
			charge: func(t *testing.T, engine store.Engine) {
				require.NoError(t, engine.CreateTransaction(ctx, stationOfA, "tx-1", "TAG", "ISO14443", nil, 0, false))
			},
			charging: func(t *testing.T, engine store.Engine) bool { return activeTransactions(t, engine) == 1 },
		},
		"a current transaction in the status of a connector": {
			charge: func(t *testing.T, engine store.Engine) {
				require.NoError(t, engine.SetConnectorStatus(ctx, stationOfA, 2, &store.ConnectorStatus{
					ChargeStationId: stationOfA, ConnectorId: 2, Status: store.ConnectorStatusCharging, CurrentTransactionId: ptr("tx-1"),
				}))
			},
			charging: func(t *testing.T, engine store.Engine) bool {
				return assert.ObjectsAreEqual(ptr("tx-1"), currentTransaction(t, engine))
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMultiTenant(t)
			r, fixture := m.withEvents(t)
			ofA := m.assetsOf(t, orgA, stationOfA)
			tc.charge(t, m.engine)
			require.True(t, tc.charging(t, m.engine))
			fixture.Take(t)

			for _, body := range []string{"", `{}`, `{"force":false}`} {
				rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", body))
				require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
				assert.JSONEq(t, conflict, rr.Body.String())
			}
			assert.Equal(t, store.StationStateActive, lookupStation(t, m.engine, stationOfA).State)
			ofA.assertUnchanged(t)
			assert.Empty(t, fixture.Take(t))

			rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", `{"force":true}`))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			assert.Equal(t, store.StationStateDecommissioned, lookupStation(t, m.engine, stationOfA).State)
			assert.Len(t, fixture.Take(t), 2)

			assert.True(t, tc.charging(t, m.engine), "the transaction goes on")
		})
	}

	t.Run("a transaction that has ended", func(t *testing.T) {
		m := newMultiTenant(t)
		r := m.router(t)
		require.NoError(t, m.engine.CreateTransaction(ctx, stationOfA, "tx-1", "TAG", "ISO14443", nil, 0, false))
		require.NoError(t, m.engine.EndTransaction(ctx, stationOfA, "tx-1", "TAG", "ISO14443", nil, 1))
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Nil(t, decode[api.ChargeStationRecord](t, rr).StateReason)
	})

	t.Run("a transaction of another charge station", func(t *testing.T) {
		m := newMultiTenant(t)
		r := m.router(t)
		require.NoError(t, m.engine.CreateTransaction(ctx, stationOfB, "tx-1", "TAG", "ISO14443", nil, 0, false))
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})
}

func TestDecommissionChargeStationRefusals(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)

	for body, wantCode := range map[string]int{
		`{"reason":"` + strings.Repeat("x", 256) + `"}`: http.StatusUnprocessableEntity,
		`{"reason":5}`: http.StatusBadRequest,
		`[`:            http.StatusBadRequest,
	} {
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", body))
		require.Equal(t, wantCode, rr.Code, "%s: %s", body, rr.Body.String())
	}
	assert.Equal(t, store.StationStateActive, lookupStation(t, m.engine, stationOfA).State)
	rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", `{"reason":"`+strings.Repeat("x", 256)+`"}`))
	assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"reason","reason":"must be at most 255 characters"}]}`, rr.Body.String())

	t.Run("a pending charge station is decommissioned too", func(t *testing.T) {
		require.NoError(t, m.engine.SetChargeStationState(ctx, stationOfA, store.StationStatePending, nil))
		rr := serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA+"/decommission", `{"reason":"rejected"}`))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, store.StationStateDecommissioned, lookupStation(t, m.engine, stationOfA).State)
	})
}

func TestDeleteToken(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	tokenUids := func() []string {
		tokens, err := m.engine.ListTokens(ctx, 0, 100)
		require.NoError(t, err)
		var uids []string
		for _, token := range tokens {
			uids = append(uids, token.OrgId+"/"+token.Uid)
		}
		return uids
	}
	for _, token := range []*store.Token{
		storedToken(orgA, "SAMEUID", "of A"), storedToken(orgB, "SAMEUID", "of B"), storedToken(orgA, "OTHERUID", "of A"),
	} {
		require.NoError(t, m.engine.SetToken(ctx, token))
	}

	rr := serve(r, as(callerOfA, http.MethodDelete, "/token/SAMEUID", ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	assert.Empty(t, rr.Body.String())
	assert.ElementsMatch(t, []string{orgB + "/SAMEUID", orgA + "/OTHERUID"}, tokenUids(), "only the token of the org of the caller")
	rr = serve(r, as(callerOfA, http.MethodGet, "/token/SAMEUID", ""))
	assert.Equal(t, http.StatusNotFound, rr.Code)

	// again: there is nothing to delete
	rr = serve(r, as(callerOfA, http.MethodDelete, "/token/SAMEUID", ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	t.Run("the org of the token", func(t *testing.T) {
		// out of the scope of the caller: like a token that does not exist, nothing deleted
		rr := serve(r, as(callerOfA, http.MethodDelete, "/token/SAMEUID?orgId="+orgB, ""))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		assert.JSONEq(t, notFoundBody, rr.Body.String())
		assert.Contains(t, tokenUids(), orgB+"/SAMEUID")

		// a partner names the org below it, and has to
		partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
		rr = serve(r, as(partner, http.MethodDelete, "/token/SAMEUID", ""))
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		assert.Contains(t, tokenUids(), orgB+"/SAMEUID")
		rr = serve(r, as(partner, http.MethodDelete, "/token/SAMEUID?orgId="+orgB, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		assert.Equal(t, []string{orgA + "/OTHERUID"}, tokenUids())
	})

	t.Run("a suspended tenant", func(t *testing.T) {
		m.setTenantStatus(t, orgA, store.TenantStatusSuspended)
		rr := serve(r, as(callerOfA, http.MethodDelete, "/token/OTHERUID", ""))
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, suspendedBody, rr.Body.String())
		assert.Equal(t, []string{orgA + "/OTHERUID"}, tokenUids())
	})
}

// withoutRegistry is a storage engine without the registry of the charge stations, that
// cannot delete the token of one org either, like Firestore.
type withoutRegistry struct {
	store.Engine
}

func (withoutRegistry) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented)
}

func (withoutRegistry) ListChargeStations(context.Context, store.ChargeStationFilter) ([]*store.ChargeStation, string, error) {
	return nil, "", fmt.Errorf("ListChargeStations: %w", store.ErrNotImplemented)
}

func (withoutRegistry) DeleteToken(context.Context, string, string) error {
	return fmt.Errorf("DeleteToken: %w", store.ErrNotImplemented)
}

func TestStationsOfAStorageEngineWithoutRegistry(t *testing.T) {
	m := newMultiTenant(t)
	r := newRouter(t, withoutRegistry{m.engine}, m.clock, nil, m.scoper, func(r *http.Request) *tenancy.Principal {
		p, _ := tenancy.FromContext(r.Context())
		return p
	})
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	const noRegistry = `{"status":"Not Implemented","error":"the storage engine of the manager does not keep the registry of the charge stations"}`

	for _, tc := range []struct{ method, path, body, want string }{
		{http.MethodGet, "/cs", "", noRegistry},
		{http.MethodGet, "/cs/" + stationOfA, "", noRegistry},
		{http.MethodPatch, "/cs/" + stationOfA, `{"display_name":"Plaza"}`, noRegistry},
		{http.MethodPost, "/cs/" + stationOfA + "/decommission", "", noRegistry},
		{http.MethodDelete, "/token/TOKEN", "", `{"status":"Not Implemented","error":"the storage engine of the manager cannot delete the token of one org"}`},
	} {
		rr := serve(r, as(callerOfA, tc.method, tc.path, tc.body))
		require.Equal(t, http.StatusNotImplemented, rr.Code, "%s %s: %s", tc.method, tc.path, rr.Body.String())
		assert.JSONEq(t, tc.want, rr.Body.String())
	}

	// its charge stations are never decommissioned: they connect and register again
	rr := serve(r, as(callerOfA, http.MethodGet, "/cs/"+stationOfA+"/auth", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodPost, "/cs/"+stationOfA, `{"securityProfile":1}`))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
}
