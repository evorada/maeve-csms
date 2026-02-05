package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ChargeStationAuthStore Tests

func TestChargeStationAuthStore_SetAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "aGVsbG93b3JsZA==",
		InvalidUsernameAllowed: false,
	}

	// Test SetChargeStationAuth
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	// Test LookupChargeStationAuth - found
	foundAuth, err := db.store.LookupChargeStationAuth(ctx, "CS001")
	require.NoError(t, err)
	require.NotNil(t, foundAuth)
	assert.Equal(t, auth.SecurityProfile, foundAuth.SecurityProfile)
	assert.Equal(t, auth.Base64SHA256Password, foundAuth.Base64SHA256Password)
	assert.Equal(t, auth.InvalidUsernameAllowed, foundAuth.InvalidUsernameAllowed)

	// Test LookupChargeStationAuth - not found
	notFound, err := db.store.LookupChargeStationAuth(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestChargeStationAuthStore_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Initial auth
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "aGVsbG93b3JsZA==",
		InvalidUsernameAllowed: false,
	}

	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	// Update with new values
	updatedAuth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithClientSideCertificates,
		Base64SHA256Password:   "bmV3cGFzc3dvcmQ=",
		InvalidUsernameAllowed: true,
	}

	err = db.store.SetChargeStationAuth(ctx, "CS001", updatedAuth)
	require.NoError(t, err)

	// Verify update
	foundAuth, err := db.store.LookupChargeStationAuth(ctx, "CS001")
	require.NoError(t, err)
	assert.Equal(t, updatedAuth.SecurityProfile, foundAuth.SecurityProfile)
	assert.Equal(t, updatedAuth.Base64SHA256Password, foundAuth.Base64SHA256Password)
	assert.Equal(t, updatedAuth.InvalidUsernameAllowed, foundAuth.InvalidUsernameAllowed)
}

func TestChargeStationAuthStore_EmptyPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithClientSideCertificates,
		Base64SHA256Password:   "",
		InvalidUsernameAllowed: true,
	}

	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	foundAuth, err := db.store.LookupChargeStationAuth(ctx, "CS001")
	require.NoError(t, err)
	assert.Empty(t, foundAuth.Base64SHA256Password)
	assert.Equal(t, store.TLSWithClientSideCertificates, foundAuth.SecurityProfile)
}

// ChargeStationSettingsStore Tests

func TestChargeStationSettingsStore_UpdateAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create the charge station first (foreign key requirement)
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "test",
		InvalidUsernameAllowed: false,
	}
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	settings := &store.ChargeStationSettings{
		ChargeStationId: "CS001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:     "60",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
			"MeterValueSampleInterval": {
				Value:     "10",
				Status:    store.ChargeStationSettingStatusPending,
				SendAfter: time.Now(),
			},
		},
	}

	// Test UpdateChargeStationSettings
	err = db.store.UpdateChargeStationSettings(ctx, "CS001", settings)
	require.NoError(t, err)

	// Test LookupChargeStationSettings - found
	foundSettings, err := db.store.LookupChargeStationSettings(ctx, "CS001")
	require.NoError(t, err)
	require.NotNil(t, foundSettings)
	assert.Equal(t, "CS001", foundSettings.ChargeStationId)
	assert.Len(t, foundSettings.Settings, 2)
	assert.Equal(t, "60", foundSettings.Settings["HeartbeatInterval"].Value)
	assert.Equal(t, "10", foundSettings.Settings["MeterValueSampleInterval"].Value)

	// Test LookupChargeStationSettings - not found
	notFound, err := db.store.LookupChargeStationSettings(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestChargeStationSettingsStore_UpdateExisting(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create the charge station first (foreign key requirement)
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "test",
		InvalidUsernameAllowed: false,
	}
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	// Initial settings
	settings := &store.ChargeStationSettings{
		ChargeStationId: "CS001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:     "60",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
		},
	}

	err = db.store.UpdateChargeStationSettings(ctx, "CS001", settings)
	require.NoError(t, err)

	// Update settings
	updatedSettings := &store.ChargeStationSettings{
		ChargeStationId: "CS001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:     "120",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
			"MeterValueSampleInterval": {
				Value:     "20",
				Status:    store.ChargeStationSettingStatusPending,
				SendAfter: time.Now(),
			},
		},
	}

	err = db.store.UpdateChargeStationSettings(ctx, "CS001", updatedSettings)
	require.NoError(t, err)

	// Verify update
	foundSettings, err := db.store.LookupChargeStationSettings(ctx, "CS001")
	require.NoError(t, err)
	assert.Len(t, foundSettings.Settings, 2)
	assert.Equal(t, "120", foundSettings.Settings["HeartbeatInterval"].Value)
}

