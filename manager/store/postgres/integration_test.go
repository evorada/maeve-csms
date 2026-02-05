package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TestIntegration_TransactionWithToken tests creating a transaction with a token lookup
func TestIntegration_TransactionWithToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create a token first
	token := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "TOKEN001",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: time.Now().Format(time.RFC3339),
	}
	require.NoError(t, db.store.SetToken(ctx, token))

	// Create a charge station auth
	csAuth := &store.ChargeStationAuth{
		SecurityProfile:          store.TLSWithBasicAuth,
		Base64SHA256Password:     "dGVzdHBhc3N3b3Jk",
		InvalidUsernameAllowed:   false,
	}
	require.NoError(t, db.store.SetChargeStationAuth(ctx, "CS001", csAuth))

	// Create a transaction using the token
	measurand := "Energy.Active.Import.Register"
	meterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}
	err := db.store.CreateTransaction(ctx, "CS001", "TXN001", "TOKEN001", "RFID", meterValues, 0, false)
	require.NoError(t, err)

	// Verify we can look up both
	foundToken, err := db.store.LookupToken(ctx, "TOKEN001")
	require.NoError(t, err)
	require.NotNil(t, foundToken)
	assert.Equal(t, "GBTWK012345678V", foundToken.ContractId)

	foundTxn, err := db.store.FindTransaction(ctx, "CS001", "TXN001")
	require.NoError(t, err)
	require.NotNil(t, foundTxn)
	assert.Equal(t, "TOKEN001", foundTxn.IdToken)
	assert.Equal(t, "CS001", foundTxn.ChargeStationId)
}

// TestIntegration_ConcurrentTokenCreation tests concurrent token creation
func TestIntegration_ConcurrentTokenCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	numGoroutines := 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Create tokens concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			token := &store.Token{
				CountryCode: "GB",
				PartyId:     "TWK",
				Type:        "RFID",
				Uid:         fmt.Sprintf("CONCURRENT%03d", idx),
				ContractId:  fmt.Sprintf("GBTWK%010dV", idx),
				Issuer:      "Thoughtworks",
				Valid:       true,
				CacheMode:   "ALWAYS",
				LastUpdated: time.Now().Format(time.RFC3339),
			}
			if err := db.store.SetToken(ctx, token); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check no errors occurred
	for err := range errors {
		t.Errorf("Concurrent token creation failed: %v", err)
	}

	// Verify all tokens were created
	tokens, err := db.store.ListTokens(ctx, 0, numGoroutines+5)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tokens), numGoroutines)
}

// TestIntegration_ConcurrentTransactionUpdates tests concurrent transaction updates
func TestIntegration_ConcurrentTransactionUpdates(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create a token
	token := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "TOKEN_CONCURRENT",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: time.Now().Format(time.RFC3339),
	}
	require.NoError(t, db.store.SetToken(ctx, token))

	// Create a transaction
	measurand := "Energy.Active.Import.Register"
	initialMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     1000.0,
					Measurand: &measurand,
				},
			},
		},
	}
	err := db.store.CreateTransaction(ctx, "CS_CONCURRENT", "TXN_CONCURRENT", "TOKEN_CONCURRENT", "RFID", initialMeterValues, 0, false)
	require.NoError(t, err)

	// Update transaction concurrently multiple times
	numUpdates := 5
	var wg sync.WaitGroup
	errors := make(chan error, numUpdates)

	for i := 0; i < numUpdates; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Add meter values
			m := "Energy.Active.Import.Register"
			meterValues := []store.MeterValue{
				{
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					SampledValues: []store.SampledValue{
						{
							Value:     1000.0 + float64(idx*100),
							Measurand: &m,
						},
					},
				},
			}
			if err := db.store.UpdateTransaction(ctx, "CS_CONCURRENT", "TXN_CONCURRENT", meterValues); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check no errors occurred
	for err := range errors {
		t.Errorf("Concurrent transaction update failed: %v", err)
	}

	// Verify transaction has meter values
	txnFound, err := db.store.FindTransaction(ctx, "CS_CONCURRENT", "TXN_CONCURRENT")
	require.NoError(t, err)
	require.NotNil(t, txnFound)
	assert.GreaterOrEqual(t, len(txnFound.MeterValues), numUpdates+1) // Initial + updates
}

