// SPDX-License-Identifier: Apache-2.0

// Package sessions works out what the charging sessions cost: it builds the session of
// a stored transaction (rating.Session) from the transaction, its meter values, its
// tariff version, the time zone of its location and its idle time, has rating.Rater
// rate it, and stores the cost with the transaction. A session that runs gets a cost so
// far, an ended session its final cost.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// RatingTimeout is the most time that the handling of a message of a charge station
// gives to one rating of its transaction (RunningCost, UpdateRunningCost, PreviewEnd,
// End). The answer to the charge station does not wait longer for it.
const RatingTimeout = 2 * time.Second

// FallbackTariff is the price of the sessions that have no tariff: a price per kWh
// without VAT.
type FallbackTariff struct {
	Currency    string // ISO 4217
	PricePerKwh money.Amount
}

// version returns the fallback as a tariff version.
func (f FallbackTariff) version() *store.TariffVersion {
	return &store.TariffVersion{
		Currency: f.Currency,
		Elements: []store.TariffElement{{
			PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: f.PricePerKwh, StepSize: 1}},
		}},
	}
}

// Pending is what the message that is being handled tells about a transaction and the
// store does not have yet: for rating a transaction as it will be once the message is
// stored.
type Pending struct {
	// MeterValues are the meter values of the message.
	MeterValues []store.MeterValue
	// StoppedAt and MeterStopWh are the end of the transaction, for the message that
	// ends it.
	StoppedAt   *time.Time
	MeterStopWh *int64
}

// Service rates the transactions of the store.
//
// A transaction is rated with the tariff version that was stored with it when it started
// (store.Transaction.TariffId and TariffVersion): a tariff that got a new version since
// is of no matter. A transaction without a tariff is rated with Fallback, and has no
// price when there is none: the methods then return no result and no error.
//
// Every store but Transactions may be nil, or may not be implemented by the storage
// backend: what it holds is then not known (no tariffs, UTC as the time zone, no idle
// time). Any other error of a store fails the rating: a transaction is not given a cost
// that what could not be read might have changed.
type Service struct {
	Rater rating.Rater
	// Analyzer tells the idle time of a transaction that has no stored analysis: one that
	// runs, or one whose analysis is not stored. Nil: such a transaction has no idle time.
	Analyzer     *rating.Analyzer
	Transactions store.TransactionStore
	MeterValues  store.MeterValuesStore
	Tariffs      store.TariffStore
	Assets       store.AssetStore
	Registry     store.ChargeStationRegistryStore
	// Tx gives the database transaction in which the cost of an ended transaction is
	// worked out (PreviewEnd, End): there the reads are on the primary, which has what
	// was written a moment ago also while a read replica is behind. Nil: none.
	Tx store.Transactor
	// Clock gives the end so far of a transaction that runs and the time of a rating.
	// Nil is the real clock.
	Clock clock.PassiveClock
	// Fallback is the price of the transactions without a tariff. Nil: they have no price.
	Fallback *FallbackTariff
	// RunningEvery is the least time between two ratings of a transaction that runs
	// (RunningCost, UpdateRunningCost). 0 rates with every call.
	RunningEvery time.Duration

	mu sync.Mutex
	// lastRunning is when the cost so far of a transaction was last worked out, by
	// runningKey
	lastRunning map[string]time.Time
}

// Rate works out what the transaction costs from what is stored of it, without storing
// anything. It returns the price and the session that it was worked out from.
//
// With final the transaction must have ended (store.ErrInvalidState otherwise) and the
// result is its final cost. Without final the result is the cost so far of a transaction
// that runs, up to the time of the clock; a transaction that has ended has no cost so
// far, and no result. A transaction without a price has no result either. A transaction
// that does not exist is store.ErrNotFound.
func (s *Service) Rate(ctx context.Context, chargeStationId, transactionId string, final bool) (*rating.Result, *rating.Session, error) {
	return s.RateWith(ctx, chargeStationId, transactionId, final, Pending{})
}

// RateWith is Rate for a transaction of which a message that is not stored yet tells
// more: the transaction is rated as it will be with pending.
func (s *Service) RateWith(ctx context.Context, chargeStationId, transactionId string, final bool, pending Pending) (*rating.Result, *rating.Session, error) {
	result, session, err := s.rate(ctx, chargeStationId, transactionId, final, pending)
	if err != nil {
		return nil, nil, fmt.Errorf("rate transaction %s/%s: %w", chargeStationId, transactionId, err)
	}
	return result, session, nil
}

