// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"math"
	"sort"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The idle settings of a manager that configures none.
const (
	DefaultChargingPowerShare = 0.01
	DefaultMinChargingPowerW  = 200.0
	DefaultTimestampTolerance = time.Hour
	DefaultFreshSession       = time.Minute
)

// IdleSettings are the thresholds for telling whether a session charges or is idle
// (plugged in without charging).
type IdleSettings struct {
	// ChargingPowerShare is the share of the highest power of the connector from which a
	// session charges: 0.01 is 1 %.
	ChargingPowerShare float64
	// MinChargingPowerW is the lowest power in W that counts as charging, whatever the
	// highest power of the connector.
	MinChargingPowerW float64
	// TimestampTolerance is how long before the start or after the end of a session a
	// reading may be timed and still be used. A reading further away comes from a clock
	// that was wrong, and is ignored.
	TimestampTolerance time.Duration
	// FreshSession is how long after its start a session is never said to be parking
	// (IsParkingNow).
	FreshSession time.Duration
}

// DefaultIdleSettings returns the idle settings of a manager that configures none.
func DefaultIdleSettings() IdleSettings {
	return IdleSettings{
		ChargingPowerShare: DefaultChargingPowerShare,
		MinChargingPowerW:  DefaultMinChargingPowerW,
		TimestampTolerance: DefaultTimestampTolerance,
		FreshSession:       DefaultFreshSession,
	}
}

// chargingThresholdW returns the power in W from which a session at a connector with the
// highest power maxPowerW charges: the higher of the minimum charging power and the share
// of maxPowerW. maxPowerW is 0 when it is not known; the minimum charging power decides
// then.
func (s IdleSettings) chargingThresholdW(maxPowerW float64) float64 {
	return math.Max(s.MinChargingPowerW, maxPowerW*s.ChargingPowerShare)
}

// MergeIdleSettings returns the defaults with the settings of a tariff version in place
// of those that the tariff version sets. A tariff version sets the two powers only.
func MergeIdleSettings(defaults IdleSettings, override *store.IdleDetection) IdleSettings {
	merged := defaults
	if override == nil {
		return merged
	}
	if override.ChargingPowerShare != nil {
		merged.ChargingPowerShare = *override.ChargingPowerShare
	}
	if override.MinChargingPowerW != nil {
		merged.MinChargingPowerW = *override.MinChargingPowerW
	}
	return merged
}

// EnergyPoint is a reading of the energy register of a connector.
type EnergyPoint struct {
	At         time.Time
	RegisterWh float64 // the reading of the register in Wh, not the energy of the session
}

// IdleResult says since when and for how long a session is idle.
type IdleResult struct {
	// IdleStartedAt is when the session stopped charging. It is nil for a session that
	// still charges, and for a session without a reading.
	IdleStartedAt *time.Time
	// Idle is the time from IdleStartedAt to the end of the session, or to now for a
	// session that runs.
	Idle time.Duration
}

// DetectIdle tells since when a session is idle, from the readings of the energy
// register. stop is the end of the session, nil for a session that runs: now is its end
// so far. meterStopWh is the reading at the stop, when it is known; maxPowerW is the
// highest power of the connector in W, 0 when it is not known.
//
// The power of the session is measured between two readings that differ: a reading that
// is not higher than the one before it is passed over, and the next higher reading is
// compared with the last one that was. A register that moves in steps of 100 Wh under a
// charger that charges slowly therefore still shows charging, while a step now and then
// under a parked car does not. The session charges in every such interval whose power
// reaches the threshold (the higher of s.MinChargingPowerW and s.ChargingPowerShare of
// maxPowerW), and it is idle from the end of the last one.
//
//   - A session that never charged is idle from its start.
//   - A session that runs and charged up to its newest reading is not idle.
//   - A session without a reading is not idle either: there is nothing to tell from.
//
// The readings may come in any order. Those timed more than s.TimestampTolerance before
// the start or after the end are ignored. The idle time is never longer than the session,
// and IdleStartedAt is never before its start.
func DetectIdle(points []EnergyPoint, start time.Time, stop *time.Time, now time.Time,
	meterStopWh *float64, maxPowerW float64, s IdleSettings) IdleResult {
	end := now
	if stop != nil {
		end = *stop
	}
	readings := sessionPoints(points, start, stop, now, meterStopWh, s.TimestampTolerance)
	if len(readings) == 0 {
		return IdleResult{}
	}
	threshold := s.chargingThresholdW(maxPowerW)

	var chargingEnd *time.Time
	prev := readings[0]
	for _, p := range readings[1:] {
		if p.RegisterWh <= prev.RegisterWh {
			// no energy since prev: the power is measured up to the next reading that
			// has some
			continue
		}
		if hours := p.At.Sub(prev.At).Hours(); hours > 0 && (p.RegisterWh-prev.RegisterWh)/hours >= threshold {
			at := p.At
			chargingEnd = &at
		}
		prev = p
	}

	idleStart := start
	if chargingEnd != nil {
		newest := readings[len(readings)-1].At
		if stop == nil && !chargingEnd.Before(newest) {
			// charging when it was last heard of
			return IdleResult{}
		}
		if chargingEnd.After(start) {
			idleStart = *chargingEnd
		}
		if idleStart.After(end) {
			idleStart = end
		}
	}
	idle := end.Sub(idleStart)
	if idle < 0 {
		idle = 0
	}
	return IdleResult{IdleStartedAt: &idleStart, Idle: idle}
}