// TestIntegration_ConnectionPooling tests that multiple connections work correctly
func TestIntegration_ConnectionPooling(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	numOperations := 50
	var wg sync.WaitGroup
	errors := make(chan error, numOperations*2)

	// Perform mixed read/write operations concurrently
	for i := 0; i < numOperations; i++ {
		wg.Add(2) // One write, one read

		// Write operation
		go func(idx int) {
			defer wg.Done()
			token := &store.Token{
				CountryCode: "GB",
				PartyId:     "TWK",
				Type:        "RFID",
				Uid:         fmt.Sprintf("POOL%03d", idx),
				ContractId:  fmt.Sprintf("GBTWK%010dV", idx),
				Issuer:      "Thoughtworks",
				Valid:       true,
				CacheMode:   "ALWAYS",
				LastUpdated: time.Now().Format(time.RFC3339),
			}
			if err := db.store.SetToken(ctx, token); err != nil {
				errors <- err
			}
		}(i)

		// Read operation
		go func(idx int) {
			defer wg.Done()
			// Try to look up a token (may not exist yet, that's ok)
			_, err := db.store.LookupToken(ctx, fmt.Sprintf("POOL%03d", idx%10))
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check no errors occurred
	for err := range errors {
		t.Errorf("Connection pooling test failed: %v", err)
	}
}

// TestIntegration_MultipleActiveTransactions tests finding active transactions per station
func TestIntegration_MultipleActiveTransactions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Create tokens
	for i := 0; i < 3; i++ {
		token := &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         fmt.Sprintf("TOKEN_MULTI_%d", i),
			ContractId:  fmt.Sprintf("GBTWK%010dV", i),
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   "ALWAYS",
			LastUpdated: time.Now().Format(time.RFC3339),
		}
		require.NoError(t, db.store.SetToken(ctx, token))
	}

	// Create multiple transactions on different charge stations
	measurand := "Energy.Active.Import.Register"
	stationIDs := []string{"CS_A", "CS_B", "CS_C"}
	for i, stationID := range stationIDs {
		meterValues := []store.MeterValue{
			{
				Timestamp: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				SampledValues: []store.SampledValue{
					{
						Value:     1000.0,
						Measurand: &measurand,
					},
				},
			},
		}
		err := db.store.CreateTransaction(ctx, stationID, fmt.Sprintf("TXN_MULTI_%d", i), 
			fmt.Sprintf("TOKEN_MULTI_%d", i), "RFID", meterValues, 0, false)
		require.NoError(t, err)
	}

	// Each station should be able to find its transaction
	for i, stationID := range stationIDs {
		txn, err := db.store.FindTransaction(ctx, stationID, fmt.Sprintf("TXN_MULTI_%d", i))
		require.NoError(t, err)
		require.NotNil(t, txn)
		assert.Equal(t, fmt.Sprintf("TXN_MULTI_%d", i), txn.TransactionId)
	}

	// End transaction on CS_A
	endMeterValues := []store.MeterValue{
		{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			SampledValues: []store.SampledValue{
				{
					Value:     2000.0,
					Measurand: &measurand,
				},
			},
		},
	}
	require.NoError(t, db.store.EndTransaction(ctx, "CS_A", "TXN_MULTI_0", "TOKEN_MULTI_0", "RFID", endMeterValues, 1))

	// Verify the transaction was ended
	txnEnded, err := db.store.FindTransaction(ctx, "CS_A", "TXN_MULTI_0")
	require.NoError(t, err)
	require.NotNil(t, txnEnded)
	assert.Equal(t, 1, txnEnded.EndedSeqNo)

	// CS_B should still have its transaction
	txnB, err := db.store.FindTransaction(ctx, "CS_B", "TXN_MULTI_1")
	require.NoError(t, err)
	require.NotNil(t, txnB)
	assert.Equal(t, "TXN_MULTI_1", txnB.TransactionId)
}

