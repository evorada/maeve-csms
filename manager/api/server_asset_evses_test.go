// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// createLocation creates a location of the party through the API.
func createLocation(t *testing.T, r http.Handler, caller *tenancy.Principal, partyRef string) api.AssetLocation {
	t.Helper()
	rr := serve(r, as(caller, http.MethodPost, "/locations", jsonBody(t, locationInput(partyRef, "Location"))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	return decode[api.AssetLocation](t, rr)
}

func TestAssetEvseLifecycle(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	loc := createLocation(t, r, callerOfA, partyOfA)

	in := evseInput(stationOfA, 1, 1, 2)
	in.PhysicalReference = ptr("A1")
	(*in.Connectors)[1].OcpiId = ptr("2b")
	(*in.Connectors)[1].MaxElectricPower = ptr(int32(22000))
	(*in.Connectors)[1].TariffRef = ptr("0193b2c6-0000-7000-8000-000000000001")
	rr := serve(r, as(callerOfA, http.MethodPost, "/locations/"+loc.Id+"/evses", jsonBody(t, in)))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	created := decode[api.AssetEvse](t, rr)
	assert.Equal(t, "/api/v0/evses/"+created.Uid, rr.Header().Get("Location"))
	assert.Equal(t, `"1"`, rr.Header().Get("ETag"))
	assert.Equal(t, loc.Id, created.LocationId)
	assert.Equal(t, stationOfA, created.CsId)
	assert.Equal(t, 1, created.OcppEvseId)
	// the eMI3 id of the party of the location and the station id
	assert.Equal(t, ptr("BE*AAA*ECP001*1"), created.EvseId)
	assert.Equal(t, ptr("A1"), created.PhysicalReference)
	assert.Equal(t, api.AssetEvseStatusACTIVE, created.Status)
	assert.Equal(t, api.AssetEvseLiveStatusUNKNOWN, created.LiveStatus)
	assert.Equal(t, int64(1), created.Version)
	require.Len(t, created.Connectors, 2)
	assert.Equal(t, "1", created.Connectors[0].OcpiId)
	assert.Equal(t, created.Uid, created.Connectors[0].EvseUid)
	assert.Equal(t, "2b", created.Connectors[1].OcpiId)
	assert.Equal(t, ptr(int32(22000)), created.Connectors[1].MaxElectricPower)
	assert.Equal(t, ptr("0193b2c6-0000-7000-8000-000000000001"), created.Connectors[1].TariffRef)
	assert.Nil(t, created.Connectors[1].AdhocTariffRef)

	t.Run("the charge station is at the location of its first EVSE", func(t *testing.T) {
		cs, err := m.engine.LookupChargeStation(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, &loc.Id, cs.LocationId)

		// a second location does not take it
		other := createLocation(t, r, callerOfA, partyOfA)
		rr := serve(r, as(callerOfA, http.MethodPost, "/locations/"+other.Id+"/evses", jsonBody(t, evseInput(stationOfA, 2, 1))))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		cs, err = m.engine.LookupChargeStation(ctx, stationOfA)
		require.NoError(t, err)
		assert.Equal(t, &loc.Id, cs.LocationId)
	})

	t.Run("read", func(t *testing.T) {
		require.NoError(t, m.engine.SetConnectorStatus(ctx, stationOfA, 1, &store.ConnectorStatus{ChargeStationId: stationOfA, ConnectorId: 1, Status: store.ConnectorStatusFaulted}))
		rr := serve(r, as(callerOfA, http.MethodGet, "/evses/"+created.Uid, ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"1"`, rr.Header().Get("ETag"))
		got := decode[api.AssetEvse](t, rr)
		assert.Equal(t, api.AssetEvseLiveStatusOUTOFORDER, got.LiveStatus)
		got.LiveStatus = created.LiveStatus
		assert.Equal(t, created, got)

		rr = serve(r, as(callerOfA, http.MethodGet, "/locations/"+loc.Id+"/evses", ""))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		evses := decode[[]api.AssetEvse](t, rr)
		require.Len(t, evses, 1)
		assert.Equal(t, created.Uid, evses[0].Uid)
	})

	t.Run("change", func(t *testing.T) {
		changed := evseInput(stationOfA, 3, 9)
		req := as(callerOfA, http.MethodPut, "/evses/"+created.Uid, jsonBody(t, changed))
		req.Header.Set("If-Match", `"1"`)
		rr := serve(r, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"2"`, rr.Header().Get("ETag"))
		got := decode[api.AssetEvse](t, rr)
		assert.Equal(t, 3, got.OcppEvseId)
		// the eMI3 id and the status keep their value, the physical reference is cleared
		assert.Equal(t, created.EvseId, got.EvseId)
		assert.Equal(t, api.AssetEvseStatusACTIVE, got.Status)
		assert.Nil(t, got.PhysicalReference)
		// the connectors are not changed by a change of the EVSE
		assert.Equal(t, created.Connectors, got.Connectors)

		changed.Status = ptr(api.AssetEvseStatusPLANNED)
		changed.EvseId = ptr("BE*AAA*EPLANNED*3")
		rr = serve(r, as(callerOfA, http.MethodPut, "/evses/"+created.Uid, jsonBody(t, changed)))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		got = decode[api.AssetEvse](t, rr)
		assert.Equal(t, api.AssetEvseStatusPLANNED, got.Status)
		assert.Equal(t, api.AssetEvseLiveStatusPLANNED, got.LiveStatus)
		assert.Equal(t, ptr("BE*AAA*EPLANNED*3"), got.EvseId)
	})

	t.Run("a change against an older version", func(t *testing.T) {
		req := as(callerOfA, http.MethodPut, "/evses/"+created.Uid, jsonBody(t, evseInput(stationOfA, 4)))
		req.Header.Set("If-Match", `"1"`)
		rr := serve(r, req)
		require.Equal(t, http.StatusPreconditionFailed, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Precondition Failed","error":"version_conflict: the EVSE has version 3"}`, rr.Body.String())
		evse, err := m.engine.LookupAssetEvse(ctx, created.Uid)
		require.NoError(t, err)
		assert.Equal(t, 3, evse.OcppEvseId)
	})

	t.Run("delete", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodDelete, "/evses/"+created.Uid, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			rr = serve(r, as(callerOfA, method, "/evses/"+created.Uid, ""))
			require.Equal(t, http.StatusNotFound, rr.Code, method)
		}
		c, err := m.engine.LookupAssetConnector(ctx, created.Connectors[0].Uid)
		require.NoError(t, err)
		assert.Nil(t, c, "the connectors are deleted with the EVSE")
	})
}

func TestCreateAssetEvseRefusals(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	loc := createLocation(t, r, callerOfA, partyOfA)
	evsesPath := "/locations/" + loc.Id + "/evses"
	for key, state := range map[string]store.StationState{"aaaa.PENDING": store.StationStatePending, "aaaa.GONE": store.StationStateDecommissioned} {
		require.NoError(t, m.engine.SetChargeStationAuth(ctx, key, &store.ChargeStationAuth{OrgId: orgA}))
		require.NoError(t, m.engine.SetChargeStationState(ctx, key, state, nil))
	}

	t.Run("a charge station that waits for approval or is decommissioned", func(t *testing.T) {
		for key, want := range map[string]string{
			"aaaa.PENDING": "station_pending: the charge station waits for approval",
			"aaaa.GONE":    "station_decommissioned: the charge station is decommissioned",
		} {
			rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, evseInput(key, 1, 1))))
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.JSONEq(t, `{"status":"Conflict","error":"`+want+`"}`, rr.Body.String())
		}
	})

	t.Run("a charge station of another org, or none", func(t *testing.T) {
		var answers []string
		for _, key := range []string{stationOfB, "aaaa.UNKNOWN", "zzzz.UNKNOWN"} {
			rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, evseInput(key, 1, 1))))
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
			answers = append(answers, rr.Body.String())
		}
		assert.JSONEq(t, `{"status":"invalid","errors":[{"field":"cs_id","reason":"is not a charge station of the org of the location"}]}`, answers[0])
		assert.Equal(t, answers[0], answers[1], "nothing is learnt about the charge stations of other orgs")
		assert.Equal(t, answers[0], answers[2])
	})

	t.Run("fields that break a rule", func(t *testing.T) {
		noConnectors := evseInput(stationOfA, 1)
		badConnector := evseInput(stationOfA, 1, 1, 1)
		(*badConnector.Connectors)[0].Format = "PLUG"
		(*badConnector.Connectors)[1].AdhocTariffRef = ptr("tariff-1")
		badEvse := evseInput("", 0, 1)
		badEvse.EvseId = ptr("not an eMI3 id")
		for name, test := range map[string]struct {
			in         api.AssetEvseInput
			wantFields []string
		}{
			"no connectors":                 {noConnectors, []string{"connectors"}},
			"connectors that break a rule":  {badConnector, []string{"connectors[0].format", "connectors[1].ocpp_connector_id", "connectors[1].adhoc_tariff_ref"}},
			"an EVSE that breaks the rules": {badEvse, []string{"ocpp_evse_id", "evse_id", "cs_id"}},
		} {
			rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, test.in)))
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, "%s: %s", name, rr.Body.String())
			var fields []string
			for _, e := range decode[api.ValidationErrors](t, rr).Errors {
				fields = append(fields, e.Field)
			}
			assert.Equal(t, test.wantFields, fields, name)
		}
	})

	t.Run("an OCPP EVSE id that the charge station has, or an eMI3 id that is taken", func(t *testing.T) {
		in := evseInput(stationOfA, 1, 1)
		in.EvseId = ptr("BE*AAA*EOWN*1")
		rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, in)))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		assert.Equal(t, ptr("BE*AAA*EOWN*1"), decode[api.AssetEvse](t, rr).EvseId, "a given eMI3 id is kept")

		for _, in := range []api.AssetEvseInput{evseInput(stationOfA, 1, 1), in} {
			if in.EvseId != nil {
				in.OcppEvseId = 2
			}
			rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, in)))
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.Contains(t, rr.Body.String(), `"error":"already_exists: `)
		}
	})

	t.Run("no eMI3 id for a location whose party is not known", func(t *testing.T) {
		tenant, err := m.engine.GetTenant(ctx, orgA)
		require.NoError(t, err)
		tenant.Parties = nil
		require.NoError(t, m.engine.PutTenant(ctx, tenant))
		defer func() {
			tenant.Parties = []store.TenantParty{{Id: partyOfA, Role: "CPO", CountryCode: "BE", PartyId: "AAA"}}
			require.NoError(t, m.engine.PutTenant(ctx, tenant))
		}()

		rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, evseInput(stationOfA, 5, 1))))
		require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"field":"evse_id"`)

		in := evseInput(stationOfA, 5, 1)
		in.EvseId = ptr("BE*AAA*EGIVEN*5")
		rr = serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, in)))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	})

	t.Run("a change to a charge station that cannot have the EVSE", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodPost, evsesPath, jsonBody(t, evseInput(stationOfA, 6, 1))))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		evse := decode[api.AssetEvse](t, rr)
		for key, want := range map[string]int{"aaaa.PENDING": http.StatusConflict, stationOfB: http.StatusUnprocessableEntity} {
			rr := serve(r, as(callerOfA, http.MethodPut, "/evses/"+evse.Uid, jsonBody(t, evseInput(key, 6))))
			require.Equal(t, want, rr.Code, rr.Body.String())
		}
		// a charge station of the org that is active
		require.NoError(t, m.engine.SetChargeStationAuth(ctx, "aaaa.CP002", &store.ChargeStationAuth{OrgId: orgA}))
		rr = serve(r, as(callerOfA, http.MethodPut, "/evses/"+evse.Uid, jsonBody(t, evseInput("aaaa.CP002", 6))))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, "aaaa.CP002", decode[api.AssetEvse](t, rr).CsId)
	})

	evses, err := m.engine.ListAssetEvsesForStation(ctx, "aaaa.PENDING")
	require.NoError(t, err)
	assert.Empty(t, evses)
}

func TestAssetConnectorLifecycle(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	ofA := m.assetsOf(t, orgA, stationOfA)
	connectorsPath := ofA.evsePath() + "/connectors"

	rr := serve(r, as(callerOfA, http.MethodPost, connectorsPath, jsonBody(t, connectorInput(3))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	assert.Equal(t, `"1"`, rr.Header().Get("ETag"))
	created := decode[api.AssetConnector](t, rr)
	assert.JSONEq(t, fmt.Sprintf(`{
		"uid": %q, "evse_uid": %q, "ocpp_connector_id": 3, "ocpi_id": "3", "standard": "IEC_62196_T2",
		"format": "SOCKET", "power_type": "AC_3_PHASE", "max_voltage": 400, "max_amperage": 32,
		"max_electric_power": null, "tariff_ref": null, "adhoc_tariff_ref": null, "version": 1,
		"created_at": %q, "updated_at": %q
	}`, created.Uid, ofA.evse.Id, m.clock.Now().UTC().Format("2006-01-02T15:04:05Z"), m.clock.Now().UTC().Format("2006-01-02T15:04:05Z")), rr.Body.String())
	connectorPath := connectorsPath + "/" + created.Uid

	t.Run("an OCPP or OCPI id that the EVSE has", func(t *testing.T) {
		taken := connectorInput(1)
		rr := serve(r, as(callerOfA, http.MethodPost, connectorsPath, jsonBody(t, taken)))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		taken = connectorInput(4)
		taken.OcpiId = ptr("2")
		rr = serve(r, as(callerOfA, http.MethodPost, connectorsPath, jsonBody(t, taken)))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		rr = serve(r, as(callerOfA, http.MethodPut, connectorPath, jsonBody(t, taken)))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	})

	t.Run("fields that break a rule", func(t *testing.T) {
		bad := connectorInput(0)
		bad.PowerType = "AC"
		bad.TariffRef = ptr("not-a-uuid")
		for _, req := range []*http.Request{
			as(callerOfA, http.MethodPost, connectorsPath, jsonBody(t, bad)),
			as(callerOfA, http.MethodPut, connectorPath, jsonBody(t, bad)),
		} {
			rr := serve(r, req)
			require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
			var fields []string
			for _, e := range decode[api.ValidationErrors](t, rr).Errors {
				fields = append(fields, e.Field)
			}
			assert.Equal(t, []string{"ocpp_connector_id", "power_type", "tariff_ref"}, fields)
		}
	})

	t.Run("change", func(t *testing.T) {
		changed := connectorInput(3)
		changed.Format = "CABLE"
		changed.MaxElectricPower = ptr(int32(11000))
		req := as(callerOfA, http.MethodPut, connectorPath, jsonBody(t, changed))
		req.Header.Set("If-Match", `"1"`)
		rr := serve(r, req)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, `"2"`, rr.Header().Get("ETag"))
		got := decode[api.AssetConnector](t, rr)
		assert.Equal(t, "CABLE", got.Format)
		assert.Equal(t, ptr(int32(11000)), got.MaxElectricPower)
		assert.Equal(t, "3", got.OcpiId, "kept when the body leaves it out")

		req = as(callerOfA, http.MethodPut, connectorPath, jsonBody(t, connectorInput(3)))
		req.Header.Set("If-Match", `"1"`)
		rr = serve(r, req)
		require.Equal(t, http.StatusPreconditionFailed, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Precondition Failed","error":"version_conflict: the connector has version 2"}`, rr.Body.String())
		stored, err := m.engine.LookupAssetConnector(ctx, created.Uid)
		require.NoError(t, err)
		assert.Equal(t, "CABLE", stored.Format)
	})

	t.Run("a connector of another EVSE", func(t *testing.T) {
		other := createLocation(t, r, callerOfA, partyOfA)
		rr := serve(r, as(callerOfA, http.MethodPost, "/locations/"+other.Id+"/evses", jsonBody(t, evseInput(stationOfA, 7, 1))))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		otherEvse := decode[api.AssetEvse](t, rr)
		wrongPath := "/evses/" + otherEvse.Uid + "/connectors/" + created.Uid
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			rr := serve(r, as(callerOfA, method, wrongPath, jsonBody(t, connectorInput(3))))
			require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		rr := serve(r, as(callerOfA, http.MethodDelete, connectorPath, ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		rr = serve(r, as(callerOfA, http.MethodDelete, connectorPath, ""))
		require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())

		rr = serve(r, as(callerOfA, http.MethodDelete, ofA.connectorPath(), ""))
		require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		// the last connector of the EVSE stays
		rr = serve(r, as(callerOfA, http.MethodDelete, connectorsPath+"/"+ofA.evse.Connectors[0].Id, ""))
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Conflict","error":"last_connector: the EVSE needs a connector; delete the EVSE instead"}`, rr.Body.String())
		evse, err := m.engine.LookupAssetEvse(ctx, ofA.evse.Id)
		require.NoError(t, err)
		assert.Len(t, evse.Connectors, 1)
	})
}

