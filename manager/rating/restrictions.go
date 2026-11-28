// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// minutesPerDay is the end of a day as a minute of the day.
const minutesPerDay = 24 * 60

// weekdays maps the weekday of a time to the OCPI day.
var weekdays = map[time.Weekday]store.DayOfWeek{
	time.Monday:    store.Monday,
	time.Tuesday:   store.Tuesday,
	time.Wednesday: store.Wednesday,
	time.Thursday:  store.Thursday,
	time.Friday:    store.Friday,
	time.Saturday:  store.Saturday,
	time.Sunday:    store.Sunday,
}

// restrictionDateLayout is the layout of a date of the restrictions, e.g. 2026-12-31.
const restrictionDateLayout = "2006-01-02"

// restrictionsHold reports whether the restrictions of a tariff element hold at the start
// of the segment: every restriction that is set must hold. Times of day, dates and
// weekdays are those of the clock on the wall of the location of the session. A lower
// limit is part of what it allows, an upper limit is not.
//
// An element for a reservation never applies: a session is not a reservation. A
// restriction on the power does not hold for a segment without a duration, and one on
// the current not while the current was not measured. A restriction that cannot be read
// (a time that is not HH:MM, a date that is not a date, a limit that is not a number)
// does not hold.
func restrictionsHold(r *store.TariffRestrictions, seg *segment, c *calculation) bool {
	if r == nil {
		return true
	}
	if r.Reservation != nil {
		return false
	}

	local := seg.at.In(c.location)
	if (r.StartTime != nil || r.EndTime != nil) && !inTimeOfDay(local, r.StartTime, r.EndTime) {
		return false
	}
	if (r.StartDate != nil || r.EndDate != nil) && !inDates(local, r.StartDate, r.EndDate) {
		return false
	}
	if len(r.DayOfWeek) > 0 && !slices.Contains(r.DayOfWeek, weekdays[local.Weekday()]) {
		return false
	}

	if !within(seg.energyBefore, r.MinKwh, r.MaxKwh, 1000) {
		return false
	}
	if !within(seg.powerKw, r.MinPower, r.MaxPower, 1) {
		return false
	}
	if !within(seg.currentA, r.MinCurrent, r.MaxCurrent, 1) {
		return false
	}
	if r.MinDuration != nil && seg.start.Cmp(secondsOffset(int64(*r.MinDuration))) < 0 {
		return false
	}
	if r.MaxDuration != nil && seg.start.Cmp(secondsOffset(int64(*r.MaxDuration))) >= 0 {
		return false
	}
	return true
}

// minuteOfDay reads a time of day as "HH:MM" and returns it as the minute of the day.
func minuteOfDay(timeOfDay string) (minute int, ok bool) {
	if len(timeOfDay) != 5 || timeOfDay[2] != ':' {
		return 0, false
	}
	hours, err := strconv.Atoi(timeOfDay[:2])
	if err != nil || hours < 0 || hours > 23 {
		return 0, false
	}
	minutes, err := strconv.Atoi(timeOfDay[3:])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, false
	}
	return hours*60 + minutes, true
}

// inTimeOfDay reports whether the time of day of local is in the window from start
// (which is part of it) to end (which is not). A window whose end is before its start
// goes on past midnight, and an end of 00:00 is the end of the day. A window with only a
// start lasts to the end of the day, one with only an end starts with the day.
func inTimeOfDay(local time.Time, start, end *string) bool {
	from, to := 0, minutesPerDay
	if start != nil {
		minute, ok := minuteOfDay(*start)
		if !ok {
			return false
		}
		from = minute
	}
	if end != nil {
		minute, ok := minuteOfDay(*end)
		if !ok {
			return false
		}
		if minute > 0 {
			to = minute
		}
	}

	// the window opens and closes with a minute, so the minute of the moment decides
	now := local.Hour()*60 + local.Minute()
	if to < from {
		return now >= from || now < to
	}
	return now >= from && now < to
}

// inDates reports whether the date of local is from the start date (which is part of the
// range) to the end date (which is not).
func inDates(local time.Time, start, end *string) bool {
	date := local.Format(restrictionDateLayout)
	for _, limit := range []*string{start, end} {
		if limit == nil {
			continue
		}
		if _, err := time.Parse(restrictionDateLayout, *limit); err != nil {
			return false
		}
	}
	// dates in this layout are in the order of their text
	if start != nil && date < *start {
		return false
	}
	if end != nil && date >= *end {
		return false
	}
	return true
}

// limitRat returns a limit of a restriction as an exact number: the decimal number that
// the float was read from. ok is false without a limit and for one that is not a number.
func limitRat(limit *float64) (*big.Rat, bool) {
	if limit == nil || math.IsNaN(*limit) || math.IsInf(*limit, 0) {
		return nil, false
	}
	return new(big.Rat).SetString(strconv.FormatFloat(*limit, 'f', -1, 64))
}

// scaledLimit returns a limit of a restriction times scale: a limit in kWh as Wh.
func scaledLimit(limit *float64, scale int64) (*big.Rat, bool) {
	r, ok := limitRat(limit)
	if !ok {
		return nil, false
	}
	return r.Mul(r, new(big.Rat).SetInt64(scale)), true
}

// within reports whether value is from min (which is allowed) to max (which is not),
// where the limits are in a unit that is scale times that of value. Without limits
// everything is within. With one, a value that is not known (nil) is not within, and
// neither is anything when a limit is not a number.
func within(value *big.Rat, min, max *float64, scale int64) bool {
	if min == nil && max == nil {
		return true
	}
	if value == nil {
		return false
	}
	if min != nil {
		limit, ok := scaledLimit(min, scale)
		if !ok || value.Cmp(limit) < 0 {
			return false
		}
	}
	if max != nil {
		limit, ok := scaledLimit(max, scale)
		if !ok || value.Cmp(limit) >= 0 {
			return false
		}
	}
	return true
}
