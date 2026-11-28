// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// testApproval is an approval of a pending station at the location, with two EVSEs: one
// with two connectors and an eMI3 id, one with one connector.
func testApproval(locationId string) *store.PendingApproval {
	first := testAssetEvse(locationId, "", 1, 1, 2)
	first.EvseId = ptr("ES*CPO*ECP001*1")
	first.PhysicalReference = ptr("left")
	first.Connectors[1].MaxElectricPower = ptr(int32(22000))
	first.Connectors[1].OcpiId = "B"
	second := testAssetEvse(locationId, "", 2, 1)
	for _, evse := range []*store.AssetEvse{first, second} {
		evse.OrgId, evse.LocationId = "", ""
	}
	return &store.PendingApproval{
		LocationId:      locationId,
		DisplayName:     ptr("Plaza 1"),
		AllowAnyToken:   true,
		MaxCapacityKw:   ptr(44.0),
		SecurityProfile: store.TLSWithBasicAuth,
		Evses:           []*store.AssetEvse{first, second},
		RequestedBy:     "user:alice",
		RequestedAt:     onboardingTime(30),
	}
}

// pendingStation records the first connection of a pending station of orgA.
func pendingStation(t *testing.T, s store.Engine, stationKey string) {
	t.Helper()
	_, err := s.RecordOnboardingConnection(context.Background(), stationKey, orgA, store.OnboardingTransportWss,
		store.OnboardingCredentialOnboardingPassword, 50, onboardingTime(0))
	require.NoError(t, err)
}

// approvingOnboarding is the onboarding of a pending station whose new password is being
// sent to it with the approval.
func approvingOnboarding(approval *store.PendingApproval) *store.StationOnboarding {
	return &store.StationOnboarding{
		FirstSeenAt: onboardingTime(0),
		LastSeenAt:  onboardingTime(0),
		Transport:   store.OnboardingTransportWss,
		Credential:  store.OnboardingCredentialOnboardingPassword,
		Status:      store.OnboardingApproving,
		CommandId:   ptr("0193b2c4-0000-7000-8000-00000000c0de"),
		Approval:    approval,
	}
}

func pendingPassword(t *testing.T, s store.Engine, stationKey string) string {
	t.Helper()
	auth, err := s.LookupChargeStationAuth(context.Background(), stationKey)
	require.NoError(t, err)
	require.NotNil(t, auth)
	return auth.PendingBase64SHA256Password
}

