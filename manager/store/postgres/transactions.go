// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Transactions retrieves all transactions from the database
func (s *Store) Transactions(ctx context.Context) ([]*store.Transaction, error) {
	txns, err := s.q.ListTransactions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list transactions: %w", err)
	}

	result := make([]*store.Transaction, len(txns))
	for i, txn := range txns {
		storeTransaction, err := s.toStoreTransaction(ctx, &txn)
		if err != nil {
			return nil, fmt.Errorf("failed to convert transaction %s: %w", txn.ID, err)
		}
		result[i] = storeTransaction
	}

	return result, nil
}

// FindTransaction retrieves a transaction by charge station ID and transaction ID
func (s *Store) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	txn, err := s.q.GetTransaction(ctx, transactionId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find transaction: %w", err)
	}

	// Verify charge station ID matches
	if txn.ChargeStationID != chargeStationId {
		return nil, nil
	}

	return s.toStoreTransaction(ctx, &txn)
}

// CreateTransaction creates a new transaction with initial meter values
func (s *Store) CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValues []store.MeterValue, seqNo int, offline bool) error {
	// Extract meter start from first meter value if available
	meterStart := int32(0)
	startTimestamp := time.Now()

	if len(meterValues) > 0 {
		// Parse timestamp from first meter value
		parsedTime, err := time.Parse(time.RFC3339, meterValues[0].Timestamp)
		if err != nil {
			return fmt.Errorf("invalid timestamp in meter value: %w", err)
		}
		startTimestamp = parsedTime

		// Try to find energy meter reading for meter_start
		for _, sv := range meterValues[0].SampledValues {
			if sv.Measurand != nil && *sv.Measurand == "Energy.Active.Import.Register" {
				meterStart = int32(sv.Value)
				break
			}
		}
	}

	// Create transaction record
	params := CreateTransactionParams{
		ID:              transactionId,
		ChargeStationID: chargeStationId,
		TokenUid:        idToken,
		TokenType:       tokenType,
		MeterStart:      meterStart,
		StartTimestamp:  timestampFromTime(startTimestamp),
		Offline:         offline,
	}

	_, err := s.q.CreateTransaction(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Store meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	return nil
}

// UpdateTransaction updates a transaction with additional meter values
func (s *Store) UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValues []store.MeterValue) error {
	// Verify transaction exists and belongs to charge station
	txn, err := s.q.GetTransaction(ctx, transactionId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("transaction not found")
		}
		return fmt.Errorf("failed to get transaction: %w", err)
	}

	if txn.ChargeStationID != chargeStationId {
		return fmt.Errorf("transaction does not belong to charge station")
	}

	// Add meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	// Update sequence number
	updateParams := UpdateTransactionParams{
		ID:            transactionId,
		MeterStop:     txn.MeterStop,
		StopTimestamp: txn.StopTimestamp,
		StoppedReason: txn.StoppedReason,
		UpdatedSeqNo:  txn.UpdatedSeqNo + 1,
	}

	_, err = s.q.UpdateTransaction(ctx, updateParams)
	if err != nil {
		return fmt.Errorf("failed to update transaction sequence: %w", err)
	}

	return nil
}

// EndTransaction ends a transaction with final meter values
func (s *Store) EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValues []store.MeterValue, seqNo int) error {
	// Verify transaction exists and belongs to charge station
	txn, err := s.q.GetTransaction(ctx, transactionId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("transaction not found")
		}
		return fmt.Errorf("failed to get transaction: %w", err)
	}

	if txn.ChargeStationID != chargeStationId {
		return fmt.Errorf("transaction does not belong to charge station")
	}

	// Extract meter stop from last meter value if available
	meterStop := int32(0)
	stopTimestamp := time.Now()

	if len(meterValues) > 0 {
		lastMV := meterValues[len(meterValues)-1]

		// Parse timestamp from last meter value
		parsedTime, err := time.Parse(time.RFC3339, lastMV.Timestamp)
		if err != nil {
			return fmt.Errorf("invalid timestamp in meter value: %w", err)
		}
		stopTimestamp = parsedTime

		// Try to find energy meter reading for meter_stop
		for _, sv := range lastMV.SampledValues {
			if sv.Measurand != nil && *sv.Measurand == "Energy.Active.Import.Register" {
				meterStop = int32(sv.Value)
				break
			}
		}
	}

	// Add final meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	// Update transaction to mark as ended
	updateParams := UpdateTransactionParams{
		ID:            transactionId,
		MeterStop:     pgtype.Int4{Int32: meterStop, Valid: true},
		StopTimestamp: pgtype.Timestamp{Time: stopTimestamp, Valid: true},
		StoppedReason: pgtype.Text{String: "Remote", Valid: true},
		UpdatedSeqNo:  txn.UpdatedSeqNo + 1,
	}

	_, err = s.q.UpdateTransaction(ctx, updateParams)
	if err != nil {
		return fmt.Errorf("failed to end transaction: %w", err)
	}

	return nil
}

// Helper function to add a meter value to the database
func (s *Store) addMeterValue(ctx context.Context, transactionId string, mv *store.MeterValue) error {
	// Parse timestamp
	parsedTime, err := time.Parse(time.RFC3339, mv.Timestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	// Serialize sampled values as JSON
	sampledValuesJSON, err := json.Marshal(mv.SampledValues)
	if err != nil {
		return fmt.Errorf("failed to marshal sampled values: %w", err)
	}

	params := AddMeterValuesParams{
		TransactionID: transactionId,
		Timestamp:     timestampFromTime(parsedTime),
		SampledValues: sampledValuesJSON,
	}

	return s.q.AddMeterValues(ctx, params)
}

// Helper function to convert PostgreSQL Transaction to store.Transaction
func (s *Store) toStoreTransaction(ctx context.Context, txn *Transaction) (*store.Transaction, error) {
	// Retrieve meter values for this transaction
	meterValueRecords, err := s.q.GetMeterValues(ctx, txn.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get meter values: %w", err)
	}

	// Convert meter value records to store.MeterValue
	meterValues := make([]store.MeterValue, len(meterValueRecords))
	for i, mvr := range meterValueRecords {
		var sampledValues []store.SampledValue
		if err := json.Unmarshal(mvr.SampledValues, &sampledValues); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sampled values: %w", err)
		}

		meterValues[i] = store.MeterValue{
			SampledValues: sampledValues,
			Timestamp:     timeFromTimestamp(mvr.Timestamp).Format(time.RFC3339),
		}
	}

	// Calculate sequence numbers
	startSeqNo := 0
	endedSeqNo := 0
	if txn.StopTimestamp.Valid {
		endedSeqNo = int(txn.UpdatedSeqNo)
	}

	return &store.Transaction{
		ChargeStationId:   txn.ChargeStationID,
		TransactionId:     txn.ID,
		IdToken:           txn.TokenUid,
		TokenType:         txn.TokenType,
		MeterValues:       meterValues,
		StartSeqNo:        startSeqNo,
		EndedSeqNo:        endedSeqNo,
		UpdatedSeqNoCount: int(txn.UpdatedSeqNo),
		Offline:           txn.Offline,
	}, nil
}
