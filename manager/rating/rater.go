// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Sample is what is known of a session at one moment: the energy that it has taken since
// its start and, when the charge station measured it, the current that it draws.
type Sample struct {
	At time.Time `json:"at"`
	// EnergyWh is the energy since the start of the session: the reading of the register
	// less the reading at the start.
	EnergyWh float64 `json:"energy_wh"`
	// CurrentA is the current in A, the sum over the phases; nil when it was not measured.
	CurrentA *float64 `json:"current_a,omitempty"`
}

// Session is what the price of a charging session is worked out from. It holds no
// reference to the store, so that a session can be kept and rated again later with
// another tariff version.
type Session struct {
	Start time.Time
	// Stop is the end of the session; nil while it runs.
	Stop *time.Time
	// Now is the end so far of a session that runs. It is not used when Stop is set.
	Now time.Time
	// Location is the time zone of the place of the session, in which the times of day,
	// the dates and the weekdays of the tariff restrictions are read. Nil is UTC.
	Location *time.Location
	// Samples may come in any order. Those outside of the session are not used.
	Samples []Sample
	// TotalEnergyWh is the energy of an ended session: the curve of the samples is closed
	// with it at Stop. Nil closes it with the newest sample.
	TotalEnergyWh *float64
	// IdleStartedAt is when the session stopped charging; nil for a session that charges
	// or of which that is not known.
	IdleStartedAt *time.Time
}

// sessionDocument is the JSON form of a Session.
type sessionDocument struct {
	Start         time.Time  `json:"start"`
	Stop          *time.Time `json:"stop,omitempty"`
	Now           *time.Time `json:"now,omitempty"`
	TimeZone      string     `json:"time_zone,omitempty"` // IANA name; left out for UTC
	Samples       []Sample   `json:"samples"`
	TotalEnergyWh *float64   `json:"total_energy_wh,omitempty"`
	IdleStartedAt *time.Time `json:"idle_started_at,omitempty"`
}

// MarshalJSON writes the session with its time zone by name.
func (s Session) MarshalJSON() ([]byte, error) {
	document := sessionDocument{
		Start:         s.Start,
		Stop:          s.Stop,
		Samples:       s.Samples,
		TotalEnergyWh: s.TotalEnergyWh,
		IdleStartedAt: s.IdleStartedAt,
	}
	if !s.Now.IsZero() {
		document.Now = &s.Now
	}
	if s.Location != nil && s.Location != time.UTC {
		document.TimeZone = s.Location.String()
	}
	if document.Samples == nil {
		document.Samples = []Sample{}
	}
	return json.Marshal(document)
}

// UnmarshalJSON reads what MarshalJSON writes. A time zone that the time zone database
// does not have is an error.
func (s *Session) UnmarshalJSON(b []byte) error {
	var document sessionDocument
	if err := json.Unmarshal(b, &document); err != nil {
		return err
	}
	session := Session{
		Start:         document.Start,
		Stop:          document.Stop,
		Samples:       document.Samples,
		TotalEnergyWh: document.TotalEnergyWh,
		IdleStartedAt: document.IdleStartedAt,
	}
	if document.Now != nil {
		session.Now = *document.Now
	}
	if document.TimeZone != "" {
		location, err := time.LoadLocation(document.TimeZone)
		if err != nil {
			return fmt.Errorf("time zone %q of the session: %w", document.TimeZone, err)
		}
		session.Location = location
	}
	*s = session
	return nil
}

// The dimensions of a charging period (OCPI CdrDimensionType).
const (
	DimensionEnergy      = "ENERGY"
	DimensionTime        = "TIME"
	DimensionParkingTime = "PARKING_TIME"
)

// Dimension is a measured fact of a charging period (OCPI CdrDimension).
type Dimension struct {
	Type string `json:"type"` // ENERGY | TIME | PARKING_TIME
	// Volume is in kWh (ENERGY) or in hours (TIME: charging, PARKING_TIME: plugged in
	// without charging), to 4 decimals.
	Volume float64 `json:"volume"`
}