func (s *Service) rate(ctx context.Context, chargeStationId, transactionId string, final bool, pending Pending) (*rating.Result, *rating.Session, error) {
	// the tariff first, from the transaction without its meter values: a transaction
	// without a price is not read in full
	summary, err := s.Transactions.FindTransactionSummary(ctx, chargeStationId, transactionId)
	if err != nil {
		return nil, nil, err
	}
	if summary == nil {
		return nil, nil, store.ErrNotFound
	}
	if !final && hasEnded(summary) {
		return nil, nil, nil
	}
	tariffId, version, err := s.tariffVersion(ctx, summary)
	if err != nil || version == nil {
		return nil, nil, err
	}

	stored, err := s.Transactions.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil {
		return nil, nil, err
	}
	if stored == nil {
		return nil, nil, store.ErrNotFound
	}
	session, err := s.session(ctx, withPending(stored, pending), final)
	if err != nil {
		return nil, nil, err
	}
	result, err := s.Rater.Rate(tariffId, version, *session)
	if err != nil {
		return nil, nil, err
	}
	return result, session, nil
}

// hasEnded reports whether the record of a transaction says that it has ended.
func hasEnded(tx *store.Transaction) bool {
	return tx.StoppedAt != nil || tx.EndedSeqNo > 0
}

// withPending returns a copy of the transaction with what the message that is being
// handled adds to it. The stored transaction is left as it is.
func withPending(stored *store.Transaction, pending Pending) *store.Transaction {
	tx := *stored
	tx.MeterValues = make([]store.MeterValue, 0, len(stored.MeterValues)+len(pending.MeterValues))
	tx.MeterValues = append(append(tx.MeterValues, stored.MeterValues...), pending.MeterValues...)
	if pending.StoppedAt != nil {
		tx.StoppedAt = pending.StoppedAt
	}
	if pending.MeterStopWh != nil {
		// the energy of the transaction follows from it (totalEnergyWh)
		tx.MeterStopWh = pending.MeterStopWh
	}
	return &tx
}

// tariffVersion returns the tariff version that the transaction is rated with and the id
// of its tariff: the version that was stored with the transaction or, for a transaction
// without a tariff, the fallback (which has no tariff id). The version is nil for a
// transaction without a price.
func (s *Service) tariffVersion(ctx context.Context, tx *store.Transaction) (string, *store.TariffVersion, error) {
	if tx.TariffId != nil && tx.TariffVersion != nil && s.Tariffs != nil {
		version, err := s.Tariffs.LookupTariffVersion(ctx, *tx.TariffId, *tx.TariffVersion)
		switch {
		case errors.Is(err, store.ErrNotImplemented):
			// a backend without tariffs: as a transaction without one
		case err != nil:
			return "", nil, fmt.Errorf("tariff version %s/%d: %w", *tx.TariffId, *tx.TariffVersion, err)
		case version == nil:
			// not rated with another price than the one that the session started under
			return "", nil, fmt.Errorf("tariff version %s/%d: %w", *tx.TariffId, *tx.TariffVersion, store.ErrNotFound)
		default:
			return *tx.TariffId, version, nil
		}
	}
	if s.Fallback == nil {
		return "", nil, nil
	}
	return "", s.Fallback.version(), nil
}

// session builds the session of a transaction that was read with its meter values.
func (s *Service) session(ctx context.Context, tx *store.Transaction, final bool) (*rating.Session, error) {
	start, stop, startKnown := rating.TransactionTimes(tx)
	switch {
	case !startKnown:
		return nil, fmt.Errorf("the start of the transaction is not known: %w", store.ErrInvalidState)
	case final && stop == nil:
		return nil, fmt.Errorf("the transaction has not ended: %w", store.ErrInvalidState)
	}

	meterValues, err := rating.LoadMeterValues(ctx, tx, s.MeterValues)
	if err != nil {
		return nil, err
	}
	points := rating.EnergyPoints(tx, meterValues)
	location, err := s.location(ctx, tx)
	if err != nil {
		return nil, err
	}

	session := &rating.Session{
		Start:    start,
		Stop:     stop,
		Location: location,
		Samples:  samples(tx, points, meterValues),
	}
	if stop != nil {
		session.TotalEnergyWh = totalEnergyWh(tx)
		analysis := tx.Analysis
		if analysis == nil && s.Analyzer != nil {
			if analysis, err = s.Analyzer.AnalyseReadings(ctx, tx, points); err != nil {
				return nil, err
			}
		}
		if analysis != nil {
			session.IdleStartedAt = analysis.IdleStartedAt
		}
		return session, nil
	}

	// the end so far is the time of the manager, or the time of the newest reading when
	// the clock of the charge station is ahead of it
	session.Now = s.now()
	if session.Now.Before(start) {
		session.Now = start
	}
	if len(points) > 0 && points[len(points)-1].At.After(session.Now) {
		session.Now = points[len(points)-1].At
	}
	if s.Analyzer != nil {
		idle, err := s.Analyzer.IdleNow(ctx, tx, points, session.Now)
		if err != nil {
			return nil, err
		}
		session.IdleStartedAt = idle.IdleStartedAt
	}
	return session, nil
}

