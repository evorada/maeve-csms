// SPDX-License-Identifier: Apache-2.0

package rating

import (
	"math"
	"math/big"
	"sort"
	"strconv"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// curvePoint is the energy that a session has taken at a moment.
type curvePoint struct {
	at *big.Rat // offset from the start of the session in nanoseconds
	wh *big.Rat
}

// energyCurve is the energy of a session over its time: points in the order of time,
// from nothing at the start to the energy of the session at its end, between which the
// energy rises in a straight line. It never falls.
type energyCurve []curvePoint

// energyRat returns an energy in Wh as an exact number, to the thousandth of a Wh: what
// is beyond that is the noise of the conversion of a reading, not a measurement. ok is
// false for a value that is not a number.
func energyRat(wh float64) (*big.Rat, bool) {
	if math.IsNaN(wh) || math.IsInf(wh, 0) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(wh, 'f', 3, 64))
	return r, ok
}

// newEnergyCurve builds the curve of a session that ends at end, from its samples.
//
// The samples before or at the start and after the end are left out, and so is a sample
// that shows less energy than the one before it: a register only counts up. Of two
// samples of the same moment the higher one counts. An ended session with a known total
// ends with that total, and no sample counts for more; any other session ends with the
// energy of its newest sample.
func newEnergyCurve(s Session, end time.Time) energyCurve {
	duration := offsetOf(end.Sub(s.Start))

	var total *big.Rat
	if s.Stop != nil && s.TotalEnergyWh != nil {
		if wh, ok := energyRat(*s.TotalEnergyWh); ok && wh.Sign() >= 0 {
			total = wh
		}
	}

	samples := make([]Sample, len(s.Samples))
	copy(samples, s.Samples)
	sort.SliceStable(samples, func(i, j int) bool {
		if !samples[i].At.Equal(samples[j].At) {
			return samples[i].At.Before(samples[j].At)
		}
		return samples[i].EnergyWh < samples[j].EnergyWh
	})

	curve := energyCurve{{at: new(big.Rat), wh: new(big.Rat)}}
	for _, sample := range samples {
		if !sample.At.After(s.Start) || sample.At.After(end) {
			continue
		}
		wh, ok := energyRat(sample.EnergyWh)
		if !ok {
			continue
		}
		if total != nil && wh.Cmp(total) > 0 {
			wh = total
		}
		last := &curve[len(curve)-1]
		if wh.Cmp(last.wh) < 0 {
			continue
		}
		at := offsetOf(sample.At.Sub(s.Start))
		if at.Cmp(last.at) == 0 {
			last.wh = wh
			continue
		}
		curve = append(curve, curvePoint{at: at, wh: wh})
	}

	last := &curve[len(curve)-1]
	closing := last.wh
	if total != nil {
		closing = total
	}
	if last.at.Cmp(duration) == 0 {
		// also the only point of a session without a duration, whose energy is all taken
		// at that moment
		last.wh = closing
	} else {
		curve = append(curve, curvePoint{at: duration, wh: closing})
	}
	return curve
}

// total returns the energy of the session in Wh.
func (c energyCurve) total() *big.Rat {
	return c[len(c)-1].wh
}

// at returns the energy in Wh that the session has taken at the offset, which lies in
// the session.
func (c energyCurve) at(offset *big.Rat) *big.Rat {
	// the first point after the offset
	next := sort.Search(len(c), func(i int) bool { return c[i].at.Cmp(offset) > 0 })
	switch {
	case next == 0:
		return c[0].wh
	case next == len(c):
		return c.total()
	}
	from, to := c[next-1], c[next]
	if from.at.Cmp(offset) == 0 || from.wh.Cmp(to.wh) == 0 {
		return from.wh
	}
	// from.wh + (to.wh - from.wh) x (offset - from.at) / (to.at - from.at)
	share := new(big.Rat).Quo(new(big.Rat).Sub(offset, from.at), new(big.Rat).Sub(to.at, from.at))
	rise := new(big.Rat).Sub(to.wh, from.wh)
	return rise.Mul(rise, share).Add(rise, from.wh)
}

// reaches returns the first offset at which the session has taken wh; ok is false when it
// never takes that much, and for the energy that it has from its start.
func (c energyCurve) reaches(wh *big.Rat) (offset *big.Rat, ok bool) {
	for i := 1; i < len(c); i++ {
		from, to := c[i-1], c[i]
		if from.wh.Cmp(wh) >= 0 || to.wh.Cmp(wh) < 0 {
			continue
		}
		// from.at + (to.at - from.at) x (wh - from.wh) / (to.wh - from.wh)
		share := new(big.Rat).Quo(new(big.Rat).Sub(wh, from.wh), new(big.Rat).Sub(to.wh, from.wh))
		span := new(big.Rat).Sub(to.at, from.at)
		return span.Mul(span, share).Add(span, from.at), true
	}
	return nil, false
}