// ChargingPeriod is a part of a session during which the same prices applied (OCPI
// ChargingPeriod). Its dimensions say what happened in it, whether or not it was charged.
type ChargingPeriod struct {
	StartDateTime time.Time   `json:"start_date_time"`
	Dimensions    []Dimension `json:"dimensions"`
	TariffId      string      `json:"tariff_id,omitempty"`
}

// Result is the price of a session with a tariff version.
type Result struct {
	Currency      string
	TariffId      string
	TariffVersion int
	// Final is false for the cost so far of a session that runs.
	Final           bool
	ChargingPeriods []ChargingPeriod
	Breakdown       PriceBreakdown
	TotalEnergyWh   int64
	// TotalTimeS is the duration of the session, ParkingTimeS the part of it after the
	// session stopped charging and ChargingTimeS the rest, in seconds.
	TotalTimeS    int64
	ChargingTimeS int64
	ParkingTimeS  int64
}

// Rounding says how the amounts of a price are rounded.
type Rounding struct {
	Scale int                // decimals, 0 to 4
	Mode  money.RoundingMode // what happens to a half
}

// DefaultRounding is the rounding of a Rater that sets none: 2 decimals, a half away from
// zero.
func DefaultRounding() Rounding {
	return Rounding{Scale: 2, Mode: money.RoundHalfUp}
}

// Rater works out the price of a session with a tariff version, by the rules of OCPI
// 2.2.1 and of the extensions of the tariff model (a grace period and a price per step for
// the time dimensions). It reads nothing but what it is given.
//
// The zero Rater rounds with DefaultRounding: a Rounding without a mode is not set.
type Rater struct {
	Rounding Rounding
}

func (r Rater) rounding() Rounding {
	if r.Rounding.Mode == "" {
		return DefaultRounding()
	}
	return r.Rounding
}

// Rate returns the price of the session with the tariff version v of the tariff tariffId.
//
// The session is cut into segments at every moment at which a price can change: the
// samples, the start of the idle time, the ends of the grace periods, and the moments at
// which a restriction of a tariff element starts or stops to hold (a time of day, a
// midnight, a date, a duration, an amount of energy). The energy of a segment is read
// from the curve of the samples, which is a straight line between two samples.
//
// In each segment and for each of ENERGY, TIME and PARKING_TIME, the price component that
// applies is that of the first tariff element, in the order of the tariff, whose
// restrictions hold at the start of the segment and which has a component of that
// dimension. Without one the dimension is free in the segment.
//
//   - ENERGY is charged for the energy of the segment.
//   - TIME is charged for the time of a segment in which the session charges, from the end
//     of the grace period of TIME, which starts with the session.
//   - PARKING_TIME is charged for the time of a segment in which the session is idle, from
//     the end of the grace period of PARKING_TIME, which starts with the idle time. A
//     grace period runs on the clock, whether or not a price applies in it, and only the
//     time after it is charged.
//   - FLAT is charged once, with the first segment in which an element with a FLAT
//     component applies.
//
// The quantity of a dimension that was charged is rounded up to a whole number of steps
// of the component that applied last, and the difference is charged at the price of that
// component (OCPI step_size). The price of each of the four parts is rounded on its own,
// without and with VAT (the VAT of the component of each term; none means that the
// amount with VAT is the amount without). The total is the sum of the rounded parts, and
// is then held to the minimum and the maximum price of the tariff.
//
// Elements for a reservation are passed over. A session whose end is before its start
// cannot be rated.
func (r Rater) Rate(tariffId string, v *store.TariffVersion, s Session) (*Result, error) {
	if v == nil {
		return nil, errors.New("rate a session: no tariff version")
	}
	end := s.Now
	if s.Stop != nil {
		end = *s.Stop
	}
	if end.Before(s.Start) {
		return nil, fmt.Errorf("rate a session: it ends at %s, before its start at %s",
			end.UTC().Format(time.RFC3339), s.Start.UTC().Format(time.RFC3339))
	}

	c := newCalculation(v, s, end)
	segments := c.segments()

	rounding := r.rounding()
	breakdown := c.price(segments, rounding)

	result := &Result{
		Currency:        v.Currency,
		TariffId:        tariffId,
		TariffVersion:   v.Version,
		Final:           s.Stop != nil,
		ChargingPeriods: chargingPeriods(segments, tariffId),
		Breakdown:       breakdown,
		TotalEnergyWh:   roundToInt(c.curve.total()),
		TotalTimeS:      breakdown.Time.TotalS + breakdown.Parking.TotalS,
		ChargingTimeS:   breakdown.Time.TotalS,
		ParkingTimeS:    breakdown.Parking.TotalS,
	}
	return result, nil
}

