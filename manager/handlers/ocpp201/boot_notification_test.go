// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func makePtr[T any](t T) *T {
	p := t
	return &p
}

func TestBootNotificationHandler(t *testing.T) {
	now, err := time.Parse(time.RFC3339, "2023-06-15T15:05:00+01:00")
	require.NoError(t, err)
	engine := inmemory.NewStore(clock.RealClock{})

	handler := handlers.BootNotificationHandler{
		Clock:               clockTest.NewFakePassiveClock(now),
		RuntimeDetailsStore: engine,
		HeartbeatInterval:   10,
	}

	req := &types.BootNotificationRequestJson{
		ChargingStation: types.ChargingStationType{
			Model:        "testy",
			SerialNumber: makePtr("cs001"),
		},
		Reason: types.BootReasonEnumTypePowerUp,
	}

	got, err := handler.HandleCall(context.Background(), "cs001", req)
	assert.NoError(t, err)

	want := &types.BootNotificationResponseJson{
		CurrentTime: "2023-06-15T15:05:00+01:00",
		Status:      types.RegistrationStatusEnumTypeAccepted,
		Interval:    10,
	}

	assert.Equal(t, want, got)

	details, err := engine.LookupChargeStationRuntimeDetails(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, store.ChargeStationRuntimeDetails{
		OcppVersion: "2.0.1",
	}, *details)
}

func TestBootNotificationHeartbeatIntervalOfTheTenant(t *testing.T) {
	ctx := context.Background()
	const (
		orgWithInterval    = "0193b2c4-0000-7000-8000-0000000000a1"
		orgWithoutInterval = "0193b2c4-0000-7000-8000-0000000000b2"
		global             = 300
	)

	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgWithInterval, Path: store.OrgPathLabel(orgWithInterval), Slug: "tenant-a", OrgKey: "aaaa",
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"heartbeat_interval_s":120}`),
	}))
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgWithoutInterval, Path: store.OrgPathLabel(orgWithoutInterval), Slug: "tenant-b", OrgKey: "bbbb",
	}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.cs001", &store.ChargeStationAuth{OrgId: orgWithInterval}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.cs001", &store.ChargeStationAuth{OrgId: orgWithoutInterval}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{}))

	multi := &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}}
	single := &tenancy.ConfigProvider{Resolver: &tenancy.Resolver{Mode: tenancy.ModeSingle, Tenants: engine, Stations: engine}}

	cases := []struct {
		name            string
		provider        *tenancy.ConfigProvider
		chargeStationId string
		want            int
	}{
		{name: "the interval of the tenant", provider: multi, chargeStationId: "aaaa.cs001", want: 120},
		{name: "a tenant without a config", provider: multi, chargeStationId: "bbbb.cs001", want: global},
		{name: "no tenant", provider: multi, chargeStationId: "cs001", want: global},
		{name: "single mode", provider: single, chargeStationId: "aaaa.cs001", want: global},
		{name: "no provider", provider: nil, chargeStationId: "aaaa.cs001", want: global},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := handlers.BootNotificationHandler{
				Clock:               clock.RealClock{},
				RuntimeDetailsStore: engine,
				HeartbeatInterval:   global,
				TenantConfig:        tc.provider,
			}

			got, err := handler.HandleCall(ctx, tc.chargeStationId, &types.BootNotificationRequestJson{
				ChargingStation: types.ChargingStationType{Model: "testy"},
				Reason:          types.BootReasonEnumTypePowerUp,
			})
			require.NoError(t, err)
			response := got.(*types.BootNotificationResponseJson)
			assert.Equal(t, tc.want, response.Interval)
			assert.Equal(t, types.RegistrationStatusEnumTypeAccepted, response.Status)
		})
	}
}