func TestChargeStationSettingsStore_ListSettings(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create settings for multiple stations
	for i := 1; i <= 5; i++ {
		csId := fmt.Sprintf("CS%03d", i)

		// Create the charge station first (foreign key requirement)
		auth := &store.ChargeStationAuth{
			SecurityProfile:        store.TLSWithBasicAuth,
			Base64SHA256Password:   "test",
			InvalidUsernameAllowed: false,
		}
		require.NoError(t, db.store.SetChargeStationAuth(ctx, csId, auth))

		settings := &store.ChargeStationSettings{
			ChargeStationId: csId,
			Settings: map[string]*store.ChargeStationSetting{
				"HeartbeatInterval": {
					Value:     "60",
					Status:    store.ChargeStationSettingStatusAccepted,
					SendAfter: time.Now(),
				},
			},
		}
		require.NoError(t, db.store.UpdateChargeStationSettings(ctx, csId, settings))
	}

	// Test list with page size
	settingsList, err := db.store.ListChargeStationSettings(ctx, 3, "")
	require.NoError(t, err)
	assert.Len(t, settingsList, 3)

	// Test pagination
	if len(settingsList) > 0 {
		lastId := settingsList[len(settingsList)-1].ChargeStationId
		nextPage, err := db.store.ListChargeStationSettings(ctx, 3, lastId)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(nextPage), 2)
	}
}

func TestChargeStationSettingsStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create the charge station first (foreign key requirement)
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "test",
		InvalidUsernameAllowed: false,
	}
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	// Create settings
	settings := &store.ChargeStationSettings{
		ChargeStationId: "CS001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:     "60",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
		},
	}

	err = db.store.UpdateChargeStationSettings(ctx, "CS001", settings)
	require.NoError(t, err)

	// Delete settings
	err = db.store.DeleteChargeStationSettings(ctx, "CS001")
	require.NoError(t, err)

	// Verify deletion
	foundSettings, err := db.store.LookupChargeStationSettings(ctx, "CS001")
	require.NoError(t, err)
	assert.Nil(t, foundSettings)
}

// ChargeStationRuntimeDetailsStore Tests

func TestChargeStationRuntimeStore_SetAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create the charge station first (foreign key requirement)
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "test",
		InvalidUsernameAllowed: false,
	}
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	runtime := &store.ChargeStationRuntimeDetails{
		OcppVersion: "1.6",
	}

	// Test SetChargeStationRuntimeDetails
	err = db.store.SetChargeStationRuntimeDetails(ctx, "CS001", runtime)
	require.NoError(t, err)

	// Test LookupChargeStationRuntimeDetails - found
	foundRuntime, err := db.store.LookupChargeStationRuntimeDetails(ctx, "CS001")
	require.NoError(t, err)
	require.NotNil(t, foundRuntime)
	assert.Equal(t, runtime.OcppVersion, foundRuntime.OcppVersion)

	// Test LookupChargeStationRuntimeDetails - not found
	notFound, err := db.store.LookupChargeStationRuntimeDetails(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestChargeStationRuntimeStore_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create the charge station first (foreign key requirement)
	auth := &store.ChargeStationAuth{
		SecurityProfile:        store.TLSWithBasicAuth,
		Base64SHA256Password:   "test",
		InvalidUsernameAllowed: false,
	}
	err := db.store.SetChargeStationAuth(ctx, "CS001", auth)
	require.NoError(t, err)

	// Initial runtime
	runtime := &store.ChargeStationRuntimeDetails{
		OcppVersion: "1.6",
	}

	err = db.store.SetChargeStationRuntimeDetails(ctx, "CS001", runtime)
	require.NoError(t, err)

	// Update runtime
	updatedRuntime := &store.ChargeStationRuntimeDetails{
		OcppVersion: "2.0.1",
	}

	err = db.store.SetChargeStationRuntimeDetails(ctx, "CS001", updatedRuntime)
	require.NoError(t, err)

	// Verify update
	foundRuntime, err := db.store.LookupChargeStationRuntimeDetails(ctx, "CS001")
	require.NoError(t, err)
	assert.Equal(t, "2.0.1", foundRuntime.OcppVersion)
}

