// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// The parties of the tenants A and B of the multi-tenant tests.
const (
	partyOfA = "cpo-a"
	partyOfB = "cpo-b"
)

// unknownAssetId is a UUID that no asset of the tests has.
const unknownAssetId = "0193b2c5-ffff-7000-8000-000000000000"

func ptr[T any](v T) *T {
	return &v
}

// jsonBody is v as the body of a request.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)
	return string(body)
}

// decode reads the JSON body of an answer.
func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &v), rr.Body.String())
	return v
}

// locationInput is a valid location of the party, without the fields that have defaults.
func locationInput(partyRef, name string) api.AssetLocationInput {
	return api.AssetLocationInput{
		PartyRef:    partyRef,
		Name:        ptr(name),
		Address:     "F. Rooseveltlaan 3A",
		City:        "Gent",
		PostalCode:  ptr("9000"),
		Country:     "BEL",
		Coordinates: api.GeoLocation{Latitude: "51.047599", Longitude: "3.729944"},
		TimeZone:    "Europe/Brussels",
	}
}

// connectorInput is a valid AC connector with the OCPP connector id.
func connectorInput(ocppConnectorId int) api.AssetConnectorInput {
	return api.AssetConnectorInput{
		OcppConnectorId: ocppConnectorId,
		Standard:        "IEC_62196_T2",
		Format:          "SOCKET",
		PowerType:       "AC_3_PHASE",
		MaxVoltage:      400,
		MaxAmperage:     32,
	}
}

// evseInput is a valid EVSE of the charge station with connectors of the OCPP connector
// ids.
func evseInput(csId string, ocppEvseId int, ocppConnectorIds ...int) api.AssetEvseInput {
	connectors := make([]api.AssetConnectorInput, len(ocppConnectorIds))
	for i, id := range ocppConnectorIds {
		connectors[i] = connectorInput(id)
	}
	return api.AssetEvseInput{CsId: csId, OcppEvseId: ocppEvseId, Connectors: &connectors}
}

// assetFixture is a location of an org with an EVSE of one of its charge stations, which
// has two connectors, all created through the store.
type assetFixture struct {
	engine   store.Engine
	location *store.AssetLocation
	evse     *store.AssetEvse
}

func (m *multiTenant) assetsOf(t *testing.T, orgId, stationKey string) *assetFixture {
	t.Helper()
	ctx := context.Background()
	partyRef := map[string]string{orgA: partyOfA, orgB: partyOfB}[orgId]
	loc := &store.AssetLocation{
		OrgId: orgId, PartyRef: partyRef, OcpiId: "LOC-" + stationKey, Name: ptr("Location of " + stationKey),
		Address: "Calle Mayor 1", City: "Madrid", Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790",
		TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, m.engine.CreateAssetLocation(ctx, loc))
	evse := &store.AssetEvse{OrgId: orgId, LocationId: loc.Id, StationKey: stationKey, OcppEvseId: 1}
	for _, id := range []int{1, 2} {
		evse.Connectors = append(evse.Connectors, &store.AssetConnector{
			OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
		})
	}
	require.NoError(t, m.engine.CreateAssetEvse(ctx, evse))
	return &assetFixture{engine: m.engine, location: loc, evse: evse}
}

func (a *assetFixture) locationPath() string {
	return "/locations/" + a.location.Id
}

func (a *assetFixture) evsePath() string {
	return "/evses/" + a.evse.Id
}

func (a *assetFixture) connectorPath() string {
	return a.evsePath() + "/connectors/" + a.evse.Connectors[1].Id
}

// paths are the paths of the asset operations that write to the assets.
func (a *assetFixture) paths() map[string]string {
	return map[string]string{
		"PUT /locations/{locationId}":                       a.locationPath(),
		"DELETE /locations/{locationId}":                    a.locationPath(),
		"POST /locations/{locationId}/evses":                a.locationPath() + "/evses",
		"PUT /evses/{evseUid}":                              a.evsePath(),
		"DELETE /evses/{evseUid}":                           a.evsePath(),
		"POST /evses/{evseUid}/connectors":                  a.evsePath() + "/connectors",
		"PUT /evses/{evseUid}/connectors/{connectorUid}":    a.connectorPath(),
		"DELETE /evses/{evseUid}/connectors/{connectorUid}": a.connectorPath(),
	}
}

