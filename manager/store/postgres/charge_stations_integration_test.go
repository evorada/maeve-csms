// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestChargeStationAuth_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	want := &store.ChargeStationAuth{
		SecurityProfile:      store.TLSWithClientSideCertificates,
		Base64SHA256Password: "DEADBEEF",
	}

	err := testStore.SetChargeStationAuth(ctx, "cs001", want)
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationAuth(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestChargeStationAuth_LookupNotFound(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	got, err := testStore.LookupChargeStationAuth(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestChargeStationSettings_UpdateAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// Must create auth first (FK constraint)
	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	settings := &store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:  "60",
				Status: store.ChargeStationSettingStatusPending,
			},
		},
	}

	err = testStore.UpdateChargeStationSettings(ctx, "cs001", settings)
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "cs001", got.ChargeStationId)
	assert.Contains(t, got.Settings, "HeartbeatInterval")
	assert.Equal(t, "60", got.Settings["HeartbeatInterval"].Value)
}

func TestChargeStationSettings_List(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for _, id := range []string{"cs001", "cs002", "cs003"} {
		err := testStore.SetChargeStationAuth(ctx, id, &store.ChargeStationAuth{
			SecurityProfile: store.UnsecuredTransportWithBasicAuth,
		})
		require.NoError(t, err)

		err = testStore.UpdateChargeStationSettings(ctx, id, &store.ChargeStationSettings{
			ChargeStationId: id,
			Settings: map[string]*store.ChargeStationSetting{
				"Key": {Value: "val", Status: store.ChargeStationSettingStatusPending},
			},
		})
		require.NoError(t, err)
	}

	results, err := testStore.ListChargeStationSettings(ctx, 10, "")
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestChargeStationSettings_Delete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.UpdateChargeStationSettings(ctx, "cs001", &store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"Key": {Value: "val", Status: store.ChargeStationSettingStatusPending},
		},
	})
	require.NoError(t, err)

	err = testStore.DeleteChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestChargeStationRuntimeDetails_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	fw := "1.0.0"
	model := "TestModel"
	vendor := "TestVendor"
	serial := "SN001"

	details := &store.ChargeStationRuntimeDetails{
		OcppVersion:     "2.0.1",
		FirmwareVersion: &fw,
		Model:           &model,
		Vendor:          &vendor,
		SerialNumber:    &serial,
	}

	err = testStore.SetChargeStationRuntimeDetails(ctx, "cs001", details)
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationRuntimeDetails(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "2.0.1", got.OcppVersion)
	assert.Equal(t, "TestModel", *got.Model)
}

func TestChargeStationTriggerMessage_SetAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	trigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: "cs001",
		TriggerMessage:  "StatusNotification",
		SendAfter:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetChargeStationTriggerMessage(ctx, "cs001", trigger)
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationTriggerMessage(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.TriggerMessageStatusNotification, got.TriggerMessage)
}

func TestChargeStationTriggerMessage_Delete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	err = testStore.SetChargeStationTriggerMessage(ctx, "cs001", &store.ChargeStationTriggerMessage{
		ChargeStationId: "cs001",
		TriggerMessage:  "StatusNotification",
		SendAfter:       time.Now().UTC().Truncate(time.Second),
	})
	require.NoError(t, err)

	err = testStore.DeleteChargeStationTriggerMessage(ctx, "cs001")
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationTriggerMessage(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestChargeStationInstallCertificates_UpdateAndLookup(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	certs := &store.ChargeStationInstallCertificates{
		ChargeStationId: "cs001",
		Certificates: []*store.ChargeStationInstallCertificate{
			{
				CertificateType:               store.CertificateTypeCSMS,
				CertificateId:                 "cert1",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: "Pending",
				SendAfter:                     time.Now().UTC().Truncate(time.Second),
			},
		},
	}

	err = testStore.UpdateChargeStationInstallCertificates(ctx, "cs001", certs)
	require.NoError(t, err)

	got, err := testStore.LookupChargeStationInstallCertificates(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Certificates, 1)
	assert.Equal(t, "cert1", got.Certificates[0].CertificateId)
}
