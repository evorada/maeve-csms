// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// testOnboardingTime is the time of the first connection of a pending station in the
// onboarding cases; the later ones follow it by minutes.
var testOnboardingTime = time.Date(2026, 11, 28, 10, 0, 0, 123456000, time.UTC)

func onboardingTime(minutes int) time.Time {
	return testOnboardingTime.Add(time.Duration(minutes) * time.Minute)
}

// testStationOnboarding is the contract of the onboarding methods of
// store.ChargeStationRegistryStore.
func testStationOnboarding(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("an onboarding connection creates a pending station", func(t *testing.T) {
		s := newStore(t)

		created, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
		require.NoError(t, err)
		assert.True(t, created)
		created, err = s.RecordOnboardingConnection(ctx, "k7f2.CP002", orgA, store.OnboardingTransportWss, store.OnboardingCredentialOnboardingPassword, 50, onboardingTime(1))
		require.NoError(t, err)
		assert.True(t, created)

		cs := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, orgA, cs.OrgId)
		assert.Equal(t, store.StationStatePending, cs.State)
		assert.False(t, cs.AllowAnyToken)
		assert.False(t, cs.SharedCredentials)
		assert.Nil(t, cs.LocationId)
		assert.False(t, cs.StateChangedAt.IsZero())
		assert.False(t, cs.CreatedAt.IsZero())
		assert.Equal(t, &store.StationOnboarding{
			FirstSeenAt: onboardingTime(0),
			LastSeenAt:  onboardingTime(0),
			Transport:   store.OnboardingTransportWs,
			Credential:  store.OnboardingCredentialOpen,
			Status:      store.OnboardingAwaitingApproval,
		}, cs.Onboarding)

		// the security profile follows the transport, and the station has no password
		auth, err := s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, OrgId: orgA}, auth)
		auth, err = s.LookupChargeStationAuth(ctx, "k7f2.CP002")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}, auth)
		assert.Equal(t, store.OnboardingCredentialOnboardingPassword, lookupStation(t, s, "k7f2.CP002").Onboarding.Credential)

		pending, err := s.CountChargeStations(ctx, orgA, store.StationStatePending)
		require.NoError(t, err)
		assert.Equal(t, 2, pending)
		assert.Equal(t, []string{"k7f2.CP001", "k7f2.CP002"}, listStationKeys(t, s, store.ChargeStationFilter{
			OrgIds: []string{orgA}, States: []store.StationState{store.StationStatePending},
		}))
	})

	t.Run("an onboarding connection of a pending station is its last connection", func(t *testing.T) {
		s := newStore(t)
		_, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
		require.NoError(t, err)

		created, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWss, store.OnboardingCredentialOnboardingPassword, 50, onboardingTime(5))
		require.NoError(t, err)
		assert.False(t, created)

		cs := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStatePending, cs.State)
		assert.Equal(t, &store.StationOnboarding{
			FirstSeenAt: onboardingTime(0),
			LastSeenAt:  onboardingTime(5),
			Transport:   store.OnboardingTransportWss,
			Credential:  store.OnboardingCredentialOnboardingPassword,
			Status:      store.OnboardingAwaitingApproval,
		}, cs.Onboarding)
		auth, err := s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, store.UnsecuredTransportWithBasicAuth, auth.SecurityProfile, "the security profile is the one of the first connection")

		// a pending station is counted once, also at its limit
		_, err = s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 1, onboardingTime(6))
		require.NoError(t, err)
		pending, err := s.CountChargeStations(ctx, orgA, store.StationStatePending)
		require.NoError(t, err)
		assert.Equal(t, 1, pending)
	})

	t.Run("the pending stations of an org are limited", func(t *testing.T) {
		s := newStore(t)
		for i, key := range []string{"k7f2.CP001", "k7f2.CP002"} {
			created, err := s.RecordOnboardingConnection(ctx, key, orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 2, onboardingTime(i))
			require.NoError(t, err)
			require.True(t, created)
		}
		// registered and decommissioned stations do not count
		registerStation(t, s, "k7f2.CP010", orgA)
		registerStation(t, s, "k7f2.CP011", orgA)
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP011", store.StationStateDecommissioned, nil))

		created, err := s.RecordOnboardingConnection(ctx, "k7f2.CP003", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 2, onboardingTime(3))
		assert.ErrorIs(t, err, store.ErrLimitExceeded)
		assert.False(t, created)
		got, err := s.LookupChargeStation(ctx, "k7f2.CP003")
		require.NoError(t, err)
		assert.Nil(t, got, "nothing is written above the limit")
		auth, err := s.LookupChargeStationAuth(ctx, "k7f2.CP003")
		require.NoError(t, err)
		assert.Nil(t, auth)

		// the stations that are pending still connect, and another org has a limit of its own
		_, err = s.RecordOnboardingConnection(ctx, "k7f2.CP002", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 2, onboardingTime(4))
		assert.NoError(t, err)
		created, err = s.RecordOnboardingConnection(ctx, "x9q1.CP003", orgB, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 2, onboardingTime(4))
		require.NoError(t, err)
		assert.True(t, created)

		// a limit of 0 creates nothing
		_, err = s.RecordOnboardingConnection(ctx, "x9q1.CP004", orgB, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 0, onboardingTime(5))
		assert.ErrorIs(t, err, store.ErrLimitExceeded)

		pending, err := s.CountChargeStations(ctx, orgA, store.StationStatePending)
		require.NoError(t, err)
		assert.Equal(t, 2, pending)
	})

	t.Run("a station that exists and is not pending is never made pending", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "k7f2.CP001", orgA)
		registerStation(t, s, "k7f2.CP002", orgA)
		require.NoError(t, s.SetChargeStationState(ctx, "k7f2.CP002", store.StationStateDecommissioned, ptr("rejected")))
		// a station of another org under a key of this one (as after a transfer)
		registerStation(t, s, "k7f2.CP003", orgB)

		for key, state := range map[string]store.StationState{
			"k7f2.CP001": store.StationStateActive,
			"k7f2.CP002": store.StationStateDecommissioned,
			"k7f2.CP003": store.StationStateActive,
		} {
			before, err := s.LookupChargeStationAuth(ctx, key)
			require.NoError(t, err)

			created, err := s.RecordOnboardingConnection(ctx, key, orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
			assert.ErrorIs(t, err, store.ErrInvalidState, key)
			assert.False(t, created, key)

			cs := lookupStation(t, s, key)
			assert.Equal(t, state, cs.State, key)
			assert.Nil(t, cs.Onboarding, key)
			after, err := s.LookupChargeStationAuth(ctx, key)
			require.NoError(t, err)
			assert.Equal(t, before, after, key)
		}
		assert.Equal(t, orgB, lookupStation(t, s, "k7f2.CP003").OrgId)
	})

	t.Run("an onboarding connection needs a known transport and credential and an org", func(t *testing.T) {
		s := newStore(t)
		for name, args := range map[string][3]string{
			"transport":  {orgA, "http", store.OnboardingCredentialOpen},
			"credential": {orgA, store.OnboardingTransportWs, "certificate"},
			"org":        {"not-a-uuid", store.OnboardingTransportWs, store.OnboardingCredentialOpen},
		} {
			_, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", args[0], args[1], args[2], 50, onboardingTime(0))
			assert.Error(t, err, name)
		}
		got, err := s.LookupChargeStation(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("an onboarding boot notifies once", func(t *testing.T) {
		s := newStore(t)
		_, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
		require.NoError(t, err)

		onboarding, notify, err := s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(1))
		require.NoError(t, err)
		assert.True(t, notify)
		want := &store.StationOnboarding{
			FirstSeenAt: onboardingTime(0),
			LastSeenAt:  onboardingTime(1),
			Transport:   store.OnboardingTransportWs,
			Credential:  store.OnboardingCredentialOpen,
			Status:      store.OnboardingAwaitingApproval,
			NotifiedAt:  ptr(onboardingTime(1)),
		}
		assert.Equal(t, want, onboarding)
		assert.Equal(t, want, lookupStation(t, s, "k7f2.CP001").Onboarding)

		onboarding, notify, err = s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(2))
		require.NoError(t, err)
		assert.False(t, notify)
		want.LastSeenAt = onboardingTime(2)
		assert.Equal(t, want, onboarding)
		assert.Equal(t, want, lookupStation(t, s, "k7f2.CP001").Onboarding)

		// a connection keeps the notification
		_, err = s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(3))
		require.NoError(t, err)
		_, notify, err = s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(4))
		require.NoError(t, err)
		assert.False(t, notify)
	})

	t.Run("an onboarding boot needs a pending station", func(t *testing.T) {
		s := newStore(t)
		registerStation(t, s, "k7f2.CP001", orgA)

		_, notify, err := s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(0))
		assert.ErrorIs(t, err, store.ErrInvalidState)
		assert.False(t, notify)
		assert.Nil(t, lookupStation(t, s, "k7f2.CP001").Onboarding)

		_, notify, err = s.RecordOnboardingBoot(ctx, "k7f2.unknown", onboardingTime(0))
		assert.ErrorIs(t, err, store.ErrNotFound)
		assert.False(t, notify)
	})

	t.Run("the limit holds for connections at the same time", func(t *testing.T) {
		s := newStore(t)
		const connections, limit = 12, 5
		var created, refused atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < connections; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ok, err := s.RecordOnboardingConnection(ctx, fmt.Sprintf("k7f2.CP%03d", i), orgA, store.OnboardingTransportWs,
					store.OnboardingCredentialOpen, limit, onboardingTime(0))
				switch {
				case errors.Is(err, store.ErrLimitExceeded):
					refused.Add(1)
				case err == nil && ok:
					created.Add(1)
				default:
					t.Errorf("connection %d: created %v, err %v", i, ok, err)
				}
			}(i)
		}
		wg.Wait()
		assert.EqualValues(t, limit, created.Load())
		assert.EqualValues(t, connections-limit, refused.Load())
		pending, err := s.CountChargeStations(ctx, orgA, store.StationStatePending)
		require.NoError(t, err)
		assert.Equal(t, limit, pending)
	})

	t.Run("one of the boots at the same time notifies", func(t *testing.T) {
		s := newStore(t)
		_, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
		require.NoError(t, err)
		var notified atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, notify, err := s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(1))
				if err != nil {
					t.Errorf("boot: %v", err)
				}
				if notify {
					notified.Add(1)
				}
			}()
		}
		wg.Wait()
		assert.EqualValues(t, 1, notified.Load())
	})

	t.Run("registering a pending station keeps it pending", func(t *testing.T) {
		s := newStore(t)
		_, err := s.RecordOnboardingConnection(ctx, "k7f2.CP001", orgA, store.OnboardingTransportWs, store.OnboardingCredentialOpen, 50, onboardingTime(0))
		require.NoError(t, err)
		before := lookupStation(t, s, "k7f2.CP001")

		registerStation(t, s, "k7f2.CP001", "")

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStatePending, got.State)
		assert.Equal(t, before.Onboarding, got.Onboarding)
		assert.Equal(t, orgA, got.OrgId)
	})

	t.Run("the pending password of the auth details", func(t *testing.T) {
		s := newStore(t)
		withPending := &store.ChargeStationAuth{
			SecurityProfile:             store.TLSWithBasicAuth,
			Base64SHA256Password:        "DEADBEEF",
			PendingBase64SHA256Password: "C0FFEE",
			OrgId:                       orgA,
		}
		require.NoError(t, s.SetChargeStationAuth(ctx, "k7f2.CP001", withPending))
		auth, err := s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, "C0FFEE", auth.PendingBase64SHA256Password)
		assert.Equal(t, "DEADBEEF", auth.Base64SHA256Password)

		// the auth details are replaced: without a pending password there is none
		require.NoError(t, s.SetChargeStationAuth(ctx, "k7f2.CP001", &store.ChargeStationAuth{
			SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "DEADBEEF", OrgId: orgA,
		}))
		auth, err = s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Empty(t, auth.PendingBase64SHA256Password)
	})
}