// bodies are valid bodies of the asset operations that write.
func (a *assetFixture) bodies() map[string]string {
	location, _ := json.Marshal(locationInput(a.location.PartyRef, "Changed"))
	evse, _ := json.Marshal(evseInput(a.evse.StationKey, 2, 1))
	changedEvse, _ := json.Marshal(evseInput(a.evse.StationKey, 1))
	connector, _ := json.Marshal(connectorInput(3))
	changedConnector, _ := json.Marshal(connectorInput(2))
	return map[string]string{
		"POST /locations":                                string(location),
		"PUT /locations/{locationId}":                    string(location),
		"POST /locations/{locationId}/evses":             string(evse),
		"PUT /evses/{evseUid}":                           string(changedEvse),
		"POST /evses/{evseUid}/connectors":               string(connector),
		"PUT /evses/{evseUid}/connectors/{connectorUid}": string(changedConnector),
	}
}

// assertUnchanged checks that the assets are as they were created.
func (a *assetFixture) assertUnchanged(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	loc, err := a.engine.LookupAssetLocation(ctx, a.location.Id)
	require.NoError(t, err)
	assert.Equal(t, a.location, loc)
	evse, err := a.engine.LookupAssetEvse(ctx, a.evse.Id)
	require.NoError(t, err)
	assert.Equal(t, a.evse, evse)
	evses, err := a.engine.ListAssetEvsesForLocation(ctx, a.location.Id)
	require.NoError(t, err)
	assert.Len(t, evses, 1)
}

