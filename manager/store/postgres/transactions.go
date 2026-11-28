// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Transactions retrieves all transactions from the database
func (s *Store) Transactions(ctx context.Context) ([]*store.Transaction, error) {
	txns, err := s.readQueries(ctx).ListTransactions(ctx)
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

// energyRegisterMeasurand is the measurand of the energy meter reading that is used to
// derive the meter start and stop values of a transaction.
const energyRegisterMeasurand = "Energy.Active.Import.Register"

// FindTransaction retrieves a transaction by charge station ID and transaction ID.
// Transaction ids are only unique per charge station: a transaction with the same id
// on another charge station is not found.
func (s *Store) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	txn, err := s.readQueries(ctx).GetTransaction(ctx, GetTransactionParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find transaction: %w", err)
	}

	return s.toStoreTransaction(ctx, &txn)
}

// FindTransactionSummary retrieves a transaction without its meter values.
func (s *Store) FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	txn, err := s.readQueries(ctx).GetTransaction(ctx, GetTransactionParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find transaction: %w", err)
	}

	return transactionSummary(&txn), nil
}

func (s *Store) FindActiveTransaction(ctx context.Context, chargeStationId string) (*store.Transaction, error) {
	txn, err := s.readQueries(ctx).FindActiveTransaction(ctx, chargeStationId)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find active transaction: %w", err)
	}

	return s.toStoreTransaction(ctx, &txn)
}

// energyRegisterValue returns the energy meter reading (Wh) of a meter value, if it has one.
func energyRegisterValue(mv *store.MeterValue) pgtype.Int8 {
	for _, sv := range mv.SampledValues {
		if sv.Measurand != nil && *sv.Measurand == energyRegisterMeasurand {
			return pgtype.Int8{Int64: int64(sv.Value), Valid: true}
		}
	}
	return pgtype.Int8{}
}

// CreateTransaction creates a new transaction with initial meter values. It returns an
// error wrapping store.ErrAlreadyExists if the charge station already has a transaction
// with this id.
func (s *Store) CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValues []store.MeterValue, seqNo int, offline bool) error {
	// Extract meter start from first meter value if available (NULL when not known)
	var meterStart pgtype.Int8
	startTimestamp := time.Now()

	if len(meterValues) > 0 {
		// Parse timestamp from first meter value
		parsedTime, err := time.Parse(time.RFC3339, meterValues[0].Timestamp)
		if err != nil {
			return fmt.Errorf("invalid timestamp in meter value: %w", err)
		}
		startTimestamp = parsedTime

		// Try to find energy meter reading for meter_start
		meterStart = energyRegisterValue(&meterValues[0])
	}

	// Create transaction record
	params := CreateTransactionParams{
		ID:              transactionId,
		ChargeStationID: chargeStationId,
		TokenUid:        idToken,
		TokenType:       tokenType,
		MeterStart:      meterStart,
		StartTimestamp:  timestampFromTime(startTimestamp.UTC()),
		Offline:         offline,
	}

	_, err := s.writeQueries(ctx).CreateTransaction(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// the insert does nothing when the transaction exists (ON CONFLICT DO NOTHING)
			return fmt.Errorf("transaction %s/%s: %w", chargeStationId, transactionId, store.ErrAlreadyExists)
		}
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	// Store meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, chargeStationId, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	return nil
}

// getTransactionForUpdate retrieves the transaction of a charge station that is about to
// be updated. It returns an error wrapping store.ErrNotFound if it does not exist.
func (s *Store) getTransactionForUpdate(ctx context.Context, chargeStationId, transactionId string) (*Transaction, error) {
	txn, err := s.readQueries(ctx).GetTransaction(ctx, GetTransactionParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("transaction not found: %s/%s: %w", chargeStationId, transactionId, store.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	return &txn, nil
}

// UpdateTransaction updates a transaction with additional meter values
func (s *Store) UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValues []store.MeterValue) error {
	// Verify transaction exists for this charge station
	txn, err := s.getTransactionForUpdate(ctx, chargeStationId, transactionId)
	if err != nil {
		return err
	}

	// Add meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, chargeStationId, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	// Update sequence number
	updateParams := UpdateTransactionParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
		MeterStop:       txn.MeterStop,
		StopTimestamp:   txn.StopTimestamp,
		StoppedReason:   txn.StoppedReason,
		UpdatedSeqNo:    txn.UpdatedSeqNo + 1,
	}

	_, err = s.writeQueries(ctx).UpdateTransaction(ctx, updateParams)
	if err != nil {
		return fmt.Errorf("failed to update transaction sequence: %w", err)
	}

	return nil
}