// An EVSE, or a connector of it, is not deleted while a transaction runs on it.
func TestAssetEvseWithATransaction(t *testing.T) {
	ctx := context.Background()
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	const conflict = `{"status":"Conflict","error":"active_transaction: a transaction is running on the EVSE"}`

	t.Run("a transaction that has not ended", func(t *testing.T) {
		m := newMultiTenant(t)
		r := m.router(t)
		ofA := m.assetsOf(t, orgA, stationOfA)
		// a transaction on another connector does not count
		require.NoError(t, m.engine.CreateTransaction(ctx, stationOfA, "tx-2", "TAG", "ISO14443", nil, 0, false))
		require.NoError(t, m.engine.UpdateTransactionDetails(ctx, stationOfA, "tx-2", &store.TransactionDetails{ConnectorId: ptr(2)}))
		require.NoError(t, m.engine.CreateTransaction(ctx, stationOfA, "tx-1", "TAG", "ISO14443", nil, 0, false))
		require.NoError(t, m.engine.UpdateTransactionDetails(ctx, stationOfA, "tx-1", &store.TransactionDetails{ConnectorId: ptr(1)}))

		for _, path := range []string{ofA.connectorPath(), ofA.evsePath()} {
			rr := serve(r, as(callerOfA, http.MethodDelete, path, ""))
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.JSONEq(t, conflict, rr.Body.String())
		}
		ofA.assertUnchanged(t)

		require.NoError(t, m.engine.EndTransaction(ctx, stationOfA, "tx-1", "TAG", "ISO14443", nil, 1))
		for _, path := range []string{ofA.connectorPath(), ofA.evsePath()} {
			rr := serve(r, as(callerOfA, http.MethodDelete, path, ""))
			require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
		}
	})

	t.Run("a current transaction in the status of the connector", func(t *testing.T) {
		m := newMultiTenant(t)
		r := m.router(t)
		ofA := m.assetsOf(t, orgA, stationOfA)
		require.NoError(t, m.engine.SetConnectorStatus(ctx, stationOfA, 1, &store.ConnectorStatus{
			ChargeStationId: stationOfA, ConnectorId: 1, Status: store.ConnectorStatusCharging, CurrentTransactionId: ptr("tx-1"),
		}))

		for _, path := range []string{ofA.connectorPath(), ofA.evsePath()} {
			rr := serve(r, as(callerOfA, http.MethodDelete, path, ""))
			require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
			assert.JSONEq(t, conflict, rr.Body.String())
		}
		ofA.assertUnchanged(t)
	})
}

