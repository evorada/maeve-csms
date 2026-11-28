// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"math/big"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// TimeBreakdown is how a time dimension of a session was charged: the charging time
// (TIME) or the idle time (PARKING_TIME).
type TimeBreakdown struct {
	// TotalS is the time of the session in the state of the dimension, in seconds.
	TotalS int64 `json:"total_s"`
	// GraceS is the part of it in the grace period, which is never charged.
	GraceS int64 `json:"grace_s"`
	// BillableS is the part of it after the grace period for which a price applied,
	// before it was rounded up to steps. The rest of TotalS was free: no tariff element
	// applied.
	BillableS int64 `json:"billable_s"`
	// StepSizeS is the step of the price component that applied last, and Steps the
	// number of such steps that BillableS was rounded up to. Both are 0 when nothing was
	// charged.
	StepSizeS int32 `json:"step_size_s"`
	Steps     int64 `json:"steps"`
	// Minutes is TotalS in minutes.
	Minutes float64     `json:"minutes"`
	Cost    money.Price `json:"cost"`
}

// EnergyBreakdown is how the energy of a session was charged.
type EnergyBreakdown struct {
	// Wh is the energy of the session.
	Wh int64 `json:"wh"`
	// BilledWh is the energy that was charged: the energy for which a price applied,
	// rounded up to the step of the price component that applied last.
	BilledWh int64       `json:"billed_wh"`
	Cost     money.Price `json:"cost"`
}

// PriceBreakdown is the price of a session by its parts. Every part is rounded on its
// own; Total is their sum, or the minimum or the maximum price of the tariff when the sum
// is below or above it.
type PriceBreakdown struct {
	Flat    money.Price     `json:"flat"`
	Energy  EnergyBreakdown `json:"energy"`
	Time    TimeBreakdown   `json:"time"`    // charging time
	Parking TimeBreakdown   `json:"parking"` // idle time
	// Adjustment is what the minimum or the maximum price of the tariff added to the sum
	// of the parts (negative for the maximum price); nil when neither applied.
	Adjustment      *money.Price `json:"adjustment,omitempty"`
	MinPriceApplied bool         `json:"min_price_applied"`
	MaxPriceApplied bool         `json:"max_price_applied"`
	Total           money.Price  `json:"total"`
}

// PriceInfo returns the breakdown as the price info map that billing systems read: the
// start, energy, charging time and parking time price and the total price, all without
// VAT, and the minutes and the steps of the charging and the parking time.
func (b PriceBreakdown) PriceInfo() map[string]any {
	return map[string]any{
		"startPrice":                 b.Flat.ExclVat,
		"energyPrice":                b.Energy.Cost.ExclVat,
		"chargingTimePrice":          b.Time.Cost.ExclVat,
		"parkingTimePrice":           b.Parking.Cost.ExclVat,
		"totalChargingTimeInMinutes": b.Time.Minutes,
		"totalChargingTimeSteps":     b.Time.Steps,
		"totalParkingTimeInMinutes":  b.Parking.Minutes,
		"totalParkingTimeSteps":      b.Parking.Steps,
		"totalPrice":                 b.Total.ExclVat,
	}
}

var (
	ratOne     = big.NewRat(1, 1)
	ratHundred = big.NewRat(100, 1)
)

// unitPrice returns the price of a component per unit of what it is charged for: per Wh
// (ENERGY) or per second (TIME, PARKING_TIME). A time component with a price per step is
// charged by that price, not by the price per hour that was derived from it.
func unitPrice(component *store.PriceComponent) *big.Rat {
	switch component.Type {
	case store.PriceComponentEnergy:
		return new(big.Rat).Quo(component.Price.Rat(), big.NewRat(1000, 1))
	case store.PriceComponentTime, store.PriceComponentParkingTime:
		if component.PricePerStep != nil && component.StepSize >= 1 {
			return new(big.Rat).Quo(component.PricePerStep.Rat(), big.NewRat(int64(component.StepSize), 1))
		}
		return new(big.Rat).Quo(component.Price.Rat(), big.NewRat(3600, 1))
	}
	return new(big.Rat)
}

