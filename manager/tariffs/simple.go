// SPDX-License-Identifier: Apache-2.0

package tariffs

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SimpleRestriction is a window of one weekday in which parking is charged. An entry
// without a day or without one of its times is left out of the conversion.
type SimpleRestriction struct {
	DayOfWeek *int   `json:"dayOfWeek,omitempty"` // 0 = Sunday ... 6 = Saturday
	StartTime string `json:"startTime,omitempty"` // "HH:MM", local time of the location
	EndTime   string `json:"endTime,omitempty"`
}

// SimpleTariff is the simple tariff form: a start fee, a price per kWh, a time and a
// parking price per step with a grace period, and parking windows by weekday and time of
// day. Prices are without VAT; the time and the parking price are per step, not per hour.
// The JSON names are the field names of the form.
type SimpleTariff struct {
	Currency   string `json:"currency"`
	TariffType string `json:"tariff_type,omitempty"` // REGULAR | AD_HOC_PAYMENT
	// StartTariff is the fee per session, Price the price per kWh.
	StartTariff *float64 `json:"start_tariff,omitempty"`
	Price       *float64 `json:"price,omitempty"`
	// Vat is the VAT percentage (21 for 21%); it applies only when IsVatRelevant is true.
	Vat           *float64 `json:"vat,omitempty"`
	IsVatRelevant *bool    `json:"is_vat_relevant,omitempty"`
	// TimeTariff is the price of one step of charging time.
	TimeTariff             *float64 `json:"time_tariff,omitempty"`
	TimeStepSizeMinutes    *int     `json:"time_step_size_in_minutes,omitempty"`
	TimeGracePeriodMinutes *int     `json:"time_grace_period_in_minutes,omitempty"`
	// ParkingTariff is the price of one step of parking time (connected, not charging).
	ParkingTariff             *float64 `json:"parking_tariff,omitempty"`
	ParkingStepSizeMinutes    *int     `json:"parking_step_size_in_minutes,omitempty"`
	ParkingGracePeriodMinutes *int     `json:"parking_grace_period_in_minutes,omitempty"`
	// ParkingTariffRestrictions are the windows in which parking is charged; without
	// them it is charged at any time.
	ParkingTariffRestrictions []SimpleRestriction `json:"parking_tariff_restrictions,omitempty"`
}

// defaultStepSizeS is the step of a time or parking price that does not say its step
// size: one minute.
const defaultStepSizeS = 60

// simpleDays maps the weekday of a SimpleRestriction (0 = Sunday) to the OCPI day.
var simpleDays = [...]store.DayOfWeek{
	store.Sunday, store.Monday, store.Tuesday, store.Wednesday, store.Thursday, store.Friday, store.Saturday,
}

// FromSimple converts a tariff in the simple form into a tariff version:
//
//   - the type is AD_HOC_PAYMENT when the form says so, else REGULAR;
//   - every price component gets the VAT of the form when the form says that VAT is
//     relevant, else none;
//   - the first element has no restrictions and holds the start fee (FLAT, when above 0),
//     the price per kWh (ENERGY, when set, also when 0) and the price of charging time
//     (TIME, when above 0);
//   - the price of parking time (PARKING_TIME, when above 0) is in the first element when
//     the form has no parking windows; with windows there is one more element per time
//     of day (start and end), in the order in which the form names them, restricted to
//     that time of day and to the weekdays that the form gives it;
//   - a time or parking price is a price per step with the step size of the form (one
//     minute when the form has none) and its grace period.
//
// A float of the form becomes an amount by its shortest decimal text, rounded half up to
// 4 decimals. The version is normalized and valid; a form that does not give a valid
// tariff version (no price at all, a negative price) is an error.
func FromSimple(form SimpleTariff) (*store.TariffVersion, error) {
	tariffType := store.TariffTypeRegular
	if form.TariffType == string(store.TariffTypeAdHocPayment) {
		tariffType = store.TariffTypeAdHocPayment
	}
	v := &store.TariffVersion{Currency: form.Currency, Type: &tariffType}

	var vat *money.Percent
	if form.IsVatRelevant != nil && *form.IsVatRelevant && form.Vat != nil {
		percent, err := percentFromFloat(*form.Vat)
		if err != nil {
			return nil, fmt.Errorf("vat: %w", err)
		}
		vat = &percent
	}
	// every component gets a VAT percentage of its own
	componentVat := func() *money.Percent {
		if vat == nil {
			return nil
		}
		percent := *vat
		return &percent
	}

	var always []store.PriceComponent
	if form.StartTariff != nil && *form.StartTariff > 0 {
		price, err := amountFromFloat(*form.StartTariff)
		if err != nil {
			return nil, fmt.Errorf("start_tariff: %w", err)
		}
		always = append(always, store.PriceComponent{Type: store.PriceComponentFlat, Price: price, Vat: componentVat(), StepSize: 1})
	}
	if form.Price != nil {
		price, err := amountFromFloat(*form.Price)
		if err != nil {
			return nil, fmt.Errorf("price: %w", err)
		}
		always = append(always, store.PriceComponent{Type: store.PriceComponentEnergy, Price: price, Vat: componentVat(), StepSize: 1})
	}
	if form.TimeTariff != nil && *form.TimeTariff > 0 {
		component, err := steppedComponent(store.PriceComponentTime, *form.TimeTariff, form.TimeStepSizeMinutes, form.TimeGracePeriodMinutes)
		if err != nil {
			return nil, fmt.Errorf("time_tariff: %w", err)
		}
		component.Vat = componentVat()
		always = append(always, component)
	}

	var windows []store.TariffElement
	if form.ParkingTariff != nil && *form.ParkingTariff > 0 {
		parking, err := steppedComponent(store.PriceComponentParkingTime, *form.ParkingTariff, form.ParkingStepSizeMinutes, form.ParkingGracePeriodMinutes)
		if err != nil {
			return nil, fmt.Errorf("parking_tariff: %w", err)
		}
		parking.Vat = componentVat()
		restrictions := parkingWindows(form.ParkingTariffRestrictions)
		if len(restrictions) == 0 {
			always = append(always, parking)
		}
		for _, restriction := range restrictions {
			component := parking
			component.Vat = componentVat()
			component.GracePeriodS = clonePtr(parking.GracePeriodS)
			component.PricePerStep = clonePtr(parking.PricePerStep)
			windows = append(windows, store.TariffElement{
				PriceComponents: []store.PriceComponent{component},
				Restrictions:    restriction,
			})
		}
	}

	if len(always) > 0 {
		v.Elements = append(v.Elements, store.TariffElement{PriceComponents: always})
	}
	v.Elements = append(v.Elements, windows...)

	if invalid := Validate(v); len(invalid) > 0 {
		reasons := make([]string, len(invalid))
		for i, e := range invalid {
			reasons[i] = e.Field + ": " + e.Reason
		}
		return nil, errors.New("the simple tariff does not give a valid tariff version: " + strings.Join(reasons, "; "))
	}
	return v, nil
}

