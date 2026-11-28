// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// AnalysisTimeout is the most time that the end of a transaction gives to its analysis
// (Analyzer.AnalyseAtStop). The answer to the charge station does not wait longer for it.
const AnalysisTimeout = 2 * time.Second

// Analyzer works out how long an ended transaction charged and how long it was idle, and
// stores that with the transaction (store.Transaction.Analysis).
//
// The thresholds are Defaults, with the idle settings of the tariff version that the
// transaction is charged with in place of those that it sets. The highest power of the
// connector is the highest power of the connector in the asset model; without one, the
// capacity of the charge station; and when neither is known, only the minimum charging
// power decides whether the transaction charged.
//
// Any of the stores but Transactions may be nil, or may not be implemented by the storage
// backend: what it holds is then not known.
type Analyzer struct {
	Transactions store.TransactionStore
	MeterValues  store.MeterValuesStore
	Assets       store.AssetStore
	Registry     store.ChargeStationRegistryStore
	Tariffs      store.TariffStore
	Defaults     IdleSettings
	// Clock gives the time of the analysis. Nil is the real clock.
	Clock clock.PassiveClock
}

// AnalyseAtStop is AnalyseAndStore for a transaction that has just ended, for the handler
// of the message that ended it, whose answer must not depend on the analysis. The
// analysis gets at most AnalysisTimeout and gives an error after that, and an analysis
// that panics gives an error too. It runs in a database transaction of tx: there it reads
// from the primary, which has the end of the transaction, also while a read replica is
// behind, and when it fails nothing of it is stored.
func (a *Analyzer) AnalyseAtStop(ctx context.Context, tx store.Transactor, chargeStationId, transactionId string) (analysis *store.TransactionAnalysis, err error) {
	ctx, cancel := context.WithTimeout(ctx, AnalysisTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			analysis, err = nil, fmt.Errorf("analyse transaction %s/%s: panic: %v", chargeStationId, transactionId, r)
		}
	}()

	err = tx.RunInTx(ctx, func(ctx context.Context) (err error) {
		analysis, err = a.AnalyseAndStore(ctx, chargeStationId, transactionId)
		return err
	})
	if err != nil {
		return nil, err
	}
	return analysis, nil
}