func TestAssetLocationLifecycle(t *testing.T) {
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)

	in := locationInput(partyOfA, "Gent Zuid")
	in.Facilities = &[]string{"CAFE", "WIFI"}
	in.ExternalRefs = &[]string{"ext-1"}
	in.ParkingType = ptr("ON_STREET")
	in.OpeningTimes = &api.OpeningTimes{RegularHours: &[]api.RegularHours{{Weekday: 1, PeriodBegin: "08:00", PeriodEnd: "20:00"}}}
	rr := serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, in)))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	created := decode[api.AssetLocation](t, rr)
	assert.Equal(t, "/api/v0/locations/"+created.Id, rr.Header().Get("Location"))
	assert.Equal(t, `"1"`, rr.Header().Get("ETag"))
	createdAt := m.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	assert.JSONEq(t, `{
		"id": "`+created.Id+`",
		"org_id": "`+orgA+`",
		"ocpi_id": "`+created.Id+`",
		"party_ref": "cpo-a",
		"name": "Gent Zuid",
		"address": "F. Rooseveltlaan 3A",
		"city": "Gent",
		"postal_code": "9000",
		"state": null,
		"country": "BEL",
		"coordinates": {"latitude": "51.047599", "longitude": "3.729944"},
		"time_zone": "Europe/Brussels",
		"parking_type": "ON_STREET",
		"publish": true,
		"opening_times": {"twentyfourseven": false, "regular_hours": [{"weekday": 1, "period_begin": "08:00", "period_end": "20:00"}]},
		"facilities": ["CAFE", "WIFI"],
		"charging_when_closed": null,
		"external_refs": ["ext-1"],
		"version": 1,
		"created_at": "`+createdAt+`",
		"updated_at": "`+createdAt+`"
	}`, rr.Body.String())

	t.Run("read", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodGet, "/locations/"+created.Id, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"1"`, rr.Header().Get("ETag"))
		assert.Equal(t, created, decode[api.AssetLocation](t, rr))
	})

	t.Run("change", func(t *testing.T) {
		changed := locationInput(partyOfA, "Gent Sint-Pieters")
		changed.State = ptr("Oost-Vlaanderen")
		req := as(callerOfA, http.MethodPut, "/locations/"+created.Id, jsonBody(t, changed))
		req.Header.Set("If-Match", `"1"`)
		rr := serve(r, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"2"`, rr.Header().Get("ETag"))
		got := decode[api.AssetLocation](t, rr)
		assert.Equal(t, int64(2), got.Version)
		assert.Equal(t, ptr("Gent Sint-Pieters"), got.Name)
		assert.Equal(t, ptr("Oost-Vlaanderen"), got.State)
		// what the body leaves out is cleared, but for what the manager sets on a create
		assert.Equal(t, created.Id, got.OcpiId)
		assert.True(t, got.Publish)
		assert.Nil(t, got.ParkingType)
		assert.Nil(t, got.OpeningTimes)
		assert.Equal(t, []string{}, got.Facilities)
		assert.Equal(t, created.CreatedAt, got.CreatedAt)
	})

	t.Run("a change against an older version", func(t *testing.T) {
		for _, ifMatch := range []string{`"1"`, `W/"2"`, `"1", "3"`} {
			req := as(callerOfA, http.MethodPut, "/locations/"+created.Id, jsonBody(t, locationInput(partyOfA, "Lost update")))
			req.Header.Set("If-Match", ifMatch)
			rr := serve(r, req)
			require.Equal(t, http.StatusPreconditionFailed, rr.Code, ifMatch)
			assert.JSONEq(t, `{"status":"Precondition Failed","error":"version_conflict: the location has version 2"}`, rr.Body.String())
		}
		loc, err := m.engine.LookupAssetLocation(context.Background(), created.Id)
		require.NoError(t, err)
		assert.Equal(t, ptr("Gent Sint-Pieters"), loc.Name, "nothing is changed")
		assert.Equal(t, int64(2), loc.Version)
	})

	t.Run("an If-Match that cannot be read", func(t *testing.T) {
		for _, ifMatch := range []string{"2", `"2`, "", `"2" "3"`} {
			req := as(callerOfA, http.MethodPut, "/locations/"+created.Id, jsonBody(t, locationInput(partyOfA, "Not read")))
			req.Header.Set("If-Match", ifMatch)
			rr := serve(r, req)
			require.Equal(t, http.StatusBadRequest, rr.Code, ifMatch)
		}
	})

	t.Run("a change with any version, publish and ocpi_id given", func(t *testing.T) {
		changed := locationInput(partyOfA, "Gent")
		changed.OcpiId = ptr("GENT-1")
		changed.Publish = ptr(false)
		changed.OrgId = ptr(uuid.MustParse(orgA))
		for _, ifMatch := range []string{`*`, `"7", "3"`} {
			req := as(callerOfA, http.MethodPut, "/locations/"+created.Id, jsonBody(t, changed))
			req.Header.Set("If-Match", ifMatch)
			rr := serve(r, req)
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			got := decode[api.AssetLocation](t, rr)
			assert.Equal(t, "GENT-1", got.OcpiId)
			assert.False(t, got.Publish)
		}
		// and without If-Match
		rr := serve(r, as(callerOfA, http.MethodPut, "/locations/"+created.Id, jsonBody(t, changed)))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"5"`, rr.Header().Get("ETag"))
	})

	t.Run("delete", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodDelete, "/locations/"+created.Id, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			rr = serve(r, as(callerOfA, method, "/locations/"+created.Id, ""))
			require.Equal(t, http.StatusNotFound, rr.Code, method)
			assert.JSONEq(t, notFoundBody, rr.Body.String())
		}
	})

	t.Run("an id that is not a UUID", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodGet, "/locations/not-a-uuid", ""))
		require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	})
}

func TestCreateAssetLocationForAnotherOrg(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)

	in := locationInput(partyOfB, "Of B")
	in.OrgId = ptr(uuid.MustParse(orgB))
	rr := serve(r, as(partner, http.MethodPost, "/locations", jsonBody(t, in)))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Equal(t, orgB, decode[api.AssetLocation](t, rr).OrgId)

	// a partner that is not a tenant names the org
	rr = serve(r, as(partner, http.MethodPost, "/locations", jsonBody(t, locationInput(partyOfB, "No org"))))
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// an org outside the scope of the caller is answered like one that does not exist
	in = locationInput(partyOfB, "Of B by A")
	in.OrgId = ptr(uuid.MustParse(orgB))
	rr = serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/locations", jsonBody(t, in)))
	require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
	assert.JSONEq(t, notFoundBody, rr.Body.String())

	locations, _, err := m.engine.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA, orgB}})
	require.NoError(t, err)
	require.Len(t, locations, 1)
	assert.Equal(t, ptr("Of B"), locations[0].Name)
}

func TestAssetLocationValidation(t *testing.T) {
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	existing := m.assetsOf(t, orgA, stationOfA)

	tests := map[string]struct {
		change     func(in *api.AssetLocationInput)
		wantFields []string
	}{
		"a country that is not alpha-3": {func(in *api.AssetLocationInput) { in.Country = "BE" }, []string{"country"}},
		"a latitude without decimals":   {func(in *api.AssetLocationInput) { in.Coordinates.Latitude = "51" }, []string{"coordinates.latitude"}},
		"a time zone that is not IANA":  {func(in *api.AssetLocationInput) { in.TimeZone = "CET+1" }, []string{"time_zone"}},
		"no address and no city":        {func(in *api.AssetLocationInput) { in.Address, in.City = "", "" }, []string{"address", "city"}},
		"an unknown facility":           {func(in *api.AssetLocationInput) { in.Facilities = &[]string{"SPA"} }, []string{"facilities[0]"}},
		"four external references":      {func(in *api.AssetLocationInput) { in.ExternalRefs = &[]string{"a", "b", "c", "d"} }, []string{"external_refs"}},
		"open all the time and on Mondays": {func(in *api.AssetLocationInput) {
			in.OpeningTimes = &api.OpeningTimes{Twentyfourseven: true, RegularHours: &[]api.RegularHours{{Weekday: 1, PeriodBegin: "08:00", PeriodEnd: "20:00"}}}
		}, []string{"opening_times.regular_hours"}},
		"a party of another tenant": {func(in *api.AssetLocationInput) { in.PartyRef = partyOfB }, []string{"party_ref"}},
		"no party":                  {func(in *api.AssetLocationInput) { in.PartyRef = "" }, []string{"party_ref"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			in := locationInput(partyOfA, "Invalid")
			test.change(&in)
			for _, req := range []*http.Request{
				as(callerOfA, http.MethodPost, "/locations", jsonBody(t, in)),
				as(callerOfA, http.MethodPut, existing.locationPath(), jsonBody(t, in)),
			} {
				rr := serve(r, req)
				require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
				got := decode[api.ValidationErrors](t, rr)
				assert.Equal(t, api.Invalid, got.Status)
				var fields []string
				for _, e := range got.Errors {
					fields = append(fields, e.Field)
					assert.NotEmpty(t, e.Reason)
				}
				assert.Equal(t, test.wantFields, fields)
			}
		})
	}

	t.Run("the answer", func(t *testing.T) {
		in := locationInput(partyOfA, "Invalid")
		in.Country = "be"
		rr := serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, in)))
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"country","reason":"must be an ISO 3166-1 alpha-3 code"}]}`, rr.Body.String())
	})

	t.Run("a change cannot move the location to another org", func(t *testing.T) {
		in := locationInput(partyOfA, "Moved")
		in.OrgId = ptr(uuid.MustParse(orgB))
		rr := serve(r, as(callerOf(orgPartner, tenancy.OrgScopeSubtree), http.MethodPut, existing.locationPath(), jsonBody(t, in)))
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"field":"org_id"`)
	})

	t.Run("a body that is not JSON", func(t *testing.T) {
		for _, req := range []*http.Request{
			as(callerOfA, http.MethodPost, "/locations", "{"),
			as(callerOfA, http.MethodPut, existing.locationPath(), `{"address": 3}`),
			as(callerOfA, http.MethodPost, "/locations", ""),
		} {
			rr := serve(r, req)
			require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})

	existing.assertUnchanged(t)
}

func TestAssetLocationConflicts(t *testing.T) {
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	existing := m.assetsOf(t, orgA, stationOfA)

	t.Run("an OCPI id that the party has", func(t *testing.T) {
		in := locationInput(partyOfA, "Twin")
		in.OcpiId = ptr(existing.location.OcpiId)
		rr := serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, in)))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"already_exists: the party already has a location with this ocpi_id"}`, rr.Body.String())

		in.OcpiId = ptr("OTHER")
		rr = serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, in)))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		other := decode[api.AssetLocation](t, rr)

		in.OcpiId = ptr(existing.location.OcpiId)
		rr = serve(r, as(callerOfA, http.MethodPut, "/locations/"+other.Id, jsonBody(t, in)))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "already_exists")
	})

	t.Run("a location with EVSEs", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodDelete, existing.locationPath(), ""))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"in_use: EVSEs are at the location"}`, rr.Body.String())
	})

	existing.assertUnchanged(t)
}

// The locations, EVSEs and connectors of another tenant are answered exactly like those
// that do not exist, and nothing is changed.
func TestAssetsOfAnotherTenantAreNotFound(t *testing.T) {
	m := newMultiTenant(t)
	r := m.router(t)
	ofA := m.assetsOf(t, orgA, stationOfA)
	ofB := m.assetsOf(t, orgB, stationOfB)
	callers := map[string]*tenancy.Principal{
		"another tenant":                 callerOf(orgB, tenancy.OrgScopeSelf),
		"a tenant that is not below":     callerOf(orgC, tenancy.OrgScopeSubtree),
		"a partner with the scope self":  callerOf(orgPartner, tenancy.OrgScopeSelf),
		"a caller with an unknown scope": callerOf(orgA, tenancy.OrgScope("all")),
	}
	bodies := ofA.bodies()
	unknownPaths := map[string]string{
		"/locations/" + ofA.location.Id: "/locations/" + unknownAssetId,
		"/evses/" + ofA.evse.Id:         "/evses/" + unknownAssetId,
		ofA.connectorPath():             ofA.evsePath() + "/connectors/" + unknownAssetId,
		ofA.locationPath() + "/evses":   "/locations/" + unknownAssetId + "/evses",
		ofA.locationPath() + "/ocpi":    "/locations/" + unknownAssetId + "/ocpi",
		ofA.evsePath() + "/connectors":  "/evses/" + unknownAssetId + "/connectors",
	}
	operations := []struct{ operation, path string }{
		{"GET /locations/{locationId}", ofA.locationPath()},
		{"GET /locations/{locationId}/ocpi", ofA.locationPath() + "/ocpi"},
		{"GET /locations/{locationId}/evses", ofA.locationPath() + "/evses"},
		{"GET /evses/{evseUid}", ofA.evsePath()},
	}
	for operation, path := range ofA.paths() {
		operations = append(operations, struct{ operation, path string }{operation, path})
	}

	for name, caller := range callers {
		for _, op := range operations {
			t.Run(name+": "+op.operation, func(t *testing.T) {
				method := methodOf(op.operation)
				other := serve(r, as(caller, method, op.path, bodies[op.operation]))
				require.Equal(t, http.StatusNotFound, other.Code, other.Body.String())
				assert.JSONEq(t, notFoundBody, other.Body.String())
				unknown := serve(r, as(caller, method, unknownPaths[op.path], bodies[op.operation]))
				assert.Equal(t, unknown.Code, other.Code)
				assert.Equal(t, unknown.Body.String(), other.Body.String())
				assert.Equal(t, unknown.Header(), other.Header())
			})
		}
	}

	t.Run("a connector of an EVSE of another tenant under an own EVSE", func(t *testing.T) {
		path := ofB.evsePath() + "/connectors/" + ofA.evse.Connectors[1].Id
		callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			rr := serve(r, as(callerOfB, method, path, bodies["PUT /evses/{evseUid}/connectors/{connectorUid}"]))
			require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		}
	})

	t.Run("lists", func(t *testing.T) {
		for name, caller := range callers {
			rr := serve(r, as(caller, http.MethodGet, "/locations", ""))
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			page := decode[api.AssetLocationPage](t, rr)
			for _, loc := range page.Items {
				assert.NotEqual(t, ofA.location.Id, loc.Id, name)
			}
		}
	})

	ofA.assertUnchanged(t)
	ofB.assertUnchanged(t)
}

// methodOf is the method of an operation ("METHOD /spec/path").
func methodOf(operation string) string {
	method, _, _ := strings.Cut(operation, " ")
	return method
}

func TestListAssetLocations(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	for i := 0; i < 120; i++ {
		loc := &store.AssetLocation{
			OrgId: orgA, PartyRef: partyOfA, OcpiId: fmt.Sprintf("A-%03d", i), Name: ptr(fmt.Sprintf("Location %03d", i)),
			Address: "Calle Mayor 1", City: "Madrid", Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790",
			TimeZone: "Europe/Madrid", Publish: i%10 != 0,
		}
		require.NoError(t, m.engine.CreateAssetLocation(ctx, loc))
	}
	ofB := m.assetsOf(t, orgB, stationOfB)

	list := func(t *testing.T, caller *tenancy.Principal, query url.Values) api.AssetLocationPage {
		t.Helper()
		rr := serve(r, as(caller, http.MethodGet, "/locations?"+query.Encode(), ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		return decode[api.AssetLocationPage](t, rr)
	}

	t.Run("pages", func(t *testing.T) {
		seen := map[string]bool{}
		var sizes []int
		query := url.Values{"limit": {"50"}}
		var previous string
		for {
			page := list(t, callerOfA, query)
			sizes = append(sizes, len(page.Items))
			for _, loc := range page.Items {
				assert.False(t, seen[loc.Id], "a location is on one page only")
				seen[loc.Id] = true
				assert.Greater(t, loc.Id, previous, "ordered by id")
				previous = loc.Id
				assert.Equal(t, orgA, loc.OrgId)
				assert.Nil(t, loc.Evses)
			}
			if page.NextCursor == nil {
				break
			}
			query.Set("cursor", *page.NextCursor)
		}
		assert.Equal(t, []int{50, 50, 20}, sizes)
		assert.Len(t, seen, 120)
	})

	t.Run("the default page", func(t *testing.T) {
		page := list(t, callerOfA, nil)
		assert.Len(t, page.Items, 50)
		assert.NotNil(t, page.NextCursor)
		rr := serve(r, as(callerOfA, http.MethodGet, "/locations?limit=200", ""))
		require.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), `"next_cursor":null`)
	})

	t.Run("filters", func(t *testing.T) {
		assert.Len(t, list(t, callerOfA, url.Values{"q": {"location 11"}}).Items, 10)
		assert.Len(t, list(t, callerOfA, url.Values{"q": {"a-007"}}).Items, 1)
		assert.Len(t, list(t, callerOfA, url.Values{"publish": {"false"}, "limit": {"200"}}).Items, 12)
		assert.Len(t, list(t, callerOfA, url.Values{"publish": {"true"}, "limit": {"200"}}).Items, 108)
		assert.Empty(t, list(t, callerOfA, url.Values{"party_ref": {partyOfB}}).Items)
		assert.Len(t, list(t, callerOfA, url.Values{"party_ref": {partyOfA}, "limit": {"200"}}).Items, 120)
	})

	t.Run("the orgs of the caller", func(t *testing.T) {
		ofOwnB := list(t, callerOf(orgB, tenancy.OrgScopeSelf), nil)
		require.Len(t, ofOwnB.Items, 1)
		assert.Equal(t, ofB.location.Id, ofOwnB.Items[0].Id)
		assert.Nil(t, ofOwnB.NextCursor)

		partner := list(t, callerOf(orgPartner, tenancy.OrgScopeSubtree), url.Values{"limit": {"200"}})
		assert.Len(t, partner.Items, 121, "the locations of A and B")

		assert.Empty(t, list(t, callerOf(orgC, tenancy.OrgScopeSubtree), nil).Items)
	})

	t.Run("parameters that are not valid", func(t *testing.T) {
		for _, query := range []string{"limit=0", "limit=201", "limit=-1", "limit=ten", "cursor=not-a-cursor", "cursor=" + store.EncodeCursor("not-a-uuid"), "publish=maybe"} {
			rr := serve(r, as(callerOfA, http.MethodGet, "/locations?"+query, ""))
			require.Equal(t, http.StatusBadRequest, rr.Code, query)
		}
	})
}

func TestGetAssetLocationWithItsEvses(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	ofA := m.assetsOf(t, orgA, stationOfA)
	require.NoError(t, m.engine.SetConnectorStatus(ctx, stationOfA, 1, &store.ConnectorStatus{ChargeStationId: stationOfA, ConnectorId: 1, Status: store.ConnectorStatusCharging}))

	rr := serve(r, as(callerOfA, http.MethodGet, ofA.locationPath()+"?expand=evses", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	got := decode[api.AssetLocation](t, rr)
	require.NotNil(t, got.Evses)
	require.Len(t, *got.Evses, 1)
	evse := (*got.Evses)[0]
	assert.Equal(t, ofA.evse.Id, evse.Uid)
	assert.Equal(t, api.AssetEvseLiveStatusCHARGING, evse.LiveStatus)
	assert.Len(t, evse.Connectors, 2)

	rr = serve(r, as(callerOfA, http.MethodGet, ofA.locationPath()+"/evses", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, *got.Evses, decode[[]api.AssetEvse](t, rr))

	rr = serve(r, as(callerOfA, http.MethodGet, ofA.locationPath()+"?expand=connectors", ""))
	require.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

	// a location without EVSEs has an empty list
	rr = serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, locationInput(partyOfA, "Empty"))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	empty := decode[api.AssetLocation](t, rr)
	rr = serve(r, as(callerOfA, http.MethodGet, "/locations/"+empty.Id+"?expand=evses", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"evses":[]`)
	rr = serve(r, as(callerOfA, http.MethodGet, "/locations/"+empty.Id+"/evses", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `[]`, rr.Body.String())
}