// segment is a stretch of a session in which nothing changes that a price depends on.
type segment struct {
	// start is the offset of its start, duration its length, both in nanoseconds. The
	// only segment of a session without a duration has none.
	start    *big.Rat
	duration *big.Rat
	// at is the start as a time, cut to the nanosecond
	at time.Time
	// energyBefore is the energy in Wh that the session had taken at the start of the
	// segment, energy what it takes in the segment
	energyBefore *big.Rat
	energy       *big.Rat
	// powerKw is the power of the segment; nil for a segment without a duration
	powerKw *big.Rat
	// currentA is the current of the newest sample at the start; nil when not measured
	currentA *big.Rat
	// idle says that the session no longer charges
	idle bool
	// timeBillable and parkingBillable say that the time of the segment counts for the
	// dimension: the session is in the state of the dimension and its grace period is
	// over
	timeBillable    bool
	parkingBillable bool
	// active are the price components that apply in the segment
	active activeComponents
}

// segments cuts the session into its segments and finds the price components of each.
func (c *calculation) segments() []*segment {
	bounds := c.boundaries()
	currents := c.currents()

	if len(bounds) == 1 {
		// a session without a duration: one segment, which takes its energy in no time
		bounds = append(bounds, bounds[0])
	}
	segments := make([]*segment, 0, len(bounds)-1)
	for i := 0; i+1 < len(bounds); i++ {
		start, end := bounds[i], bounds[i+1]
		energyBefore := c.curve.at(start)
		seg := &segment{
			start:        start,
			duration:     new(big.Rat).Sub(end, start),
			at:           c.session.Start.Add(time.Duration(floorInt(start))),
			energyBefore: energyBefore,
			energy:       new(big.Rat).Sub(c.curve.at(end), energyBefore),
		}
		if seg.duration.Sign() > 0 {
			// Wh per nanosecond to kW: x 3600 s x 1e9 ns / 1000
			seg.powerKw = new(big.Rat).Quo(seg.energy, seg.duration)
			seg.powerKw.Mul(seg.powerKw, big.NewRat(3600*int64(time.Second), 1000))
		} else {
			// all of the energy, from nothing
			seg.energyBefore, seg.energy = new(big.Rat), c.curve.total()
		}
		seg.currentA = currents.at(seg.at)
		seg.idle = c.idleAt != nil && start.Cmp(c.idleAt) >= 0
		seg.timeBillable = !seg.idle && start.Cmp(c.timeGraceEnd) >= 0
		seg.parkingBillable = seg.idle && start.Cmp(c.parkingGraceEnd) >= 0
		seg.active = c.activeComponents(seg)
		segments = append(segments, seg)
	}
	return segments
}

// boundaries returns the offsets at which the session is cut, in order and each once: its
// start, its end, and in between every moment at which a price can change.
func (c *calculation) boundaries() []*big.Rat {
	bounds := []*big.Rat{new(big.Rat), c.duration}
	add := func(offset *big.Rat) {
		if offset != nil && offset.Sign() > 0 && offset.Cmp(c.duration) < 0 {
			bounds = append(bounds, offset)
		}
	}
	addTime := func(at time.Time) {
		add(offsetOf(at.Sub(c.session.Start)))
	}

	for _, sample := range c.session.Samples {
		addTime(sample.At)
	}
	add(c.idleAt)
	add(c.timeGraceEnd)
	add(c.parkingGraceEnd)

	end := c.session.Start.Add(time.Duration(floorInt(c.duration)))
	wallClock := false
	timesOfDay := map[string]bool{"00:00": true}
	for _, element := range c.version.Elements {
		r := element.Restrictions
		if r == nil {
			continue
		}
		if r.StartTime != nil || r.EndTime != nil || r.StartDate != nil || r.EndDate != nil || len(r.DayOfWeek) > 0 {
			wallClock = true
		}
		for _, timeOfDay := range []*string{r.StartTime, r.EndTime} {
			if timeOfDay != nil {
				timesOfDay[*timeOfDay] = true
			}
		}
		for _, seconds := range []*int32{r.MinDuration, r.MaxDuration} {
			if seconds != nil {
				add(secondsOffset(int64(*seconds)))
			}
		}
		for _, kWh := range []*float64{r.MinKwh, r.MaxKwh} {
			if wh, ok := scaledLimit(kWh, 1000); ok {
				if offset, reached := c.curve.reaches(wh); reached {
					add(offset)
				}
			}
		}
	}
	if wallClock {
		for _, at := range wallClockChanges(c.session.Start, end, c.location, timesOfDay) {
			addTime(at)
		}
	}

	sort.Slice(bounds, func(i, j int) bool { return bounds[i].Cmp(bounds[j]) < 0 })
	unique := bounds[:1]
	for _, offset := range bounds[1:] {
		if offset.Cmp(unique[len(unique)-1]) != 0 {
			unique = append(unique, offset)
		}
	}
	return unique
}