// testStationApproval is the contract of the approval methods of
// store.ChargeStationRegistryStore.
func testStationApproval(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("the onboarding of a pending station keeps its approval and its pending password", func(t *testing.T) {
		s := newStore(t)
		pendingStation(t, s, "k7f2.CP001")
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		onboarding := approvingOnboarding(testApproval(loc.Id))

		require.NoError(t, s.SetOnboarding(ctx, "k7f2.CP001", onboarding, ptr("C0FFEE")))

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStatePending, got.State)
		assert.Equal(t, approvingOnboarding(testApproval(loc.Id)), got.Onboarding)
		assert.Equal(t, "C0FFEE", pendingPassword(t, s, "k7f2.CP001"))

		// a boot of the station keeps the approval
		_, _, err := s.RecordOnboardingBoot(ctx, "k7f2.CP001", onboardingTime(40))
		require.NoError(t, err)
		got = lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, testApproval(loc.Id), got.Onboarding.Approval)
		assert.Equal(t, onboardingTime(40), got.Onboarding.LastSeenAt)
		assert.Equal(t, "C0FFEE", pendingPassword(t, s, "k7f2.CP001"))

		// the record handed out is a copy
		got.Onboarding.Approval.Evses[0].Connectors[0].MaxVoltage = 1
		assert.Equal(t, testApproval(loc.Id), lookupStation(t, s, "k7f2.CP001").Onboarding.Approval)

		// replaced as a whole: no pending password without one
		failed := approvingOnboarding(nil)
		failed.Status = store.OnboardingCredentialsFailed
		failed.Error = ptr("NotSupported")
		require.NoError(t, s.SetOnboarding(ctx, "k7f2.CP001", failed, nil))
		assert.Equal(t, failed, lookupStation(t, s, "k7f2.CP001").Onboarding)
		assert.Empty(t, pendingPassword(t, s, "k7f2.CP001"))
	})

	t.Run("only the onboarding of a pending station is set", func(t *testing.T) {
		s := newStore(t)
		err := s.SetOnboarding(ctx, "k7f2.UNKNOWN", approvingOnboarding(nil), ptr("C0FFEE"))
		assert.ErrorIs(t, err, store.ErrNotFound)

		registerStation(t, s, "k7f2.CP001", orgA)
		err = s.SetOnboarding(ctx, "k7f2.CP001", approvingOnboarding(nil), ptr("C0FFEE"))
		assert.ErrorIs(t, err, store.ErrInvalidState)
		got := lookupStation(t, s, "k7f2.CP001")
		assert.Nil(t, got.Onboarding)
		assert.Empty(t, pendingPassword(t, s, "k7f2.CP001"))

		pendingStation(t, s, "k7f2.CP002")
		require.NoError(t, s.RejectChargeStation(ctx, "k7f2.CP002", ""))
		err = s.SetOnboarding(ctx, "k7f2.CP002", approvingOnboarding(nil), ptr("C0FFEE"))
		assert.ErrorIs(t, err, store.ErrInvalidState)
		assert.Empty(t, pendingPassword(t, s, "k7f2.CP002"))
	})

	t.Run("a completed approval makes the station active with its EVSEs", func(t *testing.T) {
		s := newStore(t)
		pendingStation(t, s, "k7f2.CP001")
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		approval := testApproval(loc.Id)
		require.NoError(t, s.SetOnboarding(ctx, "k7f2.CP001", approvingOnboarding(approval), ptr("C0FFEE")))

		auth := &store.ChargeStationAuth{SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: "C0FFEE", InvalidUsernameAllowed: true}
		require.NoError(t, s.CompleteApproval(ctx, "k7f2.CP001", approval, auth, false))

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStateActive, got.State)
		assert.Nil(t, got.StateReason)
		assert.Equal(t, &loc.Id, got.LocationId)
		assert.Equal(t, ptr("Plaza 1"), got.DisplayName)
		assert.Equal(t, ptr(44.0), got.MaxCapacityKw)
		assert.True(t, got.AllowAnyToken)
		assert.False(t, got.SharedCredentials)
		assert.Nil(t, got.Onboarding)
		assert.Equal(t, orgA, got.OrgId)

		stored, err := s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{
			SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: "C0FFEE", InvalidUsernameAllowed: true, OrgId: orgA,
		}, stored, "the pending password is the password now")

		evses, err := s.ListAssetEvsesForStation(ctx, "k7f2.CP001")
		require.NoError(t, err)
		require.Len(t, evses, 2)
		assert.Equal(t, evseUids(approval.Evses), evseUids(evses), "the EVSEs of the approval are filled in")
		for i, evse := range evses {
			assertUUIDv7(t, evse.Id)
			assert.Equal(t, orgA, evse.OrgId)
			assert.Equal(t, loc.Id, evse.LocationId)
			assert.Equal(t, "k7f2.CP001", evse.StationKey)
			assert.Equal(t, i+1, evse.OcppEvseId)
			assert.Equal(t, store.AssetEvseStatusActive, evse.Status)
			assert.EqualValues(t, 1, evse.Version)
		}
		assert.Equal(t, ptr("ES*CPO*ECP001*1"), evses[0].EvseId)
		assert.Equal(t, ptr("left"), evses[0].PhysicalReference)
		assert.Equal(t, []int{1, 2}, connectorIds(evses[0]))
		assert.Equal(t, ptr(int32(22000)), evses[0].Connectors[1].MaxElectricPower)
		assert.Equal(t, "B", evses[0].Connectors[1].OcpiId)
		assert.Equal(t, []int{1}, connectorIds(evses[1]))
		assert.Equal(t, orgA, evses[1].Connectors[0].OrgId)

		err = s.CompleteApproval(ctx, "k7f2.CP001", testApproval(loc.Id), auth, false)
		assert.ErrorIs(t, err, store.ErrInvalidState, "an active station is not approved again")
	})

	t.Run("an approval with the onboarding password shares the credentials", func(t *testing.T) {
		s := newStore(t)
		pendingStation(t, s, "k7f2.CP001")
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		approval := testApproval(loc.Id)
		approval.Evses = approval.Evses[:1]
		approval.DisplayName, approval.MaxCapacityKw, approval.AllowAnyToken = nil, nil, false

		require.NoError(t, s.CompleteApproval(ctx, "k7f2.CP001", approval,
			&store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "SHARED"}, true))

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStateActive, got.State)
		assert.True(t, got.SharedCredentials)
		assert.Nil(t, got.DisplayName)
		assert.Nil(t, got.MaxCapacityKw)
		assert.False(t, got.AllowAnyToken)
		auth, err := s.LookupChargeStationAuth(ctx, "k7f2.CP001")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "SHARED", OrgId: orgA}, auth)
	})

	t.Run("an approval that cannot be completed writes nothing", func(t *testing.T) {
		s := newStore(t)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		auth := &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "C0FFEE"}

		err := s.CompleteApproval(ctx, "k7f2.UNKNOWN", testApproval(loc.Id), auth, false)
		assert.ErrorIs(t, err, store.ErrNotFound)
		registerStation(t, s, "k7f2.ACTIVE", orgA)
		err = s.CompleteApproval(ctx, "k7f2.ACTIVE", testApproval(loc.Id), auth, false)
		assert.ErrorIs(t, err, store.ErrInvalidState)

		// an EVSE of another station already has the eMI3 id of the first EVSE
		registerStation(t, s, "k7f2.OTHER", orgA)
		taken := testAssetEvse(loc.Id, "k7f2.OTHER", 1, 1)
		taken.EvseId = ptr("ES*CPO*ECP001*1")
		createAssetEvse(t, s, taken)
		// two EVSEs with one OCPP id
		twice := testApproval(loc.Id)
		twice.Evses[1].OcppEvseId = 1
		twice.Evses[0].EvseId = nil

		pendingStation(t, s, "k7f2.CP001")
		require.NoError(t, s.SetOnboarding(ctx, "k7f2.CP001", approvingOnboarding(testApproval(loc.Id)), ptr("C0FFEE")))
		before := lookupStation(t, s, "k7f2.CP001")
		for name, tc := range map[string]struct {
			approval *store.PendingApproval
			want     error
		}{
			"unknown location":       {testApproval("0193b2c5-0000-7000-8000-00000000ffff"), store.ErrNotFound},
			"location id not a UUID": {testApproval("plaza"), store.ErrNotFound},
			"eMI3 id taken":          {testApproval(loc.Id), store.ErrAlreadyExists},
			"one OCPP EVSE id twice": {twice, store.ErrAlreadyExists},
		} {
			t.Run(name, func(t *testing.T) {
				err := s.CompleteApproval(ctx, "k7f2.CP001", tc.approval, auth, false)
				assert.ErrorIs(t, err, tc.want)

				got := lookupStation(t, s, "k7f2.CP001")
				assert.Equal(t, store.StationStatePending, got.State)
				assert.Equal(t, before.Onboarding, got.Onboarding)
				assert.Nil(t, got.LocationId)
				assert.Equal(t, "C0FFEE", pendingPassword(t, s, "k7f2.CP001"))
				evses, err := s.ListAssetEvsesForStation(ctx, "k7f2.CP001")
				require.NoError(t, err)
				assert.Empty(t, evses)
			})
		}
	})

	t.Run("a rejected station is decommissioned", func(t *testing.T) {
		s := newStore(t)
		pendingStation(t, s, "k7f2.CP001")
		require.NoError(t, s.SetOnboarding(ctx, "k7f2.CP001", approvingOnboarding(nil), ptr("C0FFEE")))

		require.NoError(t, s.RejectChargeStation(ctx, "k7f2.CP001", "not one of ours"))

		got := lookupStation(t, s, "k7f2.CP001")
		assert.Equal(t, store.StationStateDecommissioned, got.State)
		assert.Equal(t, ptr("rejected: not one of ours"), got.StateReason)
		assert.Empty(t, pendingPassword(t, s, "k7f2.CP001"))

		err := s.RejectChargeStation(ctx, "k7f2.CP001", "again")
		assert.ErrorIs(t, err, store.ErrInvalidState)
		assert.Equal(t, ptr("rejected: not one of ours"), lookupStation(t, s, "k7f2.CP001").StateReason)

		pendingStation(t, s, "k7f2.CP002")
		require.NoError(t, s.RejectChargeStation(ctx, "k7f2.CP002", ""))
		assert.Equal(t, ptr("rejected"), lookupStation(t, s, "k7f2.CP002").StateReason)

		err = s.RejectChargeStation(ctx, "k7f2.UNKNOWN", "")
		assert.ErrorIs(t, err, store.ErrNotFound)
		registerStation(t, s, "k7f2.ACTIVE", orgA)
		err = s.RejectChargeStation(ctx, "k7f2.ACTIVE", "")
		assert.ErrorIs(t, err, store.ErrInvalidState)
		assert.Equal(t, store.StationStateActive, lookupStation(t, s, "k7f2.ACTIVE").State)
	})
}