// AnalyseAndStore works out the analysis of an ended transaction from its meter values
// and stores it, in place of an earlier analysis. It returns what it stored; nil without
// an error when the storage backend keeps no analysis with a transaction.
//
// A transaction that does not exist is store.ErrNotFound, one that has not ended, or
// whose start is not known, is store.ErrInvalidState. When a store fails, nothing is
// stored: a transaction is not given an analysis that what could not be read might have
// changed.
func (a *Analyzer) AnalyseAndStore(ctx context.Context, chargeStationId, transactionId string) (*store.TransactionAnalysis, error) {
	what := fmt.Sprintf("analyse transaction %s/%s", chargeStationId, transactionId)

	tx, err := a.Transactions.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if tx == nil {
		return nil, fmt.Errorf("%s: %w", what, store.ErrNotFound)
	}

	analysis, err := a.Analyse(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}

	err = a.Transactions.SetTransactionAnalysis(ctx, chargeStationId, transactionId, *analysis)
	switch {
	case errors.Is(err, store.ErrNotImplemented):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return analysis, nil
}

// Analyse works out the analysis of an ended transaction that was read with its meter
// values (store.TransactionStore.FindTransaction), without storing it.
func (a *Analyzer) Analyse(ctx context.Context, tx *store.Transaction) (*store.TransactionAnalysis, error) {
	if _, _, err := sessionTimes(tx); err != nil {
		return nil, err
	}
	points, err := LoadEnergyPoints(ctx, tx, a.MeterValues)
	if err != nil {
		return nil, err
	}
	return a.AnalyseReadings(ctx, tx, points)
}

// AnalyseReadings is Analyse for a caller that has read the readings of the energy
// register of the transaction already (LoadEnergyPoints, EnergyPoints): nothing is read
// from the meter values.
func (a *Analyzer) AnalyseReadings(ctx context.Context, tx *store.Transaction, points []EnergyPoint) (*store.TransactionAnalysis, error) {
	start, stop, err := sessionTimes(tx)
	if err != nil {
		return nil, err
	}
	settings, err := a.settings(ctx, tx)
	if err != nil {
		return nil, err
	}
	maxPowerW, err := a.maxPowerW(ctx, tx)
	if err != nil {
		return nil, err
	}
	var meterStopWh *float64
	if tx.MeterStopWh != nil {
		wh := float64(*tx.MeterStopWh)
		meterStopWh = &wh
	}
	now := a.now()

	idle := DetectIdle(points, start, &stop, now, meterStopWh, maxPowerW, settings)

	analysis := &store.TransactionAnalysis{
		IdleStartedAt: idle.IdleStartedAt,
		IdleS:         int64(idle.Idle / time.Second),
		AnalysedAt:    now,
	}
	// the rest of the transaction; a transaction whose end is before its start (the clock
	// of the charge station went backwards) has no duration
	if duration := int64(stop.Sub(start) / time.Second); duration > analysis.IdleS {
		analysis.ChargingS = duration - analysis.IdleS
	}

	// the average is over the readings that the idle time was told from, to one decimal
	readings := sessionPoints(points, start, &stop, now, meterStopWh, settings.TimestampTolerance)
	if average := AveragePowerW(readings, settings.MinChargingPowerW); average != nil {
		rounded := math.Round(*average*10) / 10
		// a power that the store cannot hold comes from readings that are wrong
		if rounded >= 0 && rounded < store.MaxAveragePowerW {
			analysis.AveragePowerW = &rounded
		}
	}
	return analysis, nil
}

// IdleNow tells since when a transaction that runs is idle at the moment now, from the
// readings of its energy register (LoadEnergyPoints, EnergyPoints), with the thresholds
// that its analysis will have when it ends. The result has no idle start for a
// transaction that charged when it was last heard of. A transaction whose start is not
// known is store.ErrInvalidState.
func (a *Analyzer) IdleNow(ctx context.Context, tx *store.Transaction, points []EnergyPoint, now time.Time) (IdleResult, error) {
	start, _, known := TransactionTimes(tx)
	if !known {
		return IdleResult{}, fmt.Errorf("the start of the transaction is not known: %w", store.ErrInvalidState)
	}
	settings, err := a.settings(ctx, tx)
	if err != nil {
		return IdleResult{}, err
	}
	maxPowerW, err := a.maxPowerW(ctx, tx)
	if err != nil {
		return IdleResult{}, err
	}
	return DetectIdle(points, start, nil, now, nil, maxPowerW, settings), nil
}

func (a *Analyzer) now() time.Time {
	if a.Clock != nil {
		return a.Clock.Now()
	}
	return time.Now()
}

// sessionTimes returns the start and the end of an ended transaction: the times that are
// stored with it or, for a store that does not keep them, the times of its first and its
// last meter value.
func sessionTimes(tx *store.Transaction) (start, stop time.Time, err error) {
	begin, end, known := TransactionTimes(tx)
	switch {
	case end == nil:
		return start, stop, fmt.Errorf("the transaction has not ended: %w", store.ErrInvalidState)
	case !known:
		return start, stop, fmt.Errorf("the start of the transaction is not known: %w", store.ErrInvalidState)
	}
	return begin, *end, nil
}

// TransactionTimes returns the start and the end of a transaction that was read with its
// meter values: the times that are stored with it or, for a store that does not keep
// them, the times of its first and, when it has ended, of its last meter value. stop is
// nil for a transaction that runs; startKnown is false when the transaction has neither a
// stored start nor a meter value to tell it from.
func TransactionTimes(tx *store.Transaction) (start time.Time, stop *time.Time, startKnown bool) {
	first, last, sampled := meterValueTimes(tx.MeterValues)

	switch {
	case tx.StoppedAt != nil:
		stop = tx.StoppedAt
	case tx.EndedSeqNo > 0 && sampled:
		stop = &last
	}

	switch {
	case tx.StartedAt != nil:
		return *tx.StartedAt, stop, true
	case sampled:
		return first, stop, true
	}
	return start, stop, false
}

// meterValueTimes returns the earliest and the latest time of the meter values; ok is
// false when none has a time that can be read.
func meterValueTimes(meterValues []store.MeterValue) (first, last time.Time, ok bool) {
	for _, meterValue := range meterValues {
		at, err := time.Parse(time.RFC3339, meterValue.Timestamp)
		if err != nil {
			continue
		}
		if !ok || at.Before(first) {
			first = at
		}
		if !ok || at.After(last) {
			last = at
		}
		ok = true
	}
	return first, last, ok
}

// settings returns the idle settings of the transaction: the defaults, with those of the
// tariff version that the transaction is charged with.
func (a *Analyzer) settings(ctx context.Context, tx *store.Transaction) (IdleSettings, error) {
	if a.Tariffs == nil || tx.TariffId == nil || tx.TariffVersion == nil {
		return a.Defaults, nil
	}
	version, err := a.Tariffs.LookupTariffVersion(ctx, *tx.TariffId, *tx.TariffVersion)
	switch {
	case errors.Is(err, store.ErrNotImplemented):
		return a.Defaults, nil
	case err != nil:
		return IdleSettings{}, fmt.Errorf("tariff version %s/%d: %w", *tx.TariffId, *tx.TariffVersion, err)
	case version == nil:
		return a.Defaults, nil
	}
	return MergeIdleSettings(a.Defaults, version.IdleDetection), nil
}

// maxPowerW returns the highest power in W of the connector of the transaction: that of
// the connector in the asset model or, without one, the capacity of the charge station.
// It is 0 when neither is known.
//
// It is a property of the hardware, read from the records of the charge station as they
// are now: it does not depend on the org that the transaction belongs to.
func (a *Analyzer) maxPowerW(ctx context.Context, tx *store.Transaction) (float64, error) {
	if a.Registry == nil {
		return 0, nil
	}
	station, err := stations.Lookup(ctx, a.Registry, tx.ChargeStationId)
	if err != nil {
		return 0, err
	}

	connector, err := a.connector(ctx, tx, station)
	if err != nil {
		return 0, err
	}
	if connector != nil && connector.MaxElectricPower != nil && *connector.MaxElectricPower > 0 {
		return float64(*connector.MaxElectricPower), nil
	}
	if station.MaxCapacityKw != nil && *station.MaxCapacityKw > 0 {
		return *station.MaxCapacityKw * 1000, nil
	}
	return 0, nil
}

// connector returns the connector of the transaction in the asset model; nil when the
// transaction does not say where it ran, when the OCPP version of the charge station is
// not known (the ids of a transaction name a connector by the rules of that version), or
// when the asset model has no such connector.
func (a *Analyzer) connector(ctx context.Context, tx *store.Transaction, station *store.ChargeStation) (*store.AssetConnector, error) {
	if a.Assets == nil || tx.EvseId == nil || tx.ConnectorId == nil || station.Runtime == nil {
		return nil, nil
	}
	var version transport.OcppVersion
	switch station.Runtime.OcppVersion {
	case "1.6":
		version = transport.OcppVersion16
	case "2.0.1":
		version = transport.OcppVersion201
	default:
		return nil, nil
	}
	connector, _, err := assets.ResolveConnector(ctx, a.Assets, tx.ChargeStationId, string(version), *tx.EvseId, *tx.ConnectorId)
	if errors.Is(err, store.ErrNotImplemented) {
		return nil, nil
	}
	return connector, err
}
