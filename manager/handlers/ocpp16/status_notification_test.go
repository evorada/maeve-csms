// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// A limit on the connectors of a tenant refuses a new registration, never what a charge
// station that is registered reports or does.
func TestStationOfATenantAtItsConnectorLimitStillReportsAndCharges(t *testing.T) {
	const orgA = "0193b2c4-0000-7000-8000-0000000000a1"
	const chargeStationId = "aaaa.cs001"
	ctx := context.Background()
	now, err := time.Parse(time.RFC3339, authorizeClockTime)
	require.NoError(t, err)
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
		EntitlementsVersion: 1,
		Entitlements:        json.RawMessage(`{"version":1,"limits":{"cpms.connectors.max":{"value":1,"enforcement":"hard","scope":"org"}}}`),
	}))
	token := rfidToken("MYRFIDCARD")
	token.OrgId = orgA
	require.NoError(t, engine.SetToken(ctx, token))
	require.NoError(t, engine.SetChargeStationAuth(ctx, chargeStationId, &store.ChargeStationAuth{OrgId: orgA}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	enforcer := &entitlements.Enforcer{Tenants: engine, Counter: engine, Mode: tenancy.ModeMulti}

	report := func(connectorId int) {
		t.Helper()
		resp, err := handlers.StatusNotificationHandler{StatusStore: engine}.HandleCall(ctx, chargeStationId, &types.StatusNotificationJson{
			ConnectorId: connectorId,
			ErrorCode:   types.StatusNotificationJsonErrorCodeNoError,
			Status:      types.StatusNotificationJsonStatusAvailable,
		})
		require.NoError(t, err)
		assert.Equal(t, &types.StatusNotificationResponseJson{}, resp)
	}
	// what a new registration for the tenant would be answered with
	connectorsCounted := func() int64 {
		t.Helper()
		var exceeded *entitlements.LimitExceededError
		require.ErrorAs(t, enforcer.CheckConnectors(ctx, orgA, 1), &exceeded)
		return exceeded.Current
	}

	report(0)
	report(1)
	require.Equal(t, int64(1), connectorsCounted(), "the limit of the tenant is used up")

	// connectors beyond the limit are taken like any other
	report(2)
	report(3)
	statuses, err := engine.ListConnectorStatuses(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Len(t, statuses, 4)
	assert.Equal(t, int64(3), connectorsCounted())

	// and the charge station authorizes and charges on one of them
	idTag := "MYRFIDCARD"
	resp, err := handlers.AuthorizeHandler{TokenStore: engine, OrgResolver: resolver, Clock: clk}.
		HandleCall(ctx, chargeStationId, &types.AuthorizeJson{IdTag: idTag})
	require.NoError(t, err)
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, resp.(*types.AuthorizeResponseJson).IdTagInfo.Status)

	started := startTransaction(t, handlers.StartTransactionHandler{
		Clock: clk, TokenStore: engine, OrgResolver: resolver, TransactionStore: engine, ReservationStore: engine,
	}, chargeStationId, startTransactionRequest(idTag))
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, started.IdTagInfo.Status)
}

func TestStatusNotificationHandler(t *testing.T) {
	// TODO: Update test with proper mock StatusStore
	t.Skip("Skipping until StatusStore mock is implemented")

	/*
		timestamp := "2023-05-01T01:00:00+01:00"
		req := &types.StatusNotificationJson{
			Timestamp:   &timestamp,
			ConnectorId: 2,
			ErrorCode:   types.StatusNotificationJsonErrorCodeNoError,
			Status:      types.StatusNotificationJsonStatusPreparing,
		}

		handler := handlers.StatusNotificationHandler{
			StatusStore: mockStore,
		}

		got, err := handler.HandleCall(context.Background(), "cs001", req)
		assert.NoError(t, err)

		want := &types.StatusNotificationResponseJson{}

		assert.Equal(t, want, got)
	*/
}