// totalEnergyWh returns the energy of an ended transaction, when its record has it: the
// stored energy, or the meter stop less the meter start. Nil leaves it to the readings.
func totalEnergyWh(tx *store.Transaction) *float64 {
	var wh float64
	switch {
	case tx.EnergyWh != nil && *tx.EnergyWh >= 0:
		wh = float64(*tx.EnergyWh)
	case tx.MeterStartWh != nil && tx.MeterStopWh != nil && *tx.MeterStopWh >= *tx.MeterStartWh:
		wh = float64(*tx.MeterStopWh - *tx.MeterStartWh)
	default:
		return nil
	}
	return &wh
}

// samples returns the samples of a session: for each reading of the energy register the
// energy since the start of the transaction, which is the reading less the meter start
// (less the earliest reading when the meter start is not known), with the current that
// was measured at the same moment.
func samples(tx *store.Transaction, points []rating.EnergyPoint, meterValues []store.MeterValue) []rating.Sample {
	if len(points) == 0 {
		return nil
	}
	base := points[0].RegisterWh
	if tx.MeterStartWh != nil {
		base = float64(*tx.MeterStartWh)
	}
	measured := currents(meterValues)

	result := make([]rating.Sample, 0, len(points))
	for _, point := range points {
		sample := rating.Sample{At: point.At, EnergyWh: point.RegisterWh - base}
		if current, ok := measured[point.At.UnixNano()]; ok {
			sample.CurrentA = &current
		}
		result = append(result, sample)
	}
	return result
}

// Values of the fields of a sampled value (the same in OCPP 1.6 and 2.0.1) that say that
// it is the current that the connector delivers.
const (
	measurandCurrentImport = "Current.Import"
	locationOutlet         = "Outlet"
	unitAmpere             = "A"
)

// phaseLines maps the phase of a sampled value to the line that it measures.
var phaseLines = map[string]int{"L1": 1, "L2": 2, "L3": 3, "L1-N": 1, "L2-N": 2, "L3-N": 3}

// currents returns the current in A of each meter value that has one, by the time of the
// meter value in nanoseconds: the sum of the Current.Import values of the lines, or the
// value that is given without a phase. Values at another place than the outlet, in
// another unit than A, of the neutral conductor or between two lines are not used.
func currents(meterValues []store.MeterValue) map[int64]float64 {
	measured := map[int64]float64{}
	for _, meterValue := range meterValues {
		at, err := time.Parse(time.RFC3339, meterValue.Timestamp)
		if err != nil {
			continue
		}
		var whole *float64
		lines := map[int]float64{}
		for _, sv := range meterValue.SampledValues {
			if sv.Measurand == nil || *sv.Measurand != measurandCurrentImport {
				continue
			}
			if sv.Location != nil && *sv.Location != locationOutlet {
				continue
			}
			current := sv.Value
			if unit := sv.UnitOfMeasure; unit != nil {
				if unit.Unit != "" && unit.Unit != unitAmpere {
					continue
				}
				current *= math.Pow10(unit.Multipler)
			}
			if math.IsNaN(current) || math.IsInf(current, 0) {
				continue
			}
			if sv.Phase == nil {
				if whole == nil {
					whole = &current
				}
				continue
			}
			if line, ok := phaseLines[*sv.Phase]; ok {
				if _, seen := lines[line]; !seen {
					lines[line] = current
				}
			}
		}
		switch {
		case whole != nil:
			measured[at.UnixNano()] = *whole
		case len(lines) > 0:
			sum := 0.0
			for _, current := range lines {
				sum += current
			}
			measured[at.UnixNano()] = sum
		}
	}
	return measured
}