// ChargeStationInstallCertificatesStore Tests

func TestChargeStationInstallCertificatesStore_UpdateAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	certificates := &store.ChargeStationInstallCertificates{
		ChargeStationId: "CS001",
		Certificates: []*store.ChargeStationInstallCertificate{
			{
				CertificateType:               store.CertificateTypeV2G,
				CertificateId:                 "CERT001",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: store.CertificateInstallationPending,
				SendAfter:                     time.Now(),
			},
			{
				CertificateType:               store.CertificateTypeCSMS,
				CertificateId:                 "CERT002",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nMIID...\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: store.CertificateInstallationAccepted,
				SendAfter:                     time.Now(),
			},
		},
	}

	// Test UpdateChargeStationInstallCertificates
	err := db.store.UpdateChargeStationInstallCertificates(ctx, "CS001", certificates)
	require.NoError(t, err)

	// Test LookupChargeStationInstallCertificates - found
	foundCerts, err := db.store.LookupChargeStationInstallCertificates(ctx, "CS001")
	require.NoError(t, err)
	require.NotNil(t, foundCerts)
	assert.Equal(t, "CS001", foundCerts.ChargeStationId)
	assert.Len(t, foundCerts.Certificates, 2)

	// Test LookupChargeStationInstallCertificates - not found
	notFound, err := db.store.LookupChargeStationInstallCertificates(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestChargeStationInstallCertificatesStore_ReplaceExisting(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Add initial certificates
	certificates := &store.ChargeStationInstallCertificates{
		ChargeStationId: "CS001",
		Certificates: []*store.ChargeStationInstallCertificate{
			{
				CertificateType:               store.CertificateTypeV2G,
				CertificateId:                 "CERT001",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nOLD...\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: store.CertificateInstallationPending,
				SendAfter:                     time.Now(),
			},
		},
	}

	err := db.store.UpdateChargeStationInstallCertificates(ctx, "CS001", certificates)
	require.NoError(t, err)

	// Replace with new certificate
	newCertificates := &store.ChargeStationInstallCertificates{
		ChargeStationId: "CS001",
		Certificates: []*store.ChargeStationInstallCertificate{
			{
				CertificateType:               store.CertificateTypeCSMS,
				CertificateId:                 "CERT002",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nNEW...\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: store.CertificateInstallationAccepted,
				SendAfter:                     time.Now(),
			},
		},
	}

	err = db.store.UpdateChargeStationInstallCertificates(ctx, "CS001", newCertificates)
	require.NoError(t, err)

	// Verify old certificate was replaced
	foundCerts, err := db.store.LookupChargeStationInstallCertificates(ctx, "CS001")
	require.NoError(t, err)
	require.Len(t, foundCerts.Certificates, 1)
	assert.Equal(t, "CERT002", foundCerts.Certificates[0].CertificateId)
}

func TestChargeStationInstallCertificatesStore_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create certificates for multiple stations
	for i := 1; i <= 3; i++ {
		csId := fmt.Sprintf("CS%03d", i)
		certificates := &store.ChargeStationInstallCertificates{
			ChargeStationId: csId,
			Certificates: []*store.ChargeStationInstallCertificate{
				{
					CertificateType:               store.CertificateTypeV2G,
					CertificateId:                 fmt.Sprintf("CERT%03d", i),
					CertificateData:               "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
					CertificateInstallationStatus: store.CertificateInstallationPending,
					SendAfter:                     time.Now(),
				},
			},
		}
		require.NoError(t, db.store.UpdateChargeStationInstallCertificates(ctx, csId, certificates))
	}

	// Test list
	certsList, err := db.store.ListChargeStationInstallCertificates(ctx, 10, "")
	require.NoError(t, err)
	assert.Len(t, certsList, 3)
}

