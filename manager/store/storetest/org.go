// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	orgA = "0193b2c4-0000-7000-8000-0000000000a1"
	orgB = "0193b2c4-0000-7000-8000-0000000000b2"
)

func testChargingProfile(chargeStationId string, profileId int) *store.ChargingProfile {
	return &store.ChargingProfile{
		ChargeStationId:        chargeStationId,
		ConnectorId:            1,
		ChargingProfileId:      profileId,
		ChargingProfilePurpose: store.ChargingProfilePurposeTxDefaultProfile,
		ChargingProfileKind:    store.ChargingProfileKindAbsolute,
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit:       store.ChargingRateUnitA,
			ChargingSchedulePeriod: []store.ChargingSchedulePeriod{{StartPeriod: 0, Limit: 16}},
		},
	}
}

func testReservation(chargeStationId string, reservationId int) *store.Reservation {
	now := time.Now().UTC().Truncate(time.Second)
	return &store.Reservation{
		ReservationId:   reservationId,
		ChargeStationId: chargeStationId,
		ConnectorId:     1,
		IdTag:           "TAG001",
		ExpiryDate:      now.Add(time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       now,
	}
}

func testLocation(id, orgId, name string) *store.Location {
	return &store.Location{
		Id:      id,
		OrgId:   orgId,
		Name:    name,
		Address: "F.Rooseveltlaan 3A",
		City:    "Gent",
		Country: "BE",
	}
}

// ownedOrgIds creates a transaction, a reservation and a charging profile for a charge
// station and returns the org of each, as the store reports it.
func ownedOrgIds(t *testing.T, s store.Engine, chargeStationId string, id int) (transaction, reservation, profile string) {
	t.Helper()
	ctx := context.Background()

	transactionId := fmt.Sprintf("tx%04d", id)
	require.NoError(t, s.CreateTransaction(ctx, chargeStationId, transactionId, "TAG001", "ISO14443", nil, 0, false))
	gotTransaction, err := s.FindTransaction(ctx, chargeStationId, transactionId)
	require.NoError(t, err)
	require.NotNil(t, gotTransaction)

	require.NoError(t, s.CreateReservation(ctx, testReservation(chargeStationId, id)))
	gotReservation, err := s.GetReservation(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, gotReservation)

	require.NoError(t, s.SetChargingProfile(ctx, testChargingProfile(chargeStationId, id)))
	gotProfiles, err := s.GetChargingProfiles(ctx, chargeStationId, nil, nil, nil)
	require.NoError(t, err)
	var gotProfile *store.ChargingProfile
	for _, p := range gotProfiles {
		if p.ChargingProfileId == id {
			gotProfile = p
		}
	}
	require.NotNil(t, gotProfile)

	return gotTransaction.OrgId, gotReservation.OrgId, gotProfile.OrgId
}

// TestOrgOwnership checks which org owns the rows of a storage backend that stores orgs.
func TestOrgOwnership(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("a charge station belongs to the org it is registered with", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithBasicAuth,
			Base64SHA256Password: "pwd1",
			OrgId:                orgA,
		}))

		got, err := s.LookupChargeStationAuth(ctx, "cs001")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithBasicAuth,
			Base64SHA256Password: "pwd1",
			OrgId:                orgA,
		}, got)
	})

	t.Run("setting the auth details without an org keeps the org of the charge station", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithBasicAuth,
			Base64SHA256Password: "pwd1",
			OrgId:                orgA,
		}))
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithBasicAuth,
			Base64SHA256Password: "pwd2",
		}))

		got, err := s.LookupChargeStationAuth(ctx, "cs001")
		require.NoError(t, err)
		assert.Equal(t, &store.ChargeStationAuth{
			SecurityProfile:      store.TLSWithBasicAuth,
			Base64SHA256Password: "pwd2",
			OrgId:                orgA,
		}, got)
	})

	t.Run("setting the auth details with another org moves the charge station", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{OrgId: orgA}))
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{OrgId: orgB}))

		got, err := s.LookupChargeStationAuth(ctx, "cs001")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, orgB, got.OrgId)
	})

	t.Run("transaction, reservation and charging profile belong to the org of their charge station", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{OrgId: orgA}))
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs002", &store.ChargeStationAuth{OrgId: orgB}))

		transaction, reservation, profile := ownedOrgIds(t, s, "cs001", 1)
		assert.Equal(t, orgA, transaction)
		assert.Equal(t, orgA, reservation)
		assert.Equal(t, orgA, profile)

		transaction, reservation, profile = ownedOrgIds(t, s, "cs002", 2)
		assert.Equal(t, orgB, transaction)
		assert.Equal(t, orgB, reservation)
		assert.Equal(t, orgB, profile)
	})

	t.Run("without a registered charge station or without an org they belong to the legacy org", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs-no-org", &store.ChargeStationAuth{}))

		transaction, reservation, profile := ownedOrgIds(t, s, "cs-not-registered", 1)
		assert.Equal(t, store.LegacyOrgId, transaction)
		assert.Equal(t, store.LegacyOrgId, reservation)
		assert.Equal(t, store.LegacyOrgId, profile)

		transaction, reservation, profile = ownedOrgIds(t, s, "cs-no-org", 2)
		assert.Equal(t, store.LegacyOrgId, transaction)
		assert.Equal(t, store.LegacyOrgId, reservation)
		assert.Equal(t, store.LegacyOrgId, profile)
	})

	t.Run("a charging profile that is replaced belongs to the current org of its charge station", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{OrgId: orgA}))
		require.NoError(t, s.SetChargingProfile(ctx, testChargingProfile("cs001", 1)))

		require.NoError(t, s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{OrgId: orgB}))
		require.NoError(t, s.SetChargingProfile(ctx, testChargingProfile("cs001", 1)))

		got, err := s.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, orgB, got[0].OrgId)
	})

	t.Run("a location needs an org", func(t *testing.T) {
		s := newStore(t)
		err := s.SetLocation(ctx, testLocation("loc001", "", "No org"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "location org id is required")

		got, err := s.LookupLocation(ctx, "loc001")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("a location keeps its org", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetLocation(ctx, testLocation("loc001", orgA, "First name")))
		require.NoError(t, s.SetLocation(ctx, testLocation("loc001", orgA, "Second name")))
		require.NoError(t, s.SetLocation(ctx, testLocation("loc002", orgB, "Other org")))

		got, err := s.LookupLocation(ctx, "loc001")
		require.NoError(t, err)
		assert.Equal(t, testLocation("loc001", orgA, "Second name"), got)

		listed, err := s.ListLocations(ctx, 0, 10)
		require.NoError(t, err)
		orgIds := map[string]string{}
		for _, l := range listed {
			orgIds[l.Id] = l.OrgId
		}
		assert.Equal(t, map[string]string{"loc001": orgA, "loc002": orgB}, orgIds)
	})

	t.Run("a location of another org is not replaced", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.SetLocation(ctx, testLocation("loc001", orgA, "Org A")))

		err := s.SetLocation(ctx, testLocation("loc001", orgB, "Org B"))
		require.ErrorIs(t, err, store.ErrAlreadyExists)

		got, err := s.LookupLocation(ctx, "loc001")
		require.NoError(t, err)
		assert.Equal(t, testLocation("loc001", orgA, "Org A"), got)
	})
}