// TestIntegration_ChargeStationFullLifecycle tests a complete charge station setup
func TestIntegration_ChargeStationFullLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()
	csID := "CS_LIFECYCLE"

	// Step 1: Set up authentication
	csAuth := &store.ChargeStationAuth{
		SecurityProfile:          store.TLSWithBasicAuth,
		Base64SHA256Password:     "dGVzdHBhc3N3b3Jk",
		InvalidUsernameAllowed:   false,
	}
	require.NoError(t, db.store.SetChargeStationAuth(ctx, csID, csAuth))

	// Step 2: Set runtime details
	runtime := &store.ChargeStationRuntimeDetails{
		OcppVersion: "2.0.1",
	}
	require.NoError(t, db.store.SetChargeStationRuntimeDetails(ctx, csID, runtime))

	// Step 3: Set settings
	settings := &store.ChargeStationSettings{
		ChargeStationId: csID,
		Settings: map[string]*store.ChargeStationSetting{
			"MeterValueSampleInterval": {
				Value:     "10",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
			"HeartbeatInterval": {
				Value:     "60",
				Status:    store.ChargeStationSettingStatusAccepted,
				SendAfter: time.Now(),
			},
		},
	}
	require.NoError(t, db.store.UpdateChargeStationSettings(ctx, csID, settings))

	// Step 4: Add certificates
	certs := &store.ChargeStationInstallCertificates{
		ChargeStationId: csID,
		Certificates: []*store.ChargeStationInstallCertificate{
			{
				CertificateType:               store.CertificateTypeV2G,
				CertificateId:                 "CERT001",
				CertificateData:               "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----",
				CertificateInstallationStatus: store.CertificateInstallationPending,
				SendAfter:                     time.Now(),
			},
		},
	}
	require.NoError(t, db.store.UpdateChargeStationInstallCertificates(ctx, csID, certs))

	// Step 5: Add trigger message
	trigger := &store.ChargeStationTriggerMessage{
		ChargeStationId: csID,
		TriggerMessage:  store.TriggerMessageBootNotification,
		TriggerStatus:   store.TriggerStatusPending,
		SendAfter:       time.Now(),
	}
	require.NoError(t, db.store.SetChargeStationTriggerMessage(ctx, csID, trigger))

	// Verify all components
	foundAuth, err := db.store.LookupChargeStationAuth(ctx, csID)
	require.NoError(t, err)
	require.NotNil(t, foundAuth)
	assert.Equal(t, store.TLSWithBasicAuth, foundAuth.SecurityProfile)

	foundRuntime, err := db.store.LookupChargeStationRuntimeDetails(ctx, csID)
	require.NoError(t, err)
	require.NotNil(t, foundRuntime)
	assert.Equal(t, "2.0.1", foundRuntime.OcppVersion)

	foundSettings, err := db.store.LookupChargeStationSettings(ctx, csID)
	require.NoError(t, err)
	require.NotNil(t, foundSettings)
	assert.Len(t, foundSettings.Settings, 2)
	assert.Equal(t, "10", foundSettings.Settings["MeterValueSampleInterval"].Value)

	foundCerts, err := db.store.LookupChargeStationInstallCertificates(ctx, csID)
	require.NoError(t, err)
	require.NotNil(t, foundCerts)
	assert.Len(t, foundCerts.Certificates, 1)

	foundTrigger, err := db.store.LookupChargeStationTriggerMessage(ctx, csID)
	require.NoError(t, err)
	require.NotNil(t, foundTrigger)
	assert.Equal(t, store.TriggerMessageBootNotification, foundTrigger.TriggerMessage)
}

// TestIntegration_TokenCacheMode tests different cache modes
func TestIntegration_TokenCacheMode(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	cacheModes := []string{"ALWAYS", "NEVER", "ALLOWED"}

	for i, mode := range cacheModes {
		token := &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         fmt.Sprintf("TOKEN_CACHE_%d", i),
			ContractId:  fmt.Sprintf("GBTWK%010dV", i),
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   mode,
			LastUpdated: time.Now().Format(time.RFC3339),
		}
		require.NoError(t, db.store.SetToken(ctx, token))

		// Verify cache mode is preserved
		found, err := db.store.LookupToken(ctx, fmt.Sprintf("TOKEN_CACHE_%d", i))
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, mode, found.CacheMode)
	}
}

// TestIntegration_ErrorScenarios tests various error conditions
func TestIntegration_ErrorScenarios(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	t.Run("duplicate token uid", func(t *testing.T) {
		token := &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         "DUPLICATE_UID",
			ContractId:  "GBTWK012345678V",
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   "ALWAYS",
			LastUpdated: time.Now().Format(time.RFC3339),
		}
		require.NoError(t, db.store.SetToken(ctx, token))

		// Try to create another token with same UID
		token2 := *token
		token2.ContractId = "DIFFERENT_CONTRACT"
		err := db.store.SetToken(ctx, &token2)
		assert.Error(t, err) // Should fail due to unique constraint
	})

	t.Run("transaction without token", func(t *testing.T) {
		// Create transaction with non-existent token (should still succeed as we don't enforce FK)
		measurand := "Energy.Active.Import.Register"
		meterValues := []store.MeterValue{
			{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				SampledValues: []store.SampledValue{
					{
						Value:     1000.0,
						Measurand: &measurand,
					},
				},
			},
		}
		// This should succeed as there's no foreign key constraint
		err := db.store.CreateTransaction(ctx, "CS_NO_TOKEN", "TXN_NO_TOKEN", "NONEXISTENT_TOKEN", "RFID", meterValues, 0, false)
		require.NoError(t, err)
	})

	t.Run("update non-existent transaction", func(t *testing.T) {
		err := db.store.UpdateTransaction(ctx, "NONEXISTENT_CS", "NONEXISTENT_TXN", []store.MeterValue{})
		// Should not error, just no-op
		assert.NoError(t, err)
	})

	t.Run("end non-existent transaction", func(t *testing.T) {
		measurand := "Energy.Active.Import.Register"
		endMeterValues := []store.MeterValue{
			{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				SampledValues: []store.SampledValue{
					{
						Value:     2000.0,
						Measurand: &measurand,
					},
				},
			},
		}
		err := db.store.EndTransaction(ctx, "NONEXISTENT_CS", "NONEXISTENT_TXN", "TOKEN", "RFID", endMeterValues, 1)
		// Should not error, just no-op
		assert.NoError(t, err)
	})
}