func TestGetAssetLocationOcpi(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	ofA := m.assetsOf(t, orgA, stationOfA)
	require.NoError(t, m.engine.SetConnectorStatus(ctx, stationOfA, 1, &store.ConnectorStatus{ChargeStationId: stationOfA, ConnectorId: 1, Status: store.ConnectorStatusAvailable}))

	rr := serve(r, as(callerOfA, http.MethodGet, ofA.locationPath()+"/ocpi", ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got struct {
		CountryCode string `json:"country_code"`
		PartyId     string `json:"party_id"`
		Id          string `json:"id"`
		Publish     bool   `json:"publish"`
		Evses       []struct {
			Uid        string `json:"uid"`
			Status     string `json:"status"`
			Connectors []struct {
				Id string `json:"id"`
			} `json:"connectors"`
		} `json:"evses"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "BE", got.CountryCode)
	assert.Equal(t, "AAA", got.PartyId)
	assert.Equal(t, ofA.location.OcpiId, got.Id)
	assert.True(t, got.Publish)
	require.Len(t, got.Evses, 1)
	assert.Equal(t, ofA.evse.Id, got.Evses[0].Uid)
	assert.Equal(t, "AVAILABLE", got.Evses[0].Status)
	assert.Len(t, got.Evses[0].Connectors, 2)

	t.Run("a party that the tenant no longer has", func(t *testing.T) {
		tenant, err := m.engine.GetTenant(ctx, orgA)
		require.NoError(t, err)
		tenant.Parties = nil
		require.NoError(t, m.engine.PutTenant(ctx, tenant))

		rr := serve(r, as(callerOfA, http.MethodGet, ofA.locationPath()+"/ocpi", ""))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"unknown_party: the party of the location is not a party of its tenant"}`, rr.Body.String())
	})
}

