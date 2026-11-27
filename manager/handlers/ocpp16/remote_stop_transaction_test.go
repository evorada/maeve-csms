// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestRemoteStopTransactionHandler_HandleCallResult(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	transactionId := 12345
	idToken := "VALID_TOKEN"

	tests := []struct {
		name          string
		request       ocpp.Request
		response      ocpp.Response
		setupStore    func() store.TransactionStore
		expectedError error
	}{
		{
			name: "successful remote stop - accepted",
			request: &types.RemoteStopTransactionJson{
				TransactionId: transactionId,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TransactionStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote stop rejected",
			request: &types.RemoteStopTransactionJson{
				TransactionId: transactionId,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TransactionStore {
				engine := inmemory.NewStore(clock.RealClock{})
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)
				return engine
			},
			expectedError: nil,
		},
		{
			name: "remote stop for unknown transaction - accepted by charge station",
			request: &types.RemoteStopTransactionJson{
				TransactionId: 99999,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TransactionStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectedError: nil,
		},
		{
			name: "remote stop for unknown transaction - rejected by charge station",
			request: &types.RemoteStopTransactionJson{
				TransactionId: 99999,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TransactionStore {
				return inmemory.NewStore(clock.RealClock{})
			},
			expectedError: nil,
		},
		{
			name: "remote stop without TransactionStore - accepted",
			request: &types.RemoteStopTransactionJson{
				TransactionId: transactionId,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusAccepted,
			},
			setupStore: func() store.TransactionStore {
				return nil // No TransactionStore
			},
			expectedError: nil,
		},
		{
			name: "remote stop without TransactionStore - rejected",
			request: &types.RemoteStopTransactionJson{
				TransactionId: transactionId,
			},
			response: &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusRejected,
			},
			setupStore: func() store.TransactionStore {
				return nil // No TransactionStore
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ocpp16.RemoteStopTransactionHandler{
				TransactionStore: tt.setupStore(),
			}

			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoteStopTransactionHandler_TransactionValidation(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	idToken := "VALID_TOKEN"

	tests := []struct {
		name          string
		transactionId int
		setupStore    func(engine *inmemory.Store)
		expectError   bool
		expectWarning bool
	}{
		{
			name:          "transaction exists",
			transactionId: 12345,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)
			},
			expectError:   false,
			expectWarning: false,
		},
		{
			name:          "transaction not found",
			transactionId: 99999,
			setupStore: func(engine *inmemory.Store) {
				// Don't create any transactions
			},
			expectError:   false,
			expectWarning: true, // Handler logs warning but doesn't error
		},
		{
			name:          "multiple transactions, stop specific one",
			transactionId: 67890,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(67890), idToken, "RFID", []store.MeterValue{}, 2, false)
				_ = engine.CreateTransaction(ctx, "cs002", ocpp16.ConvertToUUID(11111), idToken, "RFID", []store.MeterValue{}, 1, false)
			},
			expectError:   false,
			expectWarning: false,
		},
		{
			name:          "transaction from different charge station",
			transactionId: 11111,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, "cs002", ocpp16.ConvertToUUID(11111), idToken, "RFID", []store.MeterValue{}, 1, false)
			},
			expectError:   false,
			expectWarning: true, // Transaction exists but not for this charge station
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			tt.setupStore(engine)

			handler := ocpp16.RemoteStopTransactionHandler{
				TransactionStore: engine,
			}

			request := &types.RemoteStopTransactionJson{
				TransactionId: tt.transactionId,
			}
			response := &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRemoteStopTransactionHandler_DifferentStatuses(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	transactionId := 12345
	idToken := "VALID_TOKEN"

	engine := inmemory.NewStore(clock.RealClock{})
	_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)

	tests := []struct {
		name           string
		responseStatus types.RemoteStopTransactionResponseJsonStatus
	}{
		{
			name:           "accepted status",
			responseStatus: types.RemoteStopTransactionResponseJsonStatusAccepted,
		},
		{
			name:           "rejected status",
			responseStatus: types.RemoteStopTransactionResponseJsonStatusRejected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := ocpp16.RemoteStopTransactionHandler{
				TransactionStore: engine,
			}

			request := &types.RemoteStopTransactionJson{
				TransactionId: transactionId,
			}
			response := &types.RemoteStopTransactionResponseJson{
				Status: tt.responseStatus,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
			require.NoError(t, err)
		})
	}
}

func TestRemoteStopTransactionHandler_EmergencyScenarios(t *testing.T) {
	ctx := context.Background()
	chargeStationId := "cs001"
	idToken := "VALID_TOKEN"

	tests := []struct {
		name          string
		transactionId int
		setupStore    func(engine *inmemory.Store)
		description   string
	}{
		{
			name:          "emergency stop of active transaction",
			transactionId: 12345,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(12345), idToken, "RFID", []store.MeterValue{}, 1, false)
			},
			description: "Normal emergency stop scenario",
		},
		{
			name:          "emergency stop after payment failure",
			transactionId: 67890,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(67890), "PAYMENT_FAILED", "ISO14443", []store.MeterValue{}, 1, false)
			},
			description: "Stop transaction due to payment failure",
		},
		{
			name:          "emergency stop of offline transaction",
			transactionId: 11111,
			setupStore: func(engine *inmemory.Store) {
				_ = engine.CreateTransaction(ctx, chargeStationId, ocpp16.ConvertToUUID(11111), idToken, "RFID", []store.MeterValue{}, 1, true)
			},
			description: "Stop offline transaction",
		},
		{
			name:          "force stop non-existent transaction",
			transactionId: 99999,
			setupStore: func(engine *inmemory.Store) {
				// No transaction created
			},
			description: "Attempt to stop transaction that doesn't exist in store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			tt.setupStore(engine)

			handler := ocpp16.RemoteStopTransactionHandler{
				TransactionStore: engine,
			}

			request := &types.RemoteStopTransactionJson{
				TransactionId: tt.transactionId,
			}
			response := &types.RemoteStopTransactionResponseJson{
				Status: types.RemoteStopTransactionResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
			require.NoError(t, err, tt.description)
		})
	}
}

// findRecorder records the transaction ids that are looked up and whether they were found.
type findRecorder struct {
	store.TransactionStore
	lookedUp []string
	found    []bool
}

func (r *findRecorder) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	transaction, err := r.TransactionStore.FindTransaction(ctx, chargeStationId, transactionId)
	r.lookedUp = append(r.lookedUp, transactionId)
	r.found = append(r.found, transaction != nil)
	return transaction, err
}

// The transactions of OCPP 1.6 are stored with the UUID form of their integer id.
func TestRemoteStopTransactionHandlerFindsTheStoredTransaction(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	require.NoError(t, engine.CreateTransaction(ctx, "cs001", ocpp16.ConvertToUUID(12345), "TAG", "ISO14443", nil, 1, false))
	recorder := &findRecorder{TransactionStore: engine}
	handler := ocpp16.RemoteStopTransactionHandler{TransactionStore: recorder}

	err := handler.HandleCallResult(ctx, "cs001", &types.RemoteStopTransactionJson{TransactionId: 12345},
		&types.RemoteStopTransactionResponseJson{Status: types.RemoteStopTransactionResponseJsonStatusAccepted}, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"00000000-0000-0000-0000-000000003039"}, recorder.lookedUp)
	assert.Equal(t, []bool{true}, recorder.found)
}