// location returns the time zone of the place of the transaction: that of the location of
// its EVSE in the asset model or, when the charge station has no such EVSE there, of the
// location of the charge station. It is UTC when the place is not known.
//
// The place is read from the records of the charge station as they are now: it is where
// the hardware stands, whatever org the transaction belongs to.
func (s *Service) location(ctx context.Context, tx *store.Transaction) (*time.Location, error) {
	if s.Registry == nil || s.Assets == nil {
		return time.UTC, nil
	}
	station, err := stations.Lookup(ctx, s.Registry, tx.ChargeStationId)
	if err != nil {
		return nil, err
	}

	locationId := ""
	if station.LocationId != nil {
		locationId = *station.LocationId
	}
	// the EVSE id of a transaction is the id of the EVSE in the asset model, also for OCPP
	// 1.6, where it is the connector id
	if tx.EvseId != nil && *tx.EvseId > 0 {
		evse, err := s.Assets.LookupAssetEvseByOcpp(ctx, tx.ChargeStationId, *tx.EvseId)
		switch {
		case errors.Is(err, store.ErrNotImplemented):
			return time.UTC, nil
		case err != nil:
			return nil, fmt.Errorf("EVSE %d of %s: %w", *tx.EvseId, tx.ChargeStationId, err)
		case evse != nil:
			locationId = evse.LocationId
		}
	}
	if locationId == "" {
		return time.UTC, nil
	}

	location, err := s.Assets.LookupAssetLocation(ctx, locationId)
	switch {
	case errors.Is(err, store.ErrNotImplemented):
		return time.UTC, nil
	case err != nil:
		return nil, fmt.Errorf("location %s: %w", locationId, err)
	case location == nil || location.TimeZone == "":
		return time.UTC, nil
	}
	zone, err := time.LoadLocation(location.TimeZone)
	if err != nil || location.TimeZone == "Local" {
		// not rated with the times of day of another place
		return nil, fmt.Errorf("time zone %q of location %s is not known", location.TimeZone, locationId)
	}
	return zone, nil
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock.Now()
	}
	return time.Now()
}

// StoreCost stores the cost of a rating with the transaction: the total without and with
// VAT, and whether it is final. A backend that keeps only the last cost of a transaction
// (store.ErrNotImplemented) gets the amount with VAT as that. No result stores nothing.
func (s *Service) StoreCost(ctx context.Context, chargeStationId, transactionId string, result *rating.Result) error {
	if result == nil {
		return nil
	}
	cost := store.TransactionCost{
		Currency: result.Currency,
		Total:    result.Breakdown.Total,
		Final:    result.Final,
		RatedAt:  s.now(),
	}
	err := s.Transactions.SetTransactionCost(ctx, chargeStationId, transactionId, cost)
	if errors.Is(err, store.ErrNotImplemented) {
		inclVat, _ := cost.Total.InclVat.Rat().Float64()
		err = s.Transactions.UpdateTransactionCost(ctx, chargeStationId, transactionId, inclVat)
	}
	if err != nil {
		return fmt.Errorf("store the cost of transaction %s/%s: %w", chargeStationId, transactionId, err)
	}
	return nil
}

// RunningCost works out the cost so far of a transaction that runs, for the handler of a
// message with meter values of the transaction, which are given as pending when they are
// not stored yet. Nothing is stored (StoreCost does that).
//
// The cost so far of a transaction is worked out at most once in RunningEvery: a call
// within that time of the one before it returns no result, and so does a call for a
// transaction that has ended or has no price. The rating gets at most RatingTimeout, and
// a rating that panics gives an error.
func (s *Service) RunningCost(ctx context.Context, chargeStationId, transactionId string, pending []store.MeterValue) (*rating.Result, error) {
	if !s.runningCostDue(runningKey(chargeStationId, transactionId)) {
		return nil, nil
	}
	return s.bounded(ctx, chargeStationId, transactionId, false, func(ctx context.Context) (*rating.Result, error) {
		result, _, err := s.RateWith(ctx, chargeStationId, transactionId, false, Pending{MeterValues: pending})
		return result, err
	})
}

// UpdateRunningCost is RunningCost for a transaction whose meter values are stored, and
// stores the cost so far with the transaction.
func (s *Service) UpdateRunningCost(ctx context.Context, chargeStationId, transactionId string) (*rating.Result, error) {
	if !s.runningCostDue(runningKey(chargeStationId, transactionId)) {
		return nil, nil
	}
	return s.bounded(ctx, chargeStationId, transactionId, false, func(ctx context.Context) (*rating.Result, error) {
		result, _, err := s.RateWith(ctx, chargeStationId, transactionId, false, Pending{})
		if err != nil || result == nil {
			return nil, err
		}
		if err := s.StoreCost(ctx, chargeStationId, transactionId, result); err != nil {
			return nil, err
		}
		return result, nil
	})
}