func TestAssetEvseEvents(t *testing.T) {
	m := newMultiTenant(t)
	r, fixture := m.withEvents(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	loc := createLocation(t, r, callerOfA, partyOfA)
	fixture.Take(t)

	rr := serve(r, as(callerOfA, http.MethodPost, "/locations/"+loc.Id+"/evses", jsonBody(t, evseInput(stationOfA, 1, 1, 2))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	evse := decode[api.AssetEvse](t, rr)
	rr = serve(r, as(callerOfA, http.MethodPut, "/evses/"+evse.Uid, jsonBody(t, evseInput(stationOfA, 1))))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodPost, "/evses/"+evse.Uid+"/connectors", jsonBody(t, connectorInput(3))))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	connector := decode[api.AssetConnector](t, rr)
	connectorPath := "/evses/" + evse.Uid + "/connectors/" + connector.Uid
	rr = serve(r, as(callerOfA, http.MethodPut, connectorPath, jsonBody(t, connectorInput(3))))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodDelete, connectorPath, ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())
	rr = serve(r, as(callerOfA, http.MethodDelete, "/evses/"+evse.Uid, ""))
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	evs := fixture.Take(t)
	require.Len(t, evs, 6)
	location := fmt.Sprintf(`"location_id":%q,"ocpi_id":%q,"party_ref":"cpo-a"`, loc.Id, loc.OcpiId)
	evseUid := fmt.Sprintf(`"evse_uid":%q`, evse.Uid)
	connectorUid := fmt.Sprintf(`"connector_uid":%q`, connector.Uid)
	publish := `"publish":true,"publish_changed":false`
	want := []string{
		`{` + location + `,"change":"created",` + evseUid + `,` + publish + `}`,
		`{` + location + `,"change":"updated",` + evseUid + `,` + publish + `}`,
		`{` + location + `,"change":"created",` + evseUid + `,` + connectorUid + `,` + publish + `}`,
		`{` + location + `,"change":"updated",` + evseUid + `,` + connectorUid + `,` + publish + `}`,
		`{` + location + `,"change":"deleted",` + evseUid + `,` + connectorUid + `,` + publish + `}`,
		`{` + location + `,"change":"deleted",` + evseUid + `,` + publish + `}`,
	}
	for i, ev := range evs {
		assert.Equal(t, events.TypeLocationUpdated, ev.Type)
		assert.Equal(t, orgA, ev.OrgId)
		assert.Equal(t, "urn:cpms:"+orgA+":location/"+loc.Id, ev.Subject)
		assert.Equal(t, "user:test", ev.Actor)
		assert.JSONEq(t, want[i], string(ev.Data), i)
	}

	t.Run("nothing is emitted for what is refused", func(t *testing.T) {
		ofA := m.assetsOf(t, orgA, stationOfA)
		require.NoError(t, m.engine.SetConnectorStatus(context.Background(), stationOfA, 1, &store.ConnectorStatus{
			ChargeStationId: stationOfA, ConnectorId: 1, CurrentTransactionId: ptr("tx-1"),
		}))
		for _, req := range []*http.Request{
			as(callerOfA, http.MethodPost, ofA.locationPath()+"/evses", jsonBody(t, evseInput(stationOfA, 1, 1))),
			as(callerOfA, http.MethodDelete, ofA.evsePath(), ""),
			as(callerOfA, http.MethodPost, ofA.evsePath()+"/connectors", jsonBody(t, connectorInput(1))),
			as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPut, ofA.connectorPath(), jsonBody(t, connectorInput(2))),
		} {
			rr := serve(r, req)
			require.GreaterOrEqual(t, rr.Code, 400, rr.Body.String())
		}
		assert.Empty(t, fixture.Take(t))
	})
}