// TestStationKeys checks that every store that is keyed by a charge station takes a
// namespaced station key ("{org_key}.{station id}") and a key of the maximum length.
func TestStationKeys(t *testing.T, newStore func(t *testing.T) store.Engine) {
	keys := map[string]string{
		"org key and a station id of 48 characters": "abcd1234." + strings.Repeat("A", 48),
		"80 characters": "abcd1234." + strings.Repeat("B", 71),
	}
	require.Len(t, keys["org key and a station id of 48 characters"], 57)
	require.Len(t, keys["80 characters"], 80)

	for name, csId := range keys {
		t.Run(name, func(t *testing.T) {
			testStationKey(t, newStore(t), csId)
		})
	}
}

func testStationKey(t *testing.T, s store.Engine, csId string) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("auth", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{Base64SHA256Password: "pwd", OrgId: orgA}))
		got, err := s.LookupChargeStationAuth(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "pwd", got.Base64SHA256Password)
	})

	t.Run("settings", func(t *testing.T) {
		require.NoError(t, s.UpdateChargeStationSettings(ctx, csId, &store.ChargeStationSettings{
			Settings: map[string]*store.ChargeStationSetting{
				"HeartbeatInterval": {Value: "300", Status: store.ChargeStationSettingStatusPending},
			},
		}))
		got, err := s.LookupChargeStationSettings(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
		assert.Contains(t, got.Settings, "HeartbeatInterval")
	})

	t.Run("runtime details", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationRuntimeDetails(ctx, csId, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
		got, err := s.LookupChargeStationRuntimeDetails(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "1.6", got.OcppVersion)
	})

	t.Run("install certificates", func(t *testing.T) {
		require.NoError(t, s.UpdateChargeStationInstallCertificates(ctx, csId, &store.ChargeStationInstallCertificates{
			Certificates: []*store.ChargeStationInstallCertificate{{
				CertificateType:               store.CertificateTypeV2G,
				CertificateId:                 "cert001",
				CertificateData:               "certificate data",
				CertificateInstallationStatus: store.CertificateInstallationPending,
				SendAfter:                     now,
			}},
		}))
		got, err := s.LookupChargeStationInstallCertificates(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
		assert.Len(t, got.Certificates, 1)
	})

	t.Run("trigger message", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationTriggerMessage(ctx, csId, &store.ChargeStationTriggerMessage{
			ChargeStationId: csId,
			TriggerMessage:  store.TriggerMessageBootNotification,
			TriggerStatus:   store.TriggerStatusPending,
			SendAfter:       now,
		}))
		got, err := s.LookupChargeStationTriggerMessage(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
	})

	t.Run("status", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationStatus(ctx, csId, &store.ChargeStationStatus{ChargeStationId: csId, Connected: true}))
		got, err := s.GetChargeStationStatus(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
		assert.True(t, got.Connected)
	})

	t.Run("connector status", func(t *testing.T) {
		require.NoError(t, s.SetConnectorStatus(ctx, csId, 1, &store.ConnectorStatus{
			ChargeStationId: csId,
			ConnectorId:     1,
			Status:          store.ConnectorStatusAvailable,
			ErrorCode:       store.ConnectorErrorCodeNoError,
		}))
		got, err := s.GetConnectorStatus(ctx, csId, 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
		assert.Equal(t, store.ConnectorStatusAvailable, got.Status)
	})

	t.Run("remote start request", func(t *testing.T) {
		require.NoError(t, s.SetRemoteStartTransactionRequest(ctx, csId, &store.RemoteStartTransactionRequest{
			ChargeStationId: csId,
			IdTag:           "TAG001",
			Status:          store.RemoteTransactionRequestStatusPending,
			SendAfter:       now,
			RequestType:     store.RemoteTransactionRequestTypeStart,
		}))
		got, err := s.GetRemoteStartTransactionRequest(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
	})

	t.Run("remote stop request", func(t *testing.T) {
		require.NoError(t, s.SetRemoteStopTransactionRequest(ctx, csId, &store.RemoteStopTransactionRequest{
			ChargeStationId: csId,
			TransactionId:   "1234",
			Status:          store.RemoteTransactionRequestStatusPending,
			SendAfter:       now,
			RequestType:     store.RemoteTransactionRequestTypeStop,
		}))
		got, err := s.GetRemoteStopTransactionRequest(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
	})

	t.Run("certificate query", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationCertificateQuery(ctx, csId, &store.ChargeStationCertificateQuery{
			ChargeStationId: csId,
			QueryStatus:     store.CertificateQueryStatusPending,
			SendAfter:       now,
		}))
		got, err := s.LookupChargeStationCertificateQuery(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
	})

	t.Run("certificate deletion", func(t *testing.T) {
		require.NoError(t, s.SetChargeStationCertificateDeletion(ctx, csId, &store.ChargeStationCertificateDeletion{
			ChargeStationId: csId,
			HashAlgorithm:   "SHA256",
			IssuerNameHash:  "name",
			IssuerKeyHash:   "key",
			SerialNumber:    "serial",
			DeletionStatus:  store.CertificateDeletionStatusPending,
			SendAfter:       now,
		}))
		got, err := s.LookupChargeStationCertificateDeletion(ctx, csId)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
	})

	t.Run("events", func(t *testing.T) {
		require.NoError(t, s.AddChargeStationEvent(ctx, csId, &store.ChargeStationEvent{
			Timestamp: now,
			EventType: "PowerLoss",
			Category:  store.ChargeStationEventCategoryDevice,
		}))
		got, total, err := s.ListChargeStationEvents(ctx, csId, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, got, 1)
		assert.Equal(t, csId, got[0].ChargeStationId)
	})

	t.Run("device report", func(t *testing.T) {
		require.NoError(t, s.AddDeviceReport(ctx, csId, &store.DeviceReport{RequestId: 1, GeneratedAt: now}))
		got, total, err := s.ListDeviceReports(ctx, csId, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, got, 1)
		assert.Equal(t, csId, got[0].ChargeStationId)
	})

	t.Run("variable monitoring", func(t *testing.T) {
		require.NoError(t, s.SetVariableMonitoring(ctx, csId, &store.VariableMonitoringConfig{
			Id:            1,
			ComponentName: "EVSE",
			VariableName:  "Power",
			MonitorType:   store.MonitoringTypeUpperThreshold,
			Value:         11000,
			Severity:      5,
		}))
		got, err := s.ListVariableMonitoring(ctx, csId, 0, 10)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, csId, got[0].ChargeStationId)
	})

	t.Run("transaction with meter values", func(t *testing.T) {
		meterValues := []store.MeterValue{{
			Timestamp:     now.Format(time.RFC3339),
			SampledValues: []store.SampledValue{{Value: 100}},
		}}
		require.NoError(t, s.CreateTransaction(ctx, csId, "tx001", "TAG001", "ISO14443", meterValues, 0, false))
		got, err := s.FindTransaction(ctx, csId, "tx001")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, csId, got.ChargeStationId)
		assert.Len(t, got.MeterValues, 1)
		assert.Equal(t, orgA, got.OrgId)
	})
}
