// SPDX-License-Identifier: Apache-2.0

package onboarding_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	testingclock "k8s.io/utils/clock/testing"
)

const (
	testOrg       = "0193b2c4-0000-7000-8000-0000000000a1"
	testStation   = "aaaa.CP777"
	testStation21 = "aaaa.CP201"
	waitFor       = 5 * time.Second
	tick          = 5 * time.Millisecond
)

var testStart = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

// sentCalls records the calls that the dispatcher sends to the stations.
type sentCalls struct {
	mu    sync.Mutex
	calls []*transport.Message
}

func (s *sentCalls) Emit(_ context.Context, _ transport.OcppVersion, _ string, msg *transport.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, msg)
	return nil
}

func (s *sentCalls) sent() []*transport.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*transport.Message(nil), s.calls...)
}

// connected says that every station is connected.
type connected struct{}

func (connected) IsConnected(context.Context, string) (bool, error) { return true, nil }

// fixture is the command queue of the manager with the approvals as its completion hook,
// on an in-memory store, with a pending OCPP 1.6 station and a pending OCPP 2.0.1 station
// of the org and a location of the org.
type fixture struct {
	t          *testing.T
	clock      *testingclock.FakeClock
	engine     *inmemory.Store
	events     *eventstest.Fixture
	calls      *sentCalls
	dispatcher *commands.Dispatcher
	service    *commands.Service
	approvals  *onboarding.Approvals
	location   *store.AssetLocation
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	clk := testingclock.NewFakeClock(testStart)
	engine := inmemory.NewStore(clk)
	evs := eventstest.New(engine, testOrg, clk)
	calls := &sentCalls{}
	config := commands.Config{ResponseTimeout: 30 * time.Second, SweepInterval: 5 * time.Second, DefaultTTL: time.Hour,
		MaxTTL: 168 * time.Hour, Retention: 90 * 24 * time.Hour}
	dispatcher := &commands.Dispatcher{
		Store:  engine,
		Tx:     evs.Tx,
		Events: evs.Events,
		Senders: map[transport.OcppVersion]handlers.RawCallMaker{
			transport.OcppVersion16:  ocpp16.NewCallMaker(calls),
			transport.OcppVersion201: ocpp201.NewCallMaker(calls),
		},
		Connectivity: connected{},
		SchemaFS:     schemas.OcppSchemas,
		Clock:        clk,
		Config:       config,
	}
	service := &commands.Service{Store: engine, Tx: evs.Tx, Events: evs.Events, RuntimeDetails: engine, Registry: engine,
		Dispatcher: dispatcher, SchemaFS: schemas.OcppSchemas, Clock: clk, Config: config}
	approvals := &onboarding.Approvals{Store: engine, Tx: evs.Tx, Events: evs.Events, Commands: service}
	dispatcher.AfterCompletion = []commands.CompletionHook{approvals}

	for station, version := range map[string]string{testStation: "1.6", testStation21: "2.0.1"} {
		_, err := engine.RecordOnboardingConnection(ctx, station, testOrg, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, testStart)
		require.NoError(t, err)
		require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, station, &store.ChargeStationRuntimeDetails{OcppVersion: version}))
	}
	loc := &store.AssetLocation{
		OrgId: testOrg, PartyRef: "cpo-a", OcpiId: "LOC1", Name: ptr("Plaza"), Address: "Calle Mayor 1", City: "Madrid",
		Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true,
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, loc))
	evs.Take(t)
	return &fixture{t: t, clock: clk, engine: engine, events: evs, calls: calls, dispatcher: dispatcher, service: service,
		approvals: approvals, location: loc}
}

// approval is an approval at the location of the fixture with one EVSE of two connectors.
func (f *fixture) approval() *store.PendingApproval {
	evse := &store.AssetEvse{OcppEvseId: 1, EvseId: ptr("ES*CPO*ECP777*1")}
	for _, id := range []int{1, 2} {
		evse.Connectors = append(evse.Connectors, &store.AssetConnector{
			OcppConnectorId: id, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
		})
	}
	return &store.PendingApproval{
		LocationId: f.location.Id, DisplayName: ptr("Plaza 1"), SecurityProfile: store.UnsecuredTransportWithBasicAuth,
		Evses: []*store.AssetEvse{evse}, RequestedBy: "user:alice", RequestedAt: testStart,
	}
}

// station returns the registry record of the station.
func (f *fixture) station(stationKey string) *store.ChargeStation {
	f.t.Helper()
	cs, err := f.engine.LookupChargeStation(context.Background(), stationKey)
	require.NoError(f.t, err)
	require.NotNil(f.t, cs)
	return cs
}

// auth returns the auth details of the station.
func (f *fixture) auth(stationKey string) *store.ChargeStationAuth {
	f.t.Helper()
	auth, err := f.engine.LookupChargeStationAuth(context.Background(), stationKey)
	require.NoError(f.t, err)
	require.NotNil(f.t, auth)
	return auth
}

// rotate starts the approval of the station with a new password and queues the command,
// as the API does; the station is connected, so the command is sent at once. It returns
// the command and the call that the station got.
func (f *fixture) rotate(stationKey string, version transport.OcppVersion) (*store.Command, *transport.Message) {
	f.t.Helper()
	ctx := context.Background()
	before := len(f.calls.sent())
	var rotation *onboarding.Rotation
	require.NoError(f.t, f.events.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		rotation, err = f.approvals.StartRotation(ctx, f.station(stationKey), f.approval(), version, ptr("user:alice"), nil)
		return err
	}))
	cmd, err := f.service.Enqueue(ctx, rotation.Command)
	require.NoError(f.t, err)
	sent := f.calls.sent()
	require.Len(f.t, sent, before+1)
	return cmd, sent[before]
}

// answer has the station answer the call of the command.
func (f *fixture) answer(call *transport.Message, response string) {
	f.t.Helper()
	tracked := f.dispatcher.ObserveCallResult(context.Background(), f.stationOf(call), &transport.Message{
		MessageType: transport.MessageTypeCallResult, Action: call.Action, MessageId: call.MessageId,
		RequestPayload: call.RequestPayload, ResponsePayload: json.RawMessage(response),
	})
	require.True(f.t, tracked)
}

// stationOf returns the station that the call was sent to.
func (f *fixture) stationOf(call *transport.Message) string {
	f.t.Helper()
	cmd, err := f.engine.GetCommand(context.Background(), call.MessageId)
	require.NoError(f.t, err)
	require.NotNil(f.t, cmd)
	return cmd.StationKey
}

// waitSent waits until n calls were sent and returns them.
func (f *fixture) waitSent(n int) []*transport.Message {
	f.t.Helper()
	require.Eventually(f.t, func() bool { return len(f.calls.sent()) >= n }, waitFor, tick, "waiting for %d calls", n)
	return f.calls.sent()
}

func ptr[T any](v T) *T {
	return &v
}
