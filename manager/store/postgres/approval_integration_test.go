// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	clockTest "k8s.io/utils/clock/testing"
)

// The approval of a pending charge station on PostgreSQL: its new password and approval
// are stored in the transaction of the caller, and its completion, its EVSEs and its
// events are stored together or not at all.
func TestApprovalOfAPendingStation(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()
	const (
		orgId      = "0193b2c4-0000-7000-8000-0000000000a1"
		stationKey = "aaaa.CP777"
	)
	fixture := eventstest.New(testStore, orgId, clockTest.NewFakePassiveClock(apiEventsTime))
	approvals := &onboarding.Approvals{Store: testStore, Tx: fixture.Tx, Events: fixture.Events}
	_, err := testStore.RecordOnboardingConnection(ctx, stationKey, orgId, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, apiEventsTime)
	require.NoError(t, err)
	loc := &store.AssetLocation{OrgId: orgId, PartyRef: "p1", OcpiId: "LOC-1", Address: "Calle Mayor 1", City: "Madrid",
		Country: "ESP", Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid", Publish: true}
	require.NoError(t, testStore.CreateAssetLocation(ctx, loc))
	approval := func() *store.PendingApproval {
		return &store.PendingApproval{LocationId: loc.Id, SecurityProfile: store.UnsecuredTransportWithBasicAuth, RequestedBy: "user:alice",
			RequestedAt: apiEventsTime, Evses: []*store.AssetEvse{{OcppEvseId: 1, EvseId: ptr("ES*CPO*ECP777*1"),
				Connectors: []*store.AssetConnector{{OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32}}}}}
	}
	lookup := func() *store.ChargeStation {
		cs, err := testStore.LookupChargeStation(ctx, stationKey)
		require.NoError(t, err)
		require.NotNil(t, cs)
		return cs
	}

	var rotation *onboarding.Rotation
	require.NoError(t, fixture.Tx.RunInTx(ctx, func(ctx context.Context) error {
		rotation, err = approvals.StartRotation(ctx, lookup(), approval(), transport.OcppVersion16, ptr("user:alice"), nil)
		return err
	}))
	requireNoOpenTransaction(t)
	assert.Equal(t, store.OnboardingApproving, lookup().Onboarding.Status)
	assert.Equal(t, approval(), lookup().Onboarding.Approval, "the approval is kept as JSON")
	auth, err := testStore.LookupChargeStationAuth(ctx, stationKey)
	require.NoError(t, err)
	pending := auth.PendingBase64SHA256Password
	require.NotEmpty(t, pending)

	t.Run("a completion whose event cannot be stored is rolled back", func(t *testing.T) {
		failing := eventstest.New(testStore, orgId, clockTest.NewFakePassiveClock(apiEventsTime))
		failing.Events.Emitter = &events.OutboxEmitter{Store: failingOutbox{testStore}, Orgs: events.StaticOrgResolver{OrgId: orgId}, Product: events.Product}
		before := outboxRows(t)
		failingApprovals := &onboarding.Approvals{Store: testStore, Tx: failing.Tx, Events: failing.Events}
		err := failing.Tx.RunInTx(ctx, func(ctx context.Context) error {
			cs := lookup()
			return failingApprovals.Complete(ctx, cs, cs.Onboarding.Approval,
				&store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: pending}, false, events.PasswordModeRotate)
		})
		require.ErrorIs(t, err, errAppendFailed)
		requireNoOpenTransaction(t)
		assert.Equal(t, store.StationStatePending, lookup().State)
		evses, err := testStore.ListAssetEvsesForStation(ctx, stationKey)
		require.NoError(t, err)
		assert.Empty(t, evses)
		assert.Equal(t, before, outboxRows(t))
	})

	t.Run("the station takes its password", func(t *testing.T) {
		cmd := *rotation.Command
		cmd.Status = store.CommandStatusAccepted
		cmd.Result = []byte(`{"status":"Accepted"}`)
		approvals.AfterCompletion(ctx, &cmd)
		requireNoOpenTransaction(t)

		cs := lookup()
		assert.Equal(t, store.StationStateActive, cs.State)
		assert.Nil(t, cs.Onboarding)
		auth, err := testStore.LookupChargeStationAuth(ctx, stationKey)
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: pending,
			InvalidUsernameAllowed: true, OrgId: orgId}, auth)
		evses, err := testStore.ListAssetEvsesForStation(ctx, stationKey)
		require.NoError(t, err)
		require.Len(t, evses, 1)
		assert.Equal(t, orgId, evses[0].OrgId)
		assert.Len(t, evses[0].Connectors, 1)
		assert.Equal(t, []string{events.TypeLocationUpdated, events.TypeStationApproved}, eventstest.Types(fixture.Take(t)))
	})
}