// withVat returns an amount with the VAT of the component: the amount itself when the
// component has none.
func withVat(excl *big.Rat, component *store.PriceComponent) *big.Rat {
	if component.Vat == nil {
		return new(big.Rat).Set(excl)
	}
	factor := new(big.Rat).Quo(component.Vat.Rat(), ratHundred)
	factor.Add(factor, ratOne)
	return factor.Mul(factor, excl)
}

// dimensionSum adds up what a dimension of a session costs.
type dimensionSum struct {
	// quantity is what was charged so far: Wh or seconds
	quantity *big.Rat
	// grace is the time in the grace period, in seconds
	grace      *big.Rat
	excl, incl *big.Rat
	// last is the component of the last segment that was charged
	last *store.PriceComponent
}

func newDimensionSum() *dimensionSum {
	return &dimensionSum{quantity: new(big.Rat), grace: new(big.Rat), excl: new(big.Rat), incl: new(big.Rat)}
}

// charge adds the quantity at the price of the component.
func (d *dimensionSum) charge(quantity *big.Rat, component *store.PriceComponent) {
	if quantity.Sign() <= 0 {
		return
	}
	excl := new(big.Rat).Mul(quantity, unitPrice(component))
	d.excl.Add(d.excl, excl)
	d.incl.Add(d.incl, withVat(excl, component))
	d.quantity.Add(d.quantity, quantity)
	d.last = component
}

// roundUp rounds the charged quantity up to a whole number of steps of the component that
// was charged last, charges the difference at the price of that component, and returns
// the step and the number of steps. Both are 0 when nothing was charged.
func (d *dimensionSum) roundUp() (stepSize int32, steps int64) {
	if d.last == nil {
		return 0, 0
	}
	stepSize = max(d.last.StepSize, 1)
	step := big.NewRat(int64(stepSize), 1)
	steps = ceilInt(new(big.Rat).Quo(d.quantity, step))
	rounded := new(big.Rat).Mul(new(big.Rat).SetInt64(steps), step)
	d.charge(new(big.Rat).Sub(rounded, d.quantity), d.last)
	d.quantity = rounded
	return stepSize, steps
}

// cost returns the sum without and with VAT, each rounded.
func (d *dimensionSum) cost(rounding Rounding) money.Price {
	return money.Price{
		ExclVat: money.RoundRat(d.excl, rounding.Scale, rounding.Mode),
		InclVat: money.RoundRat(d.incl, rounding.Scale, rounding.Mode),
	}
}

// price works out what the segments cost.
func (c *calculation) price(segments []*segment, rounding Rounding) PriceBreakdown {
	flat, energy, charging, parking := newDimensionSum(), newDimensionSum(), newDimensionSum(), newDimensionSum()

	flatCharged := false
	for _, seg := range segments {
		if component := seg.active.flat.component; component != nil && !flatCharged {
			// once, with the first segment in which a flat fee applies
			flat.excl.Set(component.Price.Rat())
			flat.incl.Set(withVat(flat.excl, component))
			flatCharged = true
		}
		if component := seg.active.energy.component; component != nil {
			energy.charge(seg.energy, component)
		}

		seconds := secondsOf(seg.duration)
		dimension, component, billable := charging, seg.active.time.component, seg.timeBillable
		if seg.idle {
			dimension, component, billable = parking, seg.active.parking.component, seg.parkingBillable
		}
		switch {
		case !billable:
			dimension.grace.Add(dimension.grace, seconds)
		case component != nil:
			dimension.charge(seconds, component)
		}
	}

	chargingS, parkingS := c.times()
	b := PriceBreakdown{
		Flat:    flat.cost(rounding),
		Energy:  EnergyBreakdown{Wh: roundToInt(c.curve.total())},
		Time:    timeBreakdown(charging, chargingS, rounding),
		Parking: timeBreakdown(parking, parkingS, rounding),
	}
	_, _ = energy.roundUp()
	b.Energy.BilledWh = roundToInt(energy.quantity)
	b.Energy.Cost = energy.cost(rounding)

	b.Total = money.Price{
		ExclVat: b.Flat.ExclVat + b.Energy.Cost.ExclVat + b.Time.Cost.ExclVat + b.Parking.Cost.ExclVat,
		InclVat: b.Flat.InclVat + b.Energy.Cost.InclVat + b.Time.Cost.InclVat + b.Parking.Cost.InclVat,
	}
	switch minPrice, maxPrice := c.version.MinPrice, c.version.MaxPrice; {
	case minPrice != nil && b.Total.ExclVat < minPrice.ExclVat:
		b.Adjustment = &money.Price{ExclVat: minPrice.ExclVat - b.Total.ExclVat, InclVat: minPrice.InclVat - b.Total.InclVat}
		b.MinPriceApplied = true
		b.Total = *minPrice
	case maxPrice != nil && b.Total.ExclVat > maxPrice.ExclVat:
		b.Adjustment = &money.Price{ExclVat: maxPrice.ExclVat - b.Total.ExclVat, InclVat: maxPrice.InclVat - b.Total.InclVat}
		b.MaxPriceApplied = true
		b.Total = *maxPrice
	}
	return b
}