// EndTransaction ends a transaction with final meter values. The stop reason is not
// changed: it is written with UpdateTransactionDetails.
func (s *Store) EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValues []store.MeterValue, seqNo int) error {
	// Verify transaction exists for this charge station
	txn, err := s.getTransactionForUpdate(ctx, chargeStationId, transactionId)
	if err != nil {
		return err
	}

	// Extract meter stop from last meter value if available, otherwise keep what is stored
	meterStop := txn.MeterStop
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
		if reading := energyRegisterValue(&lastMV); reading.Valid {
			meterStop = reading
		}
	}

	// Add final meter values
	for _, mv := range meterValues {
		if err := s.addMeterValue(ctx, chargeStationId, transactionId, &mv); err != nil {
			return fmt.Errorf("failed to add meter value: %w", err)
		}
	}

	// Update transaction to mark as ended
	updateParams := UpdateTransactionParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
		MeterStop:       meterStop,
		StopTimestamp:   timestampFromTime(stopTimestamp.UTC()),
		StoppedReason:   txn.StoppedReason,
		UpdatedSeqNo:    txn.UpdatedSeqNo + 1,
	}

	_, err = s.writeQueries(ctx).UpdateTransaction(ctx, updateParams)
	if err != nil {
		return fmt.Errorf("failed to end transaction: %w", err)
	}

	return nil
}

// UpdateTransactionDetails merges the non-nil fields of details into the stored
// transaction. It returns an error wrapping store.ErrNotFound if the transaction does
// not exist.
func (s *Store) UpdateTransactionDetails(ctx context.Context, chargeStationId, transactionId string, details *store.TransactionDetails) error {
	if details == nil {
		details = &store.TransactionDetails{}
	}

	rows, err := s.writeQueries(ctx).UpdateTransactionDetails(ctx, UpdateTransactionDetailsParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
		EvseID:          toNullInt32(details.EvseId),
		ConnectorID:     toNullInt32(details.ConnectorId),
		StartedAt:       toNullTimestamp(details.StartedAt),
		StoppedAt:       toNullTimestamp(details.StoppedAt),
		MeterStartWh:    toNullInt64(details.MeterStartWh),
		MeterStopWh:     toNullInt64(details.MeterStopWh),
		EnergyWh:        toNullInt64(details.EnergyWh),
		StopReason:      textFromString(details.StopReason),
		IDTokenGroup:    textFromString(details.IdTokenGroup),
		ReservationID:   toNullInt32(details.ReservationId),
	})
	if err != nil {
		return fmt.Errorf("failed to update transaction details for %s/%s: %w", chargeStationId, transactionId, err)
	}
	if rows == 0 {
		return fmt.Errorf("transaction %s/%s: %w", chargeStationId, transactionId, store.ErrNotFound)
	}

	return nil
}

// SetTransactionTariff stores the tariff version and the tariff source of a transaction
// that has none yet. What it is given is checked before anything is sent, so that a call
// that is refused leaves a transaction of the caller usable.
func (s *Store) SetTransactionTariff(ctx context.Context, chargeStationId, transactionId string, tariffId *string, version *int, source string) error {
	what := fmt.Sprintf("set the tariff of transaction %s/%s", chargeStationId, transactionId)
	if err := store.CheckTransactionTariff(tariffId, version, source); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	params := SetTransactionTariffParams{ChargeStationID: chargeStationId, ID: transactionId, TariffSource: source}
	if tariffId != nil {
		id, err := pgUUID(*tariffId)
		if err != nil {
			return fmt.Errorf("%s: invalid tariff id %q: %w", what, *tariffId, err)
		}
		number, err := safeIntToInt32(*version)
		if err != nil {
			return fmt.Errorf("%s: tariff version: %w", what, err)
		}
		params.TariffID = id
		params.TariffVersion = pgtype.Int4{Int32: number, Valid: true}
	}

	q := s.writeQueries(ctx)
	rows, err := q.SetTransactionTariff(ctx, params)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if rows == 0 {
		_, err := q.GetTransaction(ctx, GetTransactionParams{ChargeStationID: chargeStationId, ID: transactionId})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("%s: %w", what, store.ErrNotFound)
		case err != nil:
			return fmt.Errorf("%s: %w", what, err)
		default:
			return fmt.Errorf("%s: the transaction has its tariff: %w", what, store.ErrAlreadyExists)
		}
	}
	return nil
}