// PreviewEnd works out the final cost of a transaction that the message that is being
// handled ends, before the end is stored: pending holds the end and the meter values of
// the message. Nothing is stored. The rating gets at most RatingTimeout, runs in a
// database transaction of Tx, and a rating that panics gives an error.
func (s *Service) PreviewEnd(ctx context.Context, chargeStationId, transactionId string, pending Pending) (*rating.Result, error) {
	return s.bounded(ctx, chargeStationId, transactionId, true, func(ctx context.Context) (*rating.Result, error) {
		result, _, err := s.RateWith(ctx, chargeStationId, transactionId, true, pending)
		return result, err
	})
}

// End works out the final cost of a transaction that has ended, from what is stored of
// it, and stores it with the transaction. Its idle time is that of its stored analysis;
// without one the Analyzer works it out. The rating gets at most RatingTimeout, runs in a
// database transaction of Tx (when it fails nothing of it is stored), and a rating that
// panics gives an error.
func (s *Service) End(ctx context.Context, chargeStationId, transactionId string) (*rating.Result, error) {
	s.forgetRunning(runningKey(chargeStationId, transactionId))
	return s.bounded(ctx, chargeStationId, transactionId, true, func(ctx context.Context) (*rating.Result, error) {
		result, _, err := s.RateWith(ctx, chargeStationId, transactionId, true, Pending{})
		if err != nil || result == nil {
			return nil, err
		}
		if err := s.StoreCost(ctx, chargeStationId, transactionId, result); err != nil {
			return nil, err
		}
		return result, nil
	})
}

// bounded runs a rating with at most RatingTimeout, in a database transaction of Tx when
// inTx, and turns a panic into an error.
func (s *Service) bounded(ctx context.Context, chargeStationId, transactionId string, inTx bool,
	rate func(ctx context.Context) (*rating.Result, error)) (result *rating.Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, RatingTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("rate transaction %s/%s: panic: %v", chargeStationId, transactionId, r)
		}
	}()

	if !inTx || s.Tx == nil {
		return rate(ctx)
	}
	err = s.Tx.RunInTx(ctx, func(ctx context.Context) (err error) {
		result, err = rate(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// The times of the last ratings of the transactions that ran are forgotten when there
// are throttleSize of them, those of the transactions that were not rated for
// throttleKeep (or twice RunningEvery, when that is longer): a transaction whose end the
// manager never saw.
const (
	throttleSize = 1024
	throttleKeep = 24 * time.Hour
)

func runningKey(chargeStationId, transactionId string) string {
	return chargeStationId + "/" + transactionId
}

// runningCostDue reports whether the cost so far of the transaction may be worked out
// now, and notes that it is: the next call within RunningEvery is not due, whatever
// comes of this one. So a rating that fails or is slow is not tried with every message.
func (s *Service) runningCostDue(key string) bool {
	if s.RunningEvery <= 0 {
		return true
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if last, rated := s.lastRunning[key]; rated {
		// a clock that was set back does not hold the next rating up
		if since := now.Sub(last); since >= 0 && since < s.RunningEvery {
			return false
		}
	}
	if s.lastRunning == nil {
		s.lastRunning = map[string]time.Time{}
	}
	if len(s.lastRunning) >= throttleSize {
		keep := max(throttleKeep, 2*s.RunningEvery)
		for other, last := range s.lastRunning {
			if now.Sub(last) > keep {
				delete(s.lastRunning, other)
			}
		}
	}
	s.lastRunning[key] = now
	return true
}

func (s *Service) forgetRunning(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.lastRunning, key)
}

// EventCost returns the cost of a rating as it is given in the session events; nil
// without a result.
func EventCost(result *rating.Result) *events.SessionCost {
	if result == nil {
		return nil
	}
	cost := &events.SessionCost{
		Currency:  result.Currency,
		ExclVat:   result.Breakdown.Total.ExclVat,
		InclVat:   result.Breakdown.Total.InclVat,
		Final:     result.Final,
		EnergyWh:  result.TotalEnergyWh,
		ChargingS: result.ChargingTimeS,
		ParkingS:  result.ParkingTimeS,
	}
	if result.TariffId != "" {
		tariffId, version := result.TariffId, result.TariffVersion
		cost.TariffId, cost.TariffVersion = &tariffId, &version
	}
	return cost
}