// times returns how long the session charged and how long it was idle, in whole seconds:
// the idle time is the time from the start of the idle time to the end, the charging time
// the rest of the duration.
func (c *calculation) times() (chargingS, parkingS int64) {
	totalS := floorInt(secondsOf(c.duration))
	if c.idleAt != nil {
		parkingS = floorInt(secondsOf(new(big.Rat).Sub(c.duration, c.idleAt)))
	}
	return totalS - parkingS, parkingS
}

// timeBreakdown rounds the charged time of a dimension up to steps and returns how the
// dimension was charged. totalS is the time of the session in the state of the dimension.
func timeBreakdown(d *dimensionSum, totalS int64, rounding Rounding) TimeBreakdown {
	b := TimeBreakdown{
		TotalS:    totalS,
		GraceS:    floorInt(d.grace),
		BillableS: ceilInt(d.quantity),
		Minutes:   volume(big.NewRat(totalS, 60)),
	}
	b.StepSizeS, b.Steps = d.roundUp()
	b.Cost = d.cost(rounding)
	return b
}

// volume returns a quantity as a number with 4 decimals, the precision of an OCPI number.
func volume(quantity *big.Rat) float64 {
	return float64(money.RoundRat(quantity, 4, money.RoundHalfUp)) / 10000
}

// periodKey is what the segments of one charging period have in common.
type periodKey struct {
	idle                          bool
	energy, time, parking         componentRef
	timeBillable, parkingBillable bool
}

// chargingPeriods joins the segments that follow each other with the same state, the same
// price components and the same place in or after a grace period into charging periods.
// A period lists what happened in it: the energy in kWh, and its time in hours as TIME
// while the session charged and as PARKING_TIME while it was idle.
func chargingPeriods(segments []*segment, tariffId string) []ChargingPeriod {
	type openPeriod struct {
		key      periodKey
		period   ChargingPeriod
		energy   *big.Rat // Wh
		duration *big.Rat // nanoseconds
	}
	var periods []ChargingPeriod
	var open *openPeriod
	closeOpen := func() {
		if open == nil {
			return
		}
		open.period.Dimensions = []Dimension{}
		if open.energy.Sign() > 0 {
			kWh := new(big.Rat).Quo(open.energy, big.NewRat(1000, 1))
			open.period.Dimensions = append(open.period.Dimensions, Dimension{Type: DimensionEnergy, Volume: volume(kWh)})
		}
		if open.duration.Sign() > 0 {
			dimension := DimensionTime
			if open.key.idle {
				dimension = DimensionParkingTime
			}
			hours := new(big.Rat).Quo(secondsOf(open.duration), big.NewRat(3600, 1))
			open.period.Dimensions = append(open.period.Dimensions, Dimension{Type: dimension, Volume: volume(hours)})
		}
		periods = append(periods, open.period)
		open = nil
	}

	for _, seg := range segments {
		key := periodKey{
			idle:            seg.idle,
			energy:          seg.active.energy,
			time:            seg.active.time,
			parking:         seg.active.parking,
			timeBillable:    seg.timeBillable,
			parkingBillable: seg.parkingBillable,
		}
		if open != nil && open.key != key {
			closeOpen()
		}
		if open == nil {
			open = &openPeriod{
				key:      key,
				period:   ChargingPeriod{StartDateTime: seg.at, TariffId: tariffId},
				energy:   new(big.Rat),
				duration: new(big.Rat),
			}
		}
		open.energy.Add(open.energy, seg.energy)
		open.duration.Add(open.duration, seg.duration)
	}
	closeOpen()
	return periods
}
