// SPDX-License-Identifier: Apache-2.0

package stations_test

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
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// A station that no operator registered connects to the host of a tenant, is admitted as
// pending, boots Pending and cannot charge; once it is active it boots Accepted and
// charges.
func TestOnboardingOfAStationThatIsNotRegistered(t *testing.T) {
	ctx := context.Background()
	const (
		orgId      = "0193b2c4-0000-7000-8000-0000000000a1"
		stationKey = "aaaa.CP777"
		card       = "MYRFIDCARD"
	)
	clk := clockTest.NewFakePassiveClock(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
	}))
	require.NoError(t, engine.SetToken(ctx, &store.Token{
		OrgId: orgId, CountryCode: "ES", PartyId: "CPO", Type: "RFID", Uid: card, ContractId: "ESCPO012345678V",
		Issuer: "CPO", Valid: true, CacheMode: store.CacheModeAlways,
	}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	internal := internalapi.NewHandler(resolver, engine)
	domainEvents := eventstest.New(engine, orgId, clk)
	boot := handlers.BootNotificationHandler{
		Clock: clk, RuntimeDetailsStore: engine, SettingsStore: engine, StatusStore: engine,
		RegistryStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events, HeartbeatInterval: 300,
		TenantConfig: &tenancy.ConfigProvider{Resolver: resolver},
	}
	authorize := handlers.AuthorizeHandler{TokenStore: engine, RegistryStore: engine, OrgResolver: resolver, Clock: clk}
	start := handlers.StartTransactionHandler{
		Clock: clk, TokenStore: engine, RegistryStore: engine, OrgResolver: resolver, TransactionStore: engine,
		ReservationStore: engine, Tx: domainEvents.Tx, Events: domainEvents.Events,
	}
	query := "?" + url.Values{"host": {"a.example"}, "stationId": {"CP777"}}.Encode()

	// the gateway looks the station up and records its connection
	rr := httptest.NewRecorder()
	internal.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/station-auth"+query, nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var auth internalapi.StationAuthResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &auth))
	assert.Equal(t, stationKey, auth.StationKey)
	require.NotNil(t, auth.Onboarding)
	assert.True(t, auth.Onboarding.OpenSP1)

	rr = httptest.NewRecorder()
	internal.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/station-onboarding"+query,
		strings.NewReader(`{"transport":"ws","credential":"open"}`)))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.JSONEq(t, `{"created":true}`, rr.Body.String())

	bootStation := func() *types.BootNotificationResponseJson {
		resp, err := boot.HandleCall(ctx, stationKey, &types.BootNotificationJson{ChargePointVendor: "Acme", ChargePointModel: "Volt 22"})
		require.NoError(t, err)
		return resp.(*types.BootNotificationResponseJson)
	}
	authorizeCard := func() types.AuthorizeResponseJsonIdTagInfoStatus {
		resp, err := authorize.HandleCall(ctx, stationKey, &types.AuthorizeJson{IdTag: card})
		require.NoError(t, err)
		return resp.(*types.AuthorizeResponseJson).IdTagInfo.Status
	}
	startTransaction := func() *types.StartTransactionResponseJson {
		resp, err := start.HandleCall(ctx, stationKey, &types.StartTransactionJson{
			ConnectorId: 1, IdTag: card, MeterStart: 0, Timestamp: "2026-11-28T09:05:00Z",
		})
		require.NoError(t, err)
		return resp.(*types.StartTransactionResponseJson)
	}

	// pending: it boots Pending, is announced once, and cannot charge
	got := bootStation()
	assert.Equal(t, types.BootNotificationResponseJsonStatusPending, got.Status)
	assert.Equal(t, 60, got.Interval)
	assert.Equal(t, []string{events.TypeStationPending}, eventstest.Types(domainEvents.Take(t)))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, authorizeCard())
	refused := startTransaction()
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, refused.IdTagInfo.Status)
	assert.Equal(t, -1, refused.TransactionId)
	_, total, err := engine.ListTransactionsForChargeStation(ctx, stationKey, "", nil, nil, 10, 0)
	require.NoError(t, err)
	assert.Zero(t, total)
	pending, _, err := engine.ListChargeStations(ctx, store.ChargeStationFilter{
		OrgIds: []string{orgId}, States: []store.StationState{store.StationStatePending},
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, stationKey, pending[0].StationKey)

	// active (as an approval leaves it): it boots Accepted and charges
	require.NoError(t, engine.SetChargeStationState(ctx, stationKey, store.StationStateActive, nil))
	got = bootStation()
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.Status)
	assert.Equal(t, 300, got.Interval)
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, authorizeCard())
	started := startTransaction()
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, started.IdTagInfo.Status)
	assert.Positive(t, started.TransactionId)
}
