// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// The command operations through the handler of the API listener, with the tokens of
// the platform: the route of the cancel action has a ":" after the command id.
func TestCommandsThroughTheApiHandler(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey}))
		require.NoError(t, engine.SetChargeStationAuth(ctx, orgKey+".CP001", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgId}))
		require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, orgKey+".CP001", &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	}
	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	domainEvents := eventsOf(engine, resolver)
	// without a dispatcher the commands stay queued
	service := &commands.Service{
		Store:          engine,
		Tx:             engine,
		Events:         domainEvents,
		RuntimeDetails: engine,
		SchemaFS:       schemas.OcppSchemas,
		Clock:          clock.RealClock{},
	}
	handler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   domainEvents,
		Commands: service,
	})
	tokenOfA := tokenOf(platform, orgA, tenancy.PermConnectorCommand, tenancy.PermStationRead)
	tokenOfB := tokenOf(platform, orgB, tenancy.AllPermissions...)

	rr := request(handler, http.MethodPost, "/api/v0/cs/"+stationOfA+"/commands", tokenOfA, `{"action":"Reset","payload":{"type":"Soft"}}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	var queued struct {
		Id              string `json:"id"`
		ChargeStationId string `json:"chargeStationId"`
		Status          string `json:"status"`
		Actor           string `json:"actor"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &queued))
	location := rr.Header().Get("Location")
	assert.Equal(t, "/api/v0/commands/"+queued.Id, location)
	assert.Equal(t, "CP001", queued.ChargeStationId)
	assert.Equal(t, "queued", queued.Status)
	// the caller and the token, from the token of the platform
	assert.Equal(t, "user:alice", queued.Actor)
	stored, err := engine.GetCommand(ctx, queued.Id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.Jti)
	assert.Equal(t, "token-1", *stored.Jti)
	assert.Equal(t, orgA, stored.OrgId)

	t.Run("the Location of the command", func(t *testing.T) {
		rr := get(handler, location, tokenOfA)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"id":"`+queued.Id+`"`)
	})

	t.Run("another tenant", func(t *testing.T) {
		unknown := "/api/v0/commands/0193b2c4-0000-7000-8000-000000000000"
		for _, tc := range []struct{ method, target, unknown string }{
			{http.MethodGet, location, unknown},
			{http.MethodPost, location + ":cancel", unknown + ":cancel"},
		} {
			other := request(handler, tc.method, tc.target, tokenOfB, "")
			none := request(handler, tc.method, tc.unknown, tokenOfB, "")
			require.Equal(t, http.StatusNotFound, other.Code, "%s %s: %s", tc.method, tc.target, other.Body.String())
			assert.JSONEq(t, `{"status":"Not Found"}`, other.Body.String())
			assert.Equal(t, none.Code, other.Code)
			assert.Equal(t, none.Body.String(), other.Body.String())
		}
	})

	t.Run("without the permission to cancel", func(t *testing.T) {
		rr := request(handler, http.MethodPost, location+":cancel", tokenOf(platform, orgA, tenancy.PermStationRead), "")
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.JSONEq(t, `{"status":"Forbidden","error":"missing permission cpms.connector.command"}`, rr.Body.String())
	})

	t.Run("without a token", func(t *testing.T) {
		rr := request(handler, http.MethodPost, location+":cancel", "", "")
		require.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
	})

	// nothing above changed the command
	stored, err = engine.GetCommand(ctx, queued.Id)
	require.NoError(t, err)
	require.Equal(t, store.CommandStatusQueued, stored.Status)

	t.Run("cancel", func(t *testing.T) {
		rr := request(handler, http.MethodPost, location+":cancel", tokenOfA, "")
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"status":"cancelled"`)

		rr = request(handler, http.MethodPost, location+":cancel", tokenOfA, "")
		require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"status":"cancelled"`)
	})

	t.Run("the list of the charge station", func(t *testing.T) {
		rr := get(handler, "/api/v0/cs/"+stationOfA+"/commands?status=cancelled&action=Reset&limit=10", tokenOfA)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var list struct {
			Commands []struct {
				Id string `json:"id"`
			} `json:"commands"`
			Total int `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &list))
		require.Len(t, list.Commands, 1)
		assert.Equal(t, queued.Id, list.Commands[0].Id)
		assert.Equal(t, 1, list.Total)
	})
}

func TestCommandOperationsWithoutACommandQueueThroughTheApiHandler(t *testing.T) {
	platform, verifier := newPlatformVerifier(t)
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(context.Background(), "cs001", &store.ChargeStationAuth{OrgId: store.LegacyOrgId}))
	// the manager of a storage engine that does not keep commands
	handler := newApiHandler(engine, verifier)
	token := tokenOf(platform, store.LegacyOrgId, tenancy.AllPermissions...)

	for _, tc := range []struct{ method, target, body string }{
		{http.MethodPost, "/api/v0/cs/cs001/commands", `{"action":"Reset","payload":{"type":"Soft"}}`},
		{http.MethodGet, "/api/v0/cs/cs001/commands", ""},
		{http.MethodGet, "/api/v0/commands/0193b2c4-0000-7000-8000-000000000000", ""},
		{http.MethodPost, "/api/v0/commands/0193b2c4-0000-7000-8000-000000000000:cancel", ""},
	} {
		rr := request(handler, tc.method, tc.target, token, tc.body)
		assert.Equal(t, http.StatusNotImplemented, rr.Code, "%s %s: %s", tc.method, tc.target, rr.Body.String())
	}
}

// An operation that sends something to an OCPP 1.6 charge station queues a command with
// the caller and the token of the platform, through the handler of the API listener.
func TestTypedCommandOperationThroughTheApiHandler(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-aaaa", OrgKey: "aaaa"}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, stationOfA, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, stationOfA, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	platform, verifier := newPlatformVerifier(t)
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	domainEvents := eventsOf(engine, resolver)
	service := &commands.Service{
		Store:          engine,
		Tx:             engine,
		Events:         domainEvents,
		RuntimeDetails: engine,
		SchemaFS:       schemas.OcppSchemas,
		Clock:          clock.RealClock{},
	}
	handler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   domainEvents,
		Commands: service,
	})

	rr := request(handler, http.MethodPost, "/api/v0/cs/"+stationOfA+"/reset", tokenOf(platform, orgA, tenancy.PermConnectorCommand), `{"type":"Soft"}`)
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	var queued struct {
		Id     string `json:"id"`
		Action string `json:"action"`
		Actor  string `json:"actor"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &queued))
	assert.Equal(t, "/api/v0/commands/"+queued.Id, rr.Header().Get("Location"))
	assert.Equal(t, "Reset", queued.Action)
	assert.Equal(t, "user:alice", queued.Actor)
	stored, err := engine.GetCommand(ctx, queued.Id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.Jti)
	assert.Equal(t, "token-1", *stored.Jti)
	assert.Equal(t, orgA, stored.OrgId)
	reset, err := engine.GetResetRequest(ctx, stationOfA)
	require.NoError(t, err)
	assert.Nil(t, reset, "no reset request in the store")
}