// steppedComponent returns a TIME or PARKING_TIME component with a price per step. The
// step size and the grace period are in minutes; a step size of nil or 0 is one minute,
// a grace period that is not above 0 is none.
func steppedComponent(componentType store.PriceComponentType, pricePerStep float64, stepSizeMinutes, gracePeriodMinutes *int) (store.PriceComponent, error) {
	price, err := amountFromFloat(pricePerStep)
	if err != nil {
		return store.PriceComponent{}, err
	}
	component := store.PriceComponent{Type: componentType, StepSize: defaultStepSizeS, PricePerStep: &price}
	if stepSizeMinutes != nil && *stepSizeMinutes != 0 {
		if component.StepSize, err = secondsFromMinutes(*stepSizeMinutes); err != nil {
			return store.PriceComponent{}, fmt.Errorf("step size: %w", err)
		}
	}
	if gracePeriodMinutes != nil && *gracePeriodMinutes > 0 {
		grace, err := secondsFromMinutes(*gracePeriodMinutes)
		if err != nil {
			return store.PriceComponent{}, fmt.Errorf("grace period: %w", err)
		}
		component.GracePeriodS = &grace
	}
	return component, nil
}

// parkingWindows groups the parking windows of the form by their time of day, in the
// order in which the form names the times: one restriction per start and end time, with
// the weekdays of its entries, each once. An entry without a weekday from 0 to 6 or
// without one of its times is left out.
func parkingWindows(entries []SimpleRestriction) []*store.TariffRestrictions {
	var windows []*store.TariffRestrictions
	for _, entry := range entries {
		if entry.DayOfWeek == nil || *entry.DayOfWeek < 0 || *entry.DayOfWeek >= len(simpleDays) || entry.StartTime == "" || entry.EndTime == "" {
			continue
		}
		day := simpleDays[*entry.DayOfWeek]
		var window *store.TariffRestrictions
		for _, w := range windows {
			if *w.StartTime == entry.StartTime && *w.EndTime == entry.EndTime {
				window = w
				break
			}
		}
		if window == nil {
			start, end := entry.StartTime, entry.EndTime
			window = &store.TariffRestrictions{StartTime: &start, EndTime: &end}
			windows = append(windows, window)
		}
		listed := false
		for _, d := range window.DayOfWeek {
			listed = listed || d == day
		}
		if !listed {
			window.DayOfWeek = append(window.DayOfWeek, day)
		}
	}
	return windows
}

// secondsFromMinutes converts minutes to seconds that fit an int32.
func secondsFromMinutes(minutes int) (int32, error) {
	if minutes > math.MaxInt32/60 || minutes < math.MinInt32/60 {
		return 0, fmt.Errorf("%d minutes is out of range", minutes)
	}
	return int32(minutes * 60), nil
}

// ratFromFloat returns the float as the exact value of its shortest decimal text, so
// that 0.1 is one tenth and not the binary fraction next to it.
func ratFromFloat(f float64) (*big.Rat, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("%v is not a number", f)
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(f, 'f', -1, 64))
	if !ok {
		return nil, fmt.Errorf("%v is not a number", f)
	}
	return r, nil
}

// amountFromFloat converts a float to an amount, rounded half up to 4 decimals.
func amountFromFloat(f float64) (money.Amount, error) {
	r, err := ratFromFloat(f)
	if err != nil {
		return 0, err
	}
	return money.RoundRat(r, 4, money.RoundHalfUp), nil
}

// percentFromFloat converts a float to a percentage, rounded half up to 2 decimals.
func percentFromFloat(f float64) (money.Percent, error) {
	r, err := ratFromFloat(f)
	if err != nil {
		return 0, err
	}
	// an amount counts 1/10000 and a percentage 1/100: at 2 decimals the last two digits
	// of the amount are 0
	return money.Percent(money.RoundRat(r, 2, money.RoundHalfUp) / 100), nil
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