// SetTransactionAnalysis stores what was worked out about an ended transaction. What it
// is given is checked before anything is sent, so that a call that is refused leaves a
// transaction of the caller usable.
func (s *Store) SetTransactionAnalysis(ctx context.Context, chargeStationId, transactionId string, analysis store.TransactionAnalysis) error {
	what := fmt.Sprintf("set the analysis of transaction %s/%s", chargeStationId, transactionId)
	if err := store.CheckTransactionAnalysis(analysis); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	params := SetTransactionAnalysisParams{
		ChargeStationID: chargeStationId,
		ID:              transactionId,
		IdleStartedAt:   toNullTimestamptz(analysis.IdleStartedAt),
		IdleS:           analysis.IdleS,
		ChargingS:       analysis.ChargingS,
		AnalysedAt:      pgtype.Timestamptz{Time: analysis.AnalysedAt, Valid: true},
	}
	if analysis.AveragePowerW != nil {
		params.AveragePowerW = pgtype.Float8{Float64: *analysis.AveragePowerW, Valid: true}
	}

	rows, err := s.writeQueries(ctx).SetTransactionAnalysis(ctx, params)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if rows == 0 {
		return fmt.Errorf("%s: %w", what, store.ErrNotFound)
	}
	return nil
}

// NextOcpp16TransactionId returns the next CSMS-assigned OCPP 1.6 transaction id
// (1..2147483647, wrapping).
func (s *Store) NextOcpp16TransactionId(ctx context.Context) (int, error) {
	id, err := s.writeQueries(ctx).NextOcpp16TransactionId(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate OCPP 1.6 transaction id: %w", err)
	}
	return int(id), nil
}

// Helper function to add a meter value to the database
func (s *Store) addMeterValue(ctx context.Context, chargeStationId, transactionId string, mv *store.MeterValue) error {
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

	// The column has no time zone and pgx drops the zone of the value it is given,
	// so the instant is stored in UTC.
	params := AddMeterValuesParams{
		ChargeStationID: chargeStationId,
		TransactionID:   transactionId,
		Timestamp:       timestampFromTime(parsedTime.UTC()),
		SampledValues:   sampledValuesJSON,
	}

	return s.writeQueries(ctx).AddMeterValues(ctx, params)
}

// UpdateTransactionCost stores the most recent running cost communicated via CostUpdated.
// Uses a direct query rather than sqlc-generated code since last_cost was added via migration.
func (s *Store) UpdateTransactionCost(ctx context.Context, chargeStationId, transactionId string, totalCost float64) error {
	_, err := s.db(ctx).Exec(ctx,
		`UPDATE transactions SET last_cost = $1, updated_at = NOW() WHERE id = $2 AND charge_station_id = $3`,
		totalCost, transactionId, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to update transaction cost for %s/%s: %w", chargeStationId, transactionId, err)
	}
	return nil
}

// ListTransactionsForChargeStation lists a page of the transactions of a charge station
// and counts them. With orgIds, only the transactions of these orgs are listed and
// counted.
func (s *Store) ListTransactionsForChargeStation(ctx context.Context, chargeStationId, status string, startDate, endDate *time.Time, limit, offset int, orgIds []string) ([]*store.Transaction, int64, error) {
	orgs, err := orgIdArray(orgIds)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list transactions: %w", err)
	}
	params := ListTransactionsFilteredParams{
		ChargeStationID: chargeStationId,
		OrgIds:          orgs,
		Limit:           int32(limit),
		Offset:          int32(offset),
	}

	if status != "" {
		params.Status = pgtype.Text{String: status, Valid: true}
	}
	if startDate != nil {
		params.StartDate = pgtype.Timestamp{Time: *startDate, Valid: true}
	}
	if endDate != nil {
		params.EndDate = pgtype.Timestamp{Time: *endDate, Valid: true}
	}

	txns, err := s.readQueries(ctx).ListTransactionsFiltered(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list transactions: %w", err)
	}

	countParams := CountTransactionsFilteredParams{
		ChargeStationID: chargeStationId,
		OrgIds:          orgs,
	}
	if status != "" {
		countParams.Status = pgtype.Text{String: status, Valid: true}
	}
	if startDate != nil {
		countParams.StartDate = pgtype.Timestamp{Time: *startDate, Valid: true}
	}
	if endDate != nil {
		countParams.EndDate = pgtype.Timestamp{Time: *endDate, Valid: true}
	}

	total, err := s.readQueries(ctx).CountTransactionsFiltered(ctx, countParams)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count transactions: %w", err)
	}

	result := make([]*store.Transaction, len(txns))
	for i, txn := range txns {
		storeTransaction, err := s.toStoreTransaction(ctx, &txn)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to convert transaction %s: %w", txn.ID, err)
		}
		result[i] = storeTransaction
	}

	return result, total, nil
}

