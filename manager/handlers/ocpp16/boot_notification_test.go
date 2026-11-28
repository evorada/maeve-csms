// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func TestBootNotificationHandler(t *testing.T) {
	now, err := time.Parse(time.RFC3339, "2023-06-15T15:05:00+01:00")
	require.NoError(t, err)

	engine := inmemory.NewStore(clock.RealClock{})

	err = engine.UpdateChargeStationSettings(context.Background(), "cs001", &store.ChargeStationSettings{
		Settings: map[string]*store.ChargeStationSetting{
			"foo": {Value: "bar", Status: store.ChargeStationSettingStatusAccepted},
			"baz": {Value: "qux", Status: store.ChargeStationSettingStatusRebootRequired},
		},
	})
	require.NoError(t, err)

	handler := handlers.BootNotificationHandler{
		Clock:               clockTest.NewFakePassiveClock(now),
		RuntimeDetailsStore: engine,
		SettingsStore:       engine,
		StatusStore:         engine,
		HeartbeatInterval:   10,
	}

	serialNumber := "cs001-1234"
	req := &types.BootNotificationJson{
		ChargePointSerialNumber: &serialNumber,
	}

	got, err := handler.HandleCall(context.Background(), "cs001", req)
	assert.NoError(t, err)

	want := &types.BootNotificationResponseJson{
		CurrentTime: "2023-06-15T15:05:00+01:00",
		Status:      types.BootNotificationResponseJsonStatusAccepted,
		Interval:    10,
	}

	assert.Equal(t, want, got)

	details, err := engine.LookupChargeStationRuntimeDetails(context.Background(), "cs001")
	require.NoError(t, err)
	emptyStr := ""
	assert.Equal(t, "1.6", details.OcppVersion)
	assert.Equal(t, &emptyStr, details.Model)
	assert.Equal(t, &emptyStr, details.Vendor)
	assert.Equal(t, &serialNumber, details.SerialNumber)

	settings, err := engine.LookupChargeStationSettings(context.Background(), "cs001")
	require.NoError(t, err)
	for _, v := range settings.Settings {
		assert.NotEqual(t, store.ChargeStationSettingStatusRebootRequired, v.Status)
	}
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
		ConfigVersion: 1, Config: json.RawMessage(`{"version":1,"default_security_profile":"SP3"}`),
	}))
	// registered charge stations, and one that tenant A owns under the key of tenant B
	require.NoError(t, engine.SetChargeStationAuth(ctx, "aaaa.cs001", &store.ChargeStationAuth{OrgId: orgWithInterval}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.cs001", &store.ChargeStationAuth{OrgId: orgWithoutInterval}))
	require.NoError(t, engine.SetChargeStationAuth(ctx, "bbbb.moved", &store.ChargeStationAuth{OrgId: orgWithInterval}))
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
		{name: "a charge station that is not registered yet", provider: multi, chargeStationId: "aaaa.new", want: 120},
		{name: "the tenant that owns the charge station", provider: multi, chargeStationId: "bbbb.moved", want: 120},
		{name: "a tenant that has not set an interval", provider: multi, chargeStationId: "bbbb.cs001", want: global},
		{name: "no tenant", provider: multi, chargeStationId: "cs001", want: global},
		{name: "single mode", provider: single, chargeStationId: "aaaa.cs001", want: global},
		{name: "no provider", provider: nil, chargeStationId: "aaaa.cs001", want: global},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := handlers.BootNotificationHandler{
				Clock:               clock.RealClock{},
				RuntimeDetailsStore: engine,
				SettingsStore:       engine,
				StatusStore:         engine,
				HeartbeatInterval:   global,
				TenantConfig:        tc.provider,
			}

			got, err := handler.HandleCall(ctx, tc.chargeStationId, &types.BootNotificationJson{})
			require.NoError(t, err)
			response := got.(*types.BootNotificationResponseJson)
			assert.Equal(t, tc.want, response.Interval)
			assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, response.Status)
		})
	}
}

// recordingKicker records the stations it was asked to send the queued commands of. It
// fails with err, and blocks until release is closed when release is set.
type recordingKicker struct {
	kicked  chan string
	release chan struct{}
	err     error
}

func (k *recordingKicker) Kick(ctx context.Context, stationKey string) error {
	if k.release != nil {
		<-k.release
	}
	if ctx.Err() != nil {
		stationKey += " (cancelled)"
	}
	k.kicked <- stationKey
	return k.err
}