// maxWallClockDays is the most days of a session for which the times of day of the
// restrictions are looked up one by one. A session that seems to last longer has a start
// or an end from a clock that was wrong.
const maxWallClockDays = 3660

// wallClockChanges returns the moments from start to end at which the clock on the wall
// of the location shows one of the times of day ("HH:MM"), and those at which that clock
// is set forward or back (daylight saving time). A time of day that the clock shows twice
// in a night is returned twice, one that it skips not at all.
func wallClockChanges(start, end time.Time, location *time.Location, timesOfDay map[string]bool) []time.Time {
	var changes []time.Time

	// the changes of the clock itself
	for at := start; ; {
		_, next := at.In(location).ZoneBounds()
		if next.IsZero() || !next.Before(end) {
			break
		}
		changes = append(changes, next)
		at = next
	}

	var minutes []int
	for timeOfDay := range timesOfDay {
		if minute, ok := minuteOfDay(timeOfDay); ok {
			minutes = append(minutes, minute)
		}
	}

	// every day that the session touches, with a day to spare on both sides
	first := start.In(location).AddDate(0, 0, -1)
	last := end.In(location).AddDate(0, 0, 1)
	day := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.UTC)
	lastDay := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	for days := 0; !day.After(lastDay) && days < maxWallClockDays; days, day = days+1, day.AddDate(0, 0, 1) {
		for _, minute := range minutes {
			// the time of day as if the location were at UTC, and then at each offset from
			// UTC that the location has around that day
			wall := day.Add(time.Duration(minute) * time.Minute)
			seen := map[int]bool{}
			for _, probe := range []time.Time{wall.Add(-36 * time.Hour), wall, wall.Add(36 * time.Hour)} {
				_, offset := probe.In(location).Zone()
				if seen[offset] {
					continue
				}
				seen[offset] = true
				at := wall.Add(-time.Duration(offset) * time.Second)
				// the clock of the location shows that time of that day then, or the offset
				// is not the one of that moment
				local := at.In(location)
				shown := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), local.Second(), 0, time.UTC)
				if shown.Equal(wall) && !at.Before(start) && !at.After(end) {
					changes = append(changes, at)
				}
			}
		}
	}
	return changes
}

// currentReadings are the samples of a session in the order of time, for looking up the
// current at a moment.
type currentReadings []Sample

// currents returns the samples of the session that lie in it, in the order of time.
func (c *calculation) currents() currentReadings {
	end := c.session.Start.Add(time.Duration(floorInt(c.duration)))
	var readings currentReadings
	for _, sample := range c.session.Samples {
		if sample.At.Before(c.session.Start) || sample.At.After(end) {
			continue
		}
		readings = append(readings, sample)
	}
	sort.SliceStable(readings, func(i, j int) bool { return readings[i].At.Before(readings[j].At) })
	return readings
}

// at returns the current in A of the newest sample at or before the moment; nil when
// there is no such sample or when it has no current.
func (r currentReadings) at(moment time.Time) *big.Rat {
	// the first sample after the moment
	next := sort.Search(len(r), func(i int) bool { return r[i].At.After(moment) })
	if next == 0 {
		return nil
	}
	current, ok := limitRat(r[next-1].CurrentA)
	if !ok {
		return nil
	}
	return current
}

// activeComponents are the price components that apply in a segment: for each dimension
// the component of the first tariff element whose restrictions hold and which has one.
type activeComponents struct {
	flat, energy, time, parking componentRef
}

// componentRef names a price component of a tariff version by the place of its element
// and its own place in it; component is nil when there is none.
type componentRef struct {
	element, index int
	component      *store.PriceComponent
}

func (c *calculation) activeComponents(seg *segment) activeComponents {
	var active activeComponents
	for e := range c.version.Elements {
		element := &c.version.Elements[e]
		if !restrictionsHold(element.Restrictions, seg, c) {
			continue
		}
		for i := range element.PriceComponents {
			component := &element.PriceComponents[i]
			var slot *componentRef
			switch component.Type {
			case store.PriceComponentFlat:
				slot = &active.flat
			case store.PriceComponentEnergy:
				slot = &active.energy
			case store.PriceComponentTime:
				slot = &active.time
			case store.PriceComponentParkingTime:
				slot = &active.parking
			}
			if slot != nil && slot.component == nil {
				*slot = componentRef{element: e, index: i, component: component}
			}
		}
	}
	return active
}
