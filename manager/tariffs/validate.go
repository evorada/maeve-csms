// SPDX-License-Identifier: Apache-2.0

package tariffs

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ValidationError is a field of a tariff version that breaks a rule. Field is the name of
// the field in the JSON of a tariff, e.g. "currency" or
// "elements[0].price_components[1].step_size".
type ValidationError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

var (
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	hourMinute      = regexp.MustCompile(`^([0-1][0-9]|2[0-3]):[0-5][0-9]$`)
)

// dateLayout is the layout of a date of the restrictions, e.g. 2026-12-31.
const dateLayout = "2006-01-02"

// OCPI 2.2.1 enums.
var (
	tariffTypes = map[store.TariffType]bool{
		store.TariffTypeAdHocPayment: true,
		store.TariffTypeProfileCheap: true,
		store.TariffTypeProfileFast:  true,
		store.TariffTypeProfileGreen: true,
		store.TariffTypeRegular:      true,
	}
	componentTypes = map[store.PriceComponentType]bool{
		store.PriceComponentEnergy:      true,
		store.PriceComponentFlat:        true,
		store.PriceComponentParkingTime: true,
		store.PriceComponentTime:        true,
	}
	daysOfWeek = map[store.DayOfWeek]bool{
		store.Monday:    true,
		store.Tuesday:   true,
		store.Wednesday: true,
		store.Thursday:  true,
		store.Friday:    true,
		store.Saturday:  true,
		store.Sunday:    true,
	}
	reservationRestrictions = map[store.ReservationRestriction]bool{
		store.ReservationRestrictionReservation:        true,
		store.ReservationRestrictionReservationExpires: true,
	}
)

// maxVat is the highest VAT percentage: 100%.
const maxVat = money.Percent(10000)

// isTimed reports whether the component is charged by time: the grace period and the
// price per step are for these components only.
func isTimed(t store.PriceComponentType) bool {
	return t == store.PriceComponentTime || t == store.PriceComponentParkingTime
}

// pricePerHour returns the price per hour of a price per step of stepSize seconds,
// rounded half up to 4 decimals: 0.50 per 900 s is 2.0000 per hour.
func pricePerHour(pricePerStep money.Amount, stepSize int32) money.Amount {
	perHour := new(big.Rat).Mul(pricePerStep.Rat(), big.NewRat(3600, int64(stepSize)))
	return money.RoundRat(perHour, 4, money.RoundHalfUp)
}

// derivedPriceMatches reports whether the price is the price per hour that follows from
// the price per step, give or take 0.0001.
func derivedPriceMatches(price, derived money.Amount) bool {
	difference := price - derived
	return difference >= -1 && difference <= 1
}

// Normalize brings a tariff version into the form in which it is stored, in place:
//   - a step size of 0 of an ENERGY, TIME or PARKING_TIME component becomes 1;
//   - the price of a TIME or PARKING_TIME component with a price per step becomes the
//     price per hour that follows from it (price per step x 3600 / step size, rounded half
//     up to 4 decimals), when the price is not set or is that price give or take 0.0001.
//
// It changes nothing else, and nothing in a version that it has normalized before.
func Normalize(v *store.TariffVersion) {
	for i := range v.Elements {
		components := v.Elements[i].PriceComponents
		for j := range components {
			c := &components[j]
			if c.StepSize == 0 && c.Type != store.PriceComponentFlat && componentTypes[c.Type] {
				c.StepSize = 1
			}
			if c.PricePerStep == nil || !isTimed(c.Type) || c.StepSize < 1 || *c.PricePerStep < 0 {
				continue
			}
			derived := pricePerHour(*c.PricePerStep, c.StepSize)
			if c.Price == 0 || derivedPriceMatches(c.Price, derived) {
				c.Price = derived
			}
		}
	}
}

// Validate normalizes the tariff version (see Normalize) and checks it against the OCPI
// 2.2.1 rules and the rules of the extensions. It returns the fields that break a rule, in
// the order of the fields; none when the version can be stored.
func Validate(v *store.TariffVersion) []ValidationError {
	Normalize(v)
	c := &checker{}

	if !currencyPattern.MatchString(v.Currency) {
		c.add("currency", "must be an ISO 4217 currency code of 3 capital letters")
	}
	if v.Type != nil && !tariffTypes[*v.Type] {
		c.add("type", "%q is not a known value", *v.Type)
	}
	c.price("min_price", v.MinPrice)
	c.price("max_price", v.MaxPrice)
	if v.MinPrice != nil && v.MaxPrice != nil && v.MinPrice.ExclVat > v.MaxPrice.ExclVat {
		c.add("max_price.excl_vat", "must not be less than min_price.excl_vat")
	}
	if v.StartDateTime != nil && v.EndDateTime != nil && !v.StartDateTime.Before(*v.EndDateTime) {
		c.add("end_date_time", "must be after start_date_time")
	}

	if len(v.Elements) == 0 {
		c.add("elements", "must have at least one element")
	}
	// the grace period of a time dimension is one for the whole tariff: the first
	// component of the dimension says which
	grace := map[store.PriceComponentType]int32{}
	for i, element := range v.Elements {
		prefix := fmt.Sprintf("elements[%d].", i)
		if len(element.PriceComponents) == 0 {
			c.add(prefix+"price_components", "must have at least one price component")
		}
		seen := map[store.PriceComponentType]bool{}
		for j, component := range element.PriceComponents {
			c.component(fmt.Sprintf("%sprice_components[%d].", prefix, j), component, seen, grace)
		}
		if element.Restrictions != nil {
			c.restrictions(prefix+"restrictions.", element.Restrictions)
		}
	}

	if idle := v.IdleDetection; idle != nil {
		if share := idle.ChargingPowerShare; share != nil && (math.IsNaN(*share) || *share <= 0 || *share > 1) {
			c.add("idle_detection.charging_power_share", "must be above 0 and at most 1")
		}
		if power := idle.MinChargingPowerW; power != nil && (math.IsNaN(*power) || *power < 0) {
			c.add("idle_detection.min_charging_power_w", "must not be negative")
		}
	}
	return c.errors
}