func TestBootNotificationSendsTheQueuedCommandsOfTheStation(t *testing.T) {
	newHandler := func(kicker handlers.CommandKicker) handlers.BootNotificationHandler {
		engine := inmemory.NewStore(clock.RealClock{})
		return handlers.BootNotificationHandler{
			Clock:               clock.RealClock{},
			RuntimeDetailsStore: engine,
			SettingsStore:       engine,
			StatusStore:         engine,
			HeartbeatInterval:   10,
			CommandKicker:       kicker,
		}
	}

	t.Run("in the background, after the response", func(t *testing.T) {
		kicker := &recordingKicker{kicked: make(chan string, 1), release: make(chan struct{}), err: errors.New("broker down")}
		ctx, cancel := context.WithCancel(context.Background())

		// the response does not wait for the kick, which here blocks until released
		got, err := newHandler(kicker).HandleCall(ctx, "k7f2.CP001", &types.BootNotificationJson{})
		require.NoError(t, err)
		assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.(*types.BootNotificationResponseJson).Status)

		// the end of the message's handling does not stop the kick
		cancel()
		close(kicker.release)
		select {
		case station := <-kicker.kicked:
			assert.Equal(t, "k7f2.CP001", station)
		case <-time.After(5 * time.Second):
			t.Fatal("the queued commands were not sent")
		}
	})

	t.Run("without a kicker", func(t *testing.T) {
		got, err := newHandler(nil).HandleCall(context.Background(), "k7f2.CP001", &types.BootNotificationJson{})
		require.NoError(t, err)
		assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, got.(*types.BootNotificationResponseJson).Status)
	})
}

// The identity that a station reports at boot is recorded for the registry, with the time
// of the boot.
func TestBootNotificationRecordsTheIdentityOfTheStation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 28, 9, 30, 0, 0, time.UTC)
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.SetChargeStationAuth(ctx, "k7f2.CP001", &store.ChargeStationAuth{}))
	handler := handlers.BootNotificationHandler{
		Clock:               clockTest.NewFakePassiveClock(now),
		RuntimeDetailsStore: engine,
		SettingsStore:       engine,
		StatusStore:         engine,
		HeartbeatInterval:   10,
	}
	ptr := func(s string) *string { return &s }

	_, err := handler.HandleCall(ctx, "k7f2.CP001", &types.BootNotificationJson{
		ChargePointVendor:       "Acme",
		ChargePointModel:        "Volt 22",
		ChargePointSerialNumber: ptr("SN-1001"),
		FirmwareVersion:         ptr("1.2.3"),
		Iccid:                   ptr("89340711000000000019"),
		Imsi:                    ptr("214070000000001"),
		MeterSerialNumber:       ptr("MTR-77"),
		MeterType:               ptr("DZG DVH4013"),
	})
	require.NoError(t, err)

	record, err := engine.LookupChargeStation(ctx, "k7f2.CP001")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, &store.ChargeStationRuntimeDetails{
		OcppVersion:       "1.6",
		FirmwareVersion:   ptr("1.2.3"),
		Model:             ptr("Volt 22"),
		Vendor:            ptr("Acme"),
		SerialNumber:      ptr("SN-1001"),
		Iccid:             ptr("89340711000000000019"),
		Imsi:              ptr("214070000000001"),
		MeterSerialNumber: ptr("MTR-77"),
		MeterType:         ptr("DZG DVH4013"),
		LastBootAt:        &now,
	}, record.Runtime)
}

// failingRuntimeStore fails every write of the runtime details.
type failingRuntimeStore struct {
	store.ChargeStationRuntimeDetailsStore
}

func (failingRuntimeStore) SetChargeStationRuntimeDetails(context.Context, string, *store.ChargeStationRuntimeDetails) error {
	return errors.New("database is down")
}

// A station whose identity cannot be recorded is answered as any other: the failure is
// only logged, and the rest of the boot is handled.
func TestBootNotificationIsAcceptedWhenTheIdentityCannotBeRecorded(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 28, 9, 30, 0, 0, time.UTC)
	engine := inmemory.NewStore(clock.RealClock{})
	newHandler := func(runtime store.ChargeStationRuntimeDetailsStore) handlers.BootNotificationHandler {
		return handlers.BootNotificationHandler{
			Clock:               clockTest.NewFakePassiveClock(now),
			RuntimeDetailsStore: runtime,
			SettingsStore:       engine,
			StatusStore:         engine,
			HeartbeatInterval:   10,
		}
	}
	req := &types.BootNotificationJson{ChargePointVendor: "Acme", ChargePointModel: "Volt 22"}

	recorded, err := newHandler(engine).HandleCall(ctx, "k7f2.CP001", req)
	require.NoError(t, err)
	notRecorded, err := newHandler(failingRuntimeStore{}).HandleCall(ctx, "k7f2.CP002", req)
	require.NoError(t, err)

	assert.Equal(t, recorded, notRecorded)
	assert.Equal(t, types.BootNotificationResponseJsonStatusAccepted, notRecorded.(*types.BootNotificationResponseJson).Status)
	status, err := engine.GetChargeStationStatus(ctx, "k7f2.CP002")
	require.NoError(t, err)
	assert.True(t, status.Connected, "the status is still updated")
}