// AveragePowerW returns the average power in W of the intervals between two readings
// that follow each other in time in which the power was above minW: the energy of those
// intervals divided by their time. It is 0 when there is no such interval, and nil when
// there are no two readings to measure between. The readings may come in any order.
//
// Unlike DetectIdle it measures between every two readings, also when the register did
// not move: under a register that moves in coarse steps it counts only the intervals
// with a step.
func AveragePowerW(points []EnergyPoint, minW float64) *float64 {
	if len(points) < 2 {
		return nil
	}
	readings := make([]EnergyPoint, len(points))
	copy(readings, points)
	sortPoints(readings)

	var wh, hours float64
	for i := 1; i < len(readings); i++ {
		h := readings[i].At.Sub(readings[i-1].At).Hours()
		if h <= 0 {
			continue
		}
		if delta := readings[i].RegisterWh - readings[i-1].RegisterWh; delta/h > minW {
			wh += delta
			hours += h
		}
	}
	average := 0.0
	if hours > 0 {
		average = wh / hours
	}
	return &average
}

// IsParkingNow reports whether a session that runs looks parked at the moment now: for
// showing it so, not for charging it (DetectIdle is for that). It is false in the first
// s.FreshSession after the start, and while there are no two readings to measure between.
// After that it is true when the power between the two newest readings is at most
// s.ChargingPowerShare of maxPowerW, the highest power of the connector in W (0 when it
// is not known: only a session that draws nothing is parked then).
func IsParkingNow(points []EnergyPoint, start, now time.Time, maxPowerW float64, s IdleSettings) bool {
	if now.Sub(start) < s.FreshSession {
		return false
	}
	readings := sessionPoints(points, start, nil, now, nil, s.TimestampTolerance)
	if len(readings) < 2 {
		return false
	}
	newest := readings[len(readings)-1]
	for i := len(readings) - 2; i >= 0; i-- {
		// readings of the same moment have no power between them
		if hours := newest.At.Sub(readings[i].At).Hours(); hours > 0 {
			return (newest.RegisterWh-readings[i].RegisterWh)/hours <= maxPowerW*s.ChargingPowerShare
		}
	}
	return false
}

// sessionPoints returns the readings that tell about a session, in the order of their
// time: a copy of points with the reading at the stop when stop and meterStopWh are both
// known, without the readings timed more than tolerance before the start or after the
// end (stop, or now for a session that runs) and without those that are not a number.
func sessionPoints(points []EnergyPoint, start time.Time, stop *time.Time, now time.Time,
	meterStopWh *float64, tolerance time.Duration) []EnergyPoint {
	end := now
	if stop != nil {
		end = *stop
	}
	earliest, latest := start.Add(-tolerance), end.Add(tolerance)

	readings := make([]EnergyPoint, 0, len(points)+1)
	add := func(p EnergyPoint) {
		if p.At.Before(earliest) || p.At.After(latest) || math.IsNaN(p.RegisterWh) || math.IsInf(p.RegisterWh, 0) {
			return
		}
		readings = append(readings, p)
	}
	for _, p := range points {
		add(p)
	}
	if stop != nil && meterStopWh != nil {
		add(EnergyPoint{At: *stop, RegisterWh: *meterStopWh})
	}
	sortPoints(readings)
	return readings
}

// sortPoints orders readings by their time. Readings of the same time are ordered by the
// register, which only counts up, so that the result does not depend on the order in
// which they came.
func sortPoints(points []EnergyPoint) {
	sort.SliceStable(points, func(i, j int) bool {
		if !points[i].At.Equal(points[j].At) {
			return points[i].At.Before(points[j].At)
		}
		return points[i].RegisterWh < points[j].RegisterWh
	})
}