// checker collects the rules that the fields of a tariff version break.
type checker struct {
	errors []ValidationError
}

func (c *checker) add(field, reason string, args ...any) {
	c.errors = append(c.errors, ValidationError{Field: field, Reason: fmt.Sprintf(reason, args...)})
}

// price checks a price limit of the tariff. Both amounts of a price are always there: a
// price that is read from JSON without one of them has 0 for it.
func (c *checker) price(field string, price *money.Price) {
	if price == nil {
		return
	}
	if price.ExclVat < 0 {
		c.add(field+".excl_vat", "must not be negative")
	}
	if price.InclVat < 0 {
		c.add(field+".incl_vat", "must not be negative")
	}
}

// component checks a price component. seen holds the types of the components of the
// element before it, grace the grace period of each time dimension of the tariff so far.
func (c *checker) component(prefix string, component store.PriceComponent, seen map[store.PriceComponentType]bool, grace map[store.PriceComponentType]int32) {
	known := componentTypes[component.Type]
	switch {
	case !known:
		c.add(prefix+"type", "%q is not a known value", component.Type)
	case seen[component.Type]:
		c.add(prefix+"type", "the element already has a %s price component", component.Type)
	}
	seen[component.Type] = true

	if component.Price < 0 {
		c.add(prefix+"price", "must not be negative")
	}
	if vat := component.Vat; vat != nil && (*vat < 0 || *vat > maxVat) {
		c.add(prefix+"vat", "must be from 0 to 100")
	}
	// the step size of a FLAT component has no meaning
	if known && component.Type != store.PriceComponentFlat && component.StepSize < 1 {
		c.add(prefix+"step_size", "must be at least 1")
	}

	timed := isTimed(component.Type)
	componentGrace := int32(0)
	if component.GracePeriodS != nil {
		switch {
		case !timed:
			c.add(prefix+"grace_period_s", "is only for TIME and PARKING_TIME price components")
		case *component.GracePeriodS < 0:
			c.add(prefix+"grace_period_s", "must not be negative")
		default:
			componentGrace = *component.GracePeriodS
		}
	}
	if timed {
		if tariffGrace, found := grace[component.Type]; !found {
			grace[component.Type] = componentGrace
		} else if tariffGrace != componentGrace {
			c.add(prefix+"grace_period_s", "must be the same for every %s price component of the tariff (%d)", component.Type, tariffGrace)
		}
	}

	if component.PricePerStep != nil {
		switch {
		case !timed:
			c.add(prefix+"price_per_step", "is only for TIME and PARKING_TIME price components")
		case *component.PricePerStep < 0:
			c.add(prefix+"price_per_step", "must not be negative")
		case component.StepSize >= 1:
			if derived := pricePerHour(*component.PricePerStep, component.StepSize); !derivedPriceMatches(component.Price, derived) {
				c.add(prefix+"price", "must be the price per hour that follows from price_per_step (%s), or left out", derived)
			}
		}
	}
}

func (c *checker) restrictions(prefix string, r *store.TariffRestrictions) {
	for _, hm := range []struct {
		field string
		value *string
	}{{"start_time", r.StartTime}, {"end_time", r.EndTime}} {
		if hm.value != nil && !hourMinute.MatchString(*hm.value) {
			c.add(prefix+hm.field, "must be a time of day as HH:MM, from 00:00 to 23:59")
		}
	}

	startDate, startOk := c.date(prefix+"start_date", r.StartDate)
	endDate, endOk := c.date(prefix+"end_date", r.EndDate)
	if startOk && endOk && endDate.Before(startDate) {
		c.add(prefix+"end_date", "must not be before start_date")
	}

	for _, limits := range []struct {
		name     string
		min, max *float64
	}{{"kwh", r.MinKwh, r.MaxKwh}, {"current", r.MinCurrent, r.MaxCurrent}, {"power", r.MinPower, r.MaxPower}} {
		if limits.min != nil && limits.max != nil && *limits.min > *limits.max {
			c.add(prefix+"max_"+limits.name, "must not be less than min_%s", limits.name)
		}
	}
	if r.MinDuration != nil && r.MaxDuration != nil && *r.MinDuration > *r.MaxDuration {
		c.add(prefix+"max_duration", "must not be less than min_duration")
	}

	days := map[store.DayOfWeek]bool{}
	for i, day := range r.DayOfWeek {
		field := fmt.Sprintf("%sday_of_week[%d]", prefix, i)
		switch {
		case !daysOfWeek[day]:
			c.add(field, "%q is not a known value", day)
		case days[day]:
			c.add(field, "%s is listed twice", day)
		}
		days[day] = true
	}
	if r.Reservation != nil && !reservationRestrictions[*r.Reservation] {
		c.add(prefix+"reservation", "%q is not a known value", *r.Reservation)
	}
}

// date checks a date of the restrictions and returns it; false when there is none or it
// is not a date.
func (c *checker) date(field string, value *string) (time.Time, bool) {
	if value == nil {
		return time.Time{}, false
	}
	date, err := time.Parse(dateLayout, *value)
	if err != nil {
		c.add(field, "must be a date as YYYY-MM-DD")
		return time.Time{}, false
	}
	return date, true
}