// ChargeStationTriggerMessageStore Tests

func TestChargeStationTriggerMessageStore_SetAndLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	trigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: "CS001",
		TriggerMessage:  store.TriggerMessageBootNotification,
		TriggerStatus:   store.TriggerStatusPending,
		SendAfter:       time.Now(),
	}

	// Test SetChargeStationTriggerMessage
	err := db.store.SetChargeStationTriggerMessage(ctx, "CS001", trigger)
	require.NoError(t, err)

	// Test LookupChargeStationTriggerMessage - found
	foundTrigger, err := db.store.LookupChargeStationTriggerMessage(ctx, "CS001")
	require.NoError(t, err)
	require.NotNil(t, foundTrigger)
	assert.Equal(t, "CS001", foundTrigger.ChargeStationId)
	assert.Equal(t, store.TriggerMessageBootNotification, foundTrigger.TriggerMessage)
	assert.Equal(t, store.TriggerStatusPending, foundTrigger.TriggerStatus)

	// Test LookupChargeStationTriggerMessage - not found
	notFound, err := db.store.LookupChargeStationTriggerMessage(ctx, "NOTEXIST")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestChargeStationTriggerMessageStore_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Initial trigger
	trigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: "CS001",
		TriggerMessage:  store.TriggerMessageBootNotification,
		TriggerStatus:   store.TriggerStatusPending,
		SendAfter:       time.Now(),
	}

	err := db.store.SetChargeStationTriggerMessage(ctx, "CS001", trigger)
	require.NoError(t, err)

	// Update trigger
	updatedTrigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: "CS001",
		TriggerMessage:  store.TriggerMessageHeartbeat,
		TriggerStatus:   store.TriggerStatusAccepted,
		SendAfter:       time.Now(),
	}

	err = db.store.SetChargeStationTriggerMessage(ctx, "CS001", updatedTrigger)
	require.NoError(t, err)

	// Verify update
	foundTrigger, err := db.store.LookupChargeStationTriggerMessage(ctx, "CS001")
	require.NoError(t, err)
	assert.Equal(t, store.TriggerMessageHeartbeat, foundTrigger.TriggerMessage)
	assert.Equal(t, store.TriggerStatusAccepted, foundTrigger.TriggerStatus)
}

func TestChargeStationTriggerMessageStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create trigger
	trigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: "CS001",
		TriggerMessage:  store.TriggerMessageBootNotification,
		TriggerStatus:   store.TriggerStatusPending,
		SendAfter:       time.Now(),
	}

	err := db.store.SetChargeStationTriggerMessage(ctx, "CS001", trigger)
	require.NoError(t, err)

	// Delete trigger
	err = db.store.DeleteChargeStationTriggerMessage(ctx, "CS001")
	require.NoError(t, err)

	// Verify deletion
	foundTrigger, err := db.store.LookupChargeStationTriggerMessage(ctx, "CS001")
	require.NoError(t, err)
	assert.Nil(t, foundTrigger)
}

func TestChargeStationTriggerMessageStore_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create triggers for multiple stations
	for i := 1; i <= 5; i++ {
		csId := fmt.Sprintf("CS%03d", i)
		trigger := &store.ChargeStationTriggerMessage{
			ChargeStationId: csId,
			TriggerMessage:  store.TriggerMessageBootNotification,
			TriggerStatus:   store.TriggerStatusPending,
			SendAfter:       time.Now(),
		}
		require.NoError(t, db.store.SetChargeStationTriggerMessage(ctx, csId, trigger))
	}

	// Test list with page size
	triggers, err := db.store.ListChargeStationTriggerMessages(ctx, 3, "")
	require.NoError(t, err)
	assert.Len(t, triggers, 3)

	// Test pagination
	if len(triggers) > 0 {
		lastId := triggers[len(triggers)-1].ChargeStationId
		nextPage, err := db.store.ListChargeStationTriggerMessages(ctx, 3, lastId)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(nextPage), 2)
	}
}