func TestAssetLocationEvents(t *testing.T) {
	m := newMultiTenant(t)
	r, fixture := m.withEvents(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)

	rr := serve(r, as(callerOfA, http.MethodPost, "/locations", jsonBody(t, locationInput(partyOfA, "Evented"))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	loc := decode[api.AssetLocation](t, rr)

	changed := locationInput(partyOfA, "Evented")
	changed.Publish = ptr(false)
	rr = serve(r, as(callerOfA, http.MethodPut, "/locations/"+loc.Id, jsonBody(t, changed)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodPut, "/locations/"+loc.Id, jsonBody(t, locationInput(partyOfA, "Renamed"))))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodDelete, "/locations/"+loc.Id, ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	evs := fixture.Take(t)
	require.Len(t, evs, 4)
	want := []string{
		`{"location_id":"%[1]s","ocpi_id":"%[1]s","party_ref":"cpo-a","change":"created","publish":true,"publish_changed":false}`,
		`{"location_id":"%[1]s","ocpi_id":"%[1]s","party_ref":"cpo-a","change":"updated","publish":false,"publish_changed":true}`,
		`{"location_id":"%[1]s","ocpi_id":"%[1]s","party_ref":"cpo-a","change":"updated","publish":false,"publish_changed":false}`,
		`{"location_id":"%[1]s","ocpi_id":"%[1]s","party_ref":"cpo-a","change":"deleted","publish":false,"publish_changed":false}`,
	}
	for i, ev := range evs {
		assert.Equal(t, events.TypeLocationUpdated, ev.Type)
		assert.Equal(t, orgA, ev.OrgId)
		assert.Equal(t, "urn:cpms:"+orgA+":location/"+loc.Id, ev.Subject)
		assert.Equal(t, "user:test", ev.Actor)
		assert.JSONEq(t, fmt.Sprintf(want[i], loc.Id), string(ev.Data), i)
	}

	t.Run("nothing is emitted for what is refused", func(t *testing.T) {
		ofA := m.assetsOf(t, orgA, stationOfA)
		for _, req := range []*http.Request{
			as(callerOfA, http.MethodPost, "/locations", `{"party_ref":"cpo-a"}`),
			as(callerOfA, http.MethodDelete, ofA.locationPath(), ""),
			as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPut, ofA.locationPath(), jsonBody(t, locationInput(partyOfB, "B"))),
		} {
			rr := serve(r, req)
			require.GreaterOrEqual(t, rr.Code, 400, rr.Body.String())
		}
		assert.Empty(t, fixture.Take(t))
	})
}

// In single mode the assets belong to the default org, which is not a tenant: the party of
// a location is not checked and an EVSE needs its eMI3 id. The requests pass the
// validation against the API spec.
func TestAssetsInSingleMode(t *testing.T) {
	_, r, engine, _ := setupServer(t)
	registerStation(t, engine, "CS.001")
	call := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		return serve(r, req)
	}

	rr := call(http.MethodPost, "/locations", jsonBody(t, locationInput("any-party", "Single")))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	loc := decode[api.AssetLocation](t, rr)
	assert.Equal(t, testOrgId, loc.OrgId)

	rr = call(http.MethodPost, "/locations/"+loc.Id+"/evses", jsonBody(t, evseInput("CS.001", 1, 1)))
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"evse_id","reason":"is required: the party of the location is not a party of its tenant"}]}`, rr.Body.String())

	in := evseInput("CS.001", 1, 1)
	in.EvseId = ptr("GB*TWK*ECS001*1")
	rr = call(http.MethodPost, "/locations/"+loc.Id+"/evses", jsonBody(t, in))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Equal(t, "CS.001", decode[api.AssetEvse](t, rr).CsId, "the station key is the station id")

	rr = call(http.MethodGet, "/locations/"+loc.Id+"/evses", "")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Len(t, decode[[]api.AssetEvse](t, rr), 1)

	rr = call(http.MethodPut, "/locations/"+loc.Id, jsonBody(t, locationInput("any-party", "Renamed")))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, ptr("Renamed"), decode[api.AssetLocation](t, rr).Name)
}
