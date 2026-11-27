// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func locationBody(name, orgId string) string {
	org := ""
	if orgId != "" {
		org = fmt.Sprintf(`"orgId": %q,`, orgId)
	}
	return fmt.Sprintf(`{
  %s
  "name": %q,
  "address": "F.Rooseveltlaan 3A",
  "city": "Gent",
  "party_id": "TWK",
  "postal_code": "9000",
  "country": "BEL",
  "country_code": "BEL",
  "coordinates": {"latitude": "51.047599", "longitude": "3.729944"},
  "parking_type": "ON_STREET"
}`, org, name)
}

func TestRegisterLocationOnlyWritesTheOrgsOfTheCaller(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	callerOfA := callerOf(orgA, tenancy.OrgScopeSelf)
	callerOfB := callerOf(orgB, tenancy.OrgScopeSelf)
	partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)

	rr := serve(r, as(callerOfA, http.MethodPost, "/location/loc001", locationBody("Location of A", "")))
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	ofA, err := m.engine.LookupLocation(ctx, "loc001")
	require.NoError(t, err)
	require.NotNil(t, ofA)
	require.Equal(t, orgA, ofA.OrgId)
	require.Equal(t, "Location of A", ofA.Name)

	t.Run("a location id of another org is a conflict and nothing changes", func(t *testing.T) {
		for name, req := range map[string]*http.Request{
			"another tenant":                    as(callerOfB, http.MethodPost, "/location/loc001", locationBody("Taken over by B", "")),
			"a partner that names another org":  as(partner, http.MethodPost, "/location/loc001", locationBody("Moved to B", orgB)),
			"a tenant that is not below at all": as(callerOf(orgC, tenancy.OrgScopeSubtree), http.MethodPost, "/location/loc001", locationBody("Taken over by C", "")),
		} {
			rr := serve(r, req)
			require.Equal(t, http.StatusConflict, rr.Code, name)
			// the answer does not say whose location it is
			assert.JSONEq(t, `{"status":"Conflict","error":"location id already in use"}`, rr.Body.String(), name)

			got, err := m.engine.LookupLocation(ctx, "loc001")
			require.NoError(t, err)
			assert.Equal(t, ofA, got, name)
		}
	})

	t.Run("the owner updates its location", func(t *testing.T) {
		rr := serve(r, as(partner, http.MethodPost, "/location/loc001", locationBody("Renamed by the partner", orgA)))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupLocation(ctx, "loc001")
		require.NoError(t, err)
		assert.Equal(t, orgA, got.OrgId)
		assert.Equal(t, "Renamed by the partner", got.Name)
	})

	t.Run("an org outside the scope of the caller", func(t *testing.T) {
		for name, req := range map[string]*http.Request{
			"a tenant names another org":               as(callerOfA, http.MethodPost, "/location/loc002", locationBody("x", orgB)),
			"a partner names an org that is not below": as(partner, http.MethodPost, "/location/loc002", locationBody("x", orgC)),
			"a partner names itself":                   as(partner, http.MethodPost, "/location/loc002", locationBody("x", orgPartner)),
			// also for an id that is taken: the org is checked first
			"a tenant names another org, id in use": as(callerOfB, http.MethodPost, "/location/loc001", locationBody("x", orgA)),
		} {
			rr := serve(r, req)
			require.Equal(t, http.StatusNotFound, rr.Code, name)
			assert.JSONEq(t, notFoundBody, rr.Body.String(), name)
		}

		rr := serve(r, as(partner, http.MethodPost, "/location/loc002", locationBody("x", "")))
		require.Equal(t, http.StatusBadRequest, rr.Code)
		assert.JSONEq(t, `{"status":"Bad Request","error":"orgId is required"}`, rr.Body.String())

		got, err := m.engine.LookupLocation(ctx, "loc002")
		require.NoError(t, err)
		assert.Nil(t, got, "nothing is stored")
	})

	t.Run("a partner creates a location in an org below", func(t *testing.T) {
		rr := serve(r, as(partner, http.MethodPost, "/location/loc003", locationBody("Location of B", orgB)))
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		got, err := m.engine.LookupLocation(ctx, "loc003")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgB, got.OrgId)
	})

	t.Run("without the permission", func(t *testing.T) {
		rr := serve(r, as(withPerms(callerOf(orgB, tenancy.OrgScopeSelf), tenancy.PermStationRead), http.MethodPost, "/location/loc001", locationBody("x", "")))
		require.Equal(t, http.StatusForbidden, rr.Code)
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.station.write"}`, rr.Body.String())
	})
}

// Reservation ids are shared by all charge stations: a tenant cannot replace the
// reservation of a charge station of another tenant by using its id.
func TestCreateReservationDoesNotReplaceAReservationOfAnotherChargeStation(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	r := m.router(t)
	const body = `{"reservationId":7,"connectorId":1,"idTag":"TAGOF%s","expiryDate":"2026-01-02T05:00:00Z"}`

	rr := serve(r, as(callerOf(orgA, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+stationOfA+"/reservation", fmt.Sprintf(body, "A")))
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	ofA, err := m.engine.GetReservation(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, ofA)
	require.Equal(t, stationOfA, ofA.ChargeStationId)

	rr = serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodPost, "/cs/"+stationOfB+"/reservation", fmt.Sprintf(body, "B")))
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"status":"Conflict","error":"reservation id already in use"}`, rr.Body.String())

	got, err := m.engine.GetReservation(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, ofA, got)

	// and it cannot cancel it through its own charge station
	rr = serve(r, as(callerOf(orgB, tenancy.OrgScopeSelf), http.MethodDelete, "/cs/"+stationOfB+"/reservation/7", ""))
	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.JSONEq(t, notFoundBody, rr.Body.String())
	got, err = m.engine.GetReservation(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, ofA, got)
}