// Helper function to convert PostgreSQL Transaction to store.Transaction
func (s *Store) toStoreTransaction(ctx context.Context, txn *Transaction) (*store.Transaction, error) {
	// Retrieve meter values for this transaction
	meterValueRecords, err := s.readQueries(ctx).GetMeterValues(ctx, GetMeterValuesParams{
		ChargeStationID: txn.ChargeStationID,
		TransactionID:   txn.ID,
	})
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

	transaction := transactionSummary(txn)
	transaction.MeterValues = meterValues
	return transaction, nil
}

// transactionSummary converts the row of a transaction to a store.Transaction without
// meter values.
func transactionSummary(txn *Transaction) *store.Transaction {
	// Calculate sequence numbers
	startSeqNo := 0
	endedSeqNo := 0
	if txn.StopTimestamp.Valid {
		endedSeqNo = int(txn.UpdatedSeqNo)
	}

	var lastCost *float64
	if txn.LastCost.Valid {
		f, _ := txn.LastCost.Float64Value()
		if f.Valid {
			lastCost = &f.Float64
		}
	}

	return &store.Transaction{
		ChargeStationId:   txn.ChargeStationID,
		TransactionId:     txn.ID,
		IdToken:           txn.TokenUid,
		TokenType:         txn.TokenType,
		StartSeqNo:        startSeqNo,
		EndedSeqNo:        endedSeqNo,
		UpdatedSeqNoCount: int(txn.UpdatedSeqNo),
		Offline:           txn.Offline,
		LastCost:          lastCost,
		EvseId:            fromNullInt32(txn.EvseID),
		ConnectorId:       fromNullInt32(txn.ConnectorID),
		StartedAt:         fromNullTimestamp(txn.StartTimestamp),
		StoppedAt:         fromNullTimestamp(txn.StopTimestamp),
		MeterStartWh:      fromNullInt64(txn.MeterStart),
		MeterStopWh:       fromNullInt64(txn.MeterStop),
		EnergyWh:          fromNullInt64(txn.EnergyWh),
		StopReason:        stringFromText(txn.StoppedReason),
		IdTokenGroup:      stringFromText(txn.IDTokenGroup),
		ReservationId:     fromNullInt32(txn.ReservationID),
		OrgId:             uuidString(txn.OrgID),
		TariffId:          optionalUUIDString(txn.TariffID),
		TariffVersion:     fromNullInt32(txn.TariffVersion),
		TariffSource:      stringFromText(txn.TariffSource),
		Analysis:          transactionAnalysis(txn),
	}
}

// transactionAnalysis returns the analysis of the row of a transaction; nil for a
// transaction that was not analysed.
func transactionAnalysis(txn *Transaction) *store.TransactionAnalysis {
	if !txn.AnalysedAt.Valid {
		return nil
	}
	analysis := &store.TransactionAnalysis{
		IdleStartedAt: fromNullTimestamptz(txn.IdleStartedAt),
		IdleS:         txn.IdleS.Int64,
		ChargingS:     txn.ChargingS.Int64,
		AnalysedAt:    txn.AnalysedAt.Time.UTC(),
	}
	if txn.AveragePowerW.Valid {
		if f, err := txn.AveragePowerW.Float64Value(); err == nil && f.Valid {
			analysis.AveragePowerW = &f.Float64
		}
	}
	return analysis
}