// calculation holds what the rating of one session works with. Moments of the session
// are offsets from its start in nanoseconds, as exact rational numbers: the moment at
// which the energy reaches a threshold lies between two nanoseconds.
type calculation struct {
	version  *store.TariffVersion
	session  Session
	location *time.Location
	// duration is the length of the session
	duration *big.Rat
	curve    energyCurve
	// idleAt is the offset from which the session is idle; nil for a session that charges
	// to its end
	idleAt *big.Rat
	// timeGraceEnd and parkingGraceEnd are the offsets at which the grace periods of the
	// two time dimensions end; parkingGraceEnd is nil without idle time
	timeGraceEnd    *big.Rat
	parkingGraceEnd *big.Rat
}

func newCalculation(v *store.TariffVersion, s Session, end time.Time) *calculation {
	c := &calculation{
		version:  v,
		session:  s,
		location: s.Location,
		duration: offsetOf(end.Sub(s.Start)),
	}
	if c.location == nil {
		c.location = time.UTC
	}
	c.curve = newEnergyCurve(s, end)

	if s.IdleStartedAt != nil && s.IdleStartedAt.Before(end) {
		// a session is not idle before it starts
		idleAt := new(big.Rat)
		if s.IdleStartedAt.After(s.Start) {
			idleAt = offsetOf(s.IdleStartedAt.Sub(s.Start))
		}
		c.idleAt = idleAt
		c.parkingGraceEnd = new(big.Rat).Add(idleAt, secondsOffset(gracePeriodS(v, store.PriceComponentParkingTime)))
	}
	c.timeGraceEnd = secondsOffset(gracePeriodS(v, store.PriceComponentTime))
	return c
}

// gracePeriodS returns the grace period of a time dimension of the tariff in seconds:
// that of its first price component of the dimension, which every other component of the
// dimension of a valid tariff has too.
func gracePeriodS(v *store.TariffVersion, dimension store.PriceComponentType) int64 {
	for _, element := range v.Elements {
		for _, component := range element.PriceComponents {
			if component.Type != dimension {
				continue
			}
			if component.GracePeriodS != nil && *component.GracePeriodS > 0 {
				return int64(*component.GracePeriodS)
			}
			return 0
		}
	}
	return 0
}

var nanosPerSecond = big.NewRat(int64(time.Second), 1)

// offsetOf returns a duration as an offset in nanoseconds.
func offsetOf(d time.Duration) *big.Rat {
	return new(big.Rat).SetInt64(int64(d))
}

// secondsOffset returns a number of seconds as an offset in nanoseconds.
func secondsOffset(seconds int64) *big.Rat {
	return new(big.Rat).Mul(new(big.Rat).SetInt64(seconds), nanosPerSecond)
}

// secondsOf returns an offset or a time in nanoseconds as seconds.
func secondsOf(nanos *big.Rat) *big.Rat {
	return new(big.Rat).Quo(nanos, nanosPerSecond)
}

// floorInt returns the whole number at or below r, for r that is not negative.
func floorInt(r *big.Rat) int64 {
	return new(big.Int).Quo(r.Num(), r.Denom()).Int64()
}

// ceilInt returns the whole number at or above r, for r that is not negative.
func ceilInt(r *big.Rat) int64 {
	quotient, remainder := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	return quotient.Int64()
}

// roundToInt returns the whole number nearest to r, a half away from zero.
func roundToInt(r *big.Rat) int64 {
	return int64(money.RoundRat(r, 0, money.RoundHalfUp) / 10000)
}
