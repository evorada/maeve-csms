// SPDX-License-Identifier: Apache-2.0

package rating_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The sessions of these tests start at sessionStart, at a connector of 22 kW whose
// register reads registerBaseWh at that moment. A reading is given as the minute of the
// session and the energy of the session so far in kWh.
var sessionStart = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

const (
	registerBaseWh = 100_000.0
	maxPowerW      = 22_000.0
)

func minute(m float64) time.Time {
	return sessionStart.Add(time.Duration(m * float64(time.Minute)))
}

func reading(m, kWh float64) rating.EnergyPoint {
	return rating.EnergyPoint{At: minute(m), RegisterWh: registerBaseWh + kWh*1000}
}

func readings(minutesAndKWh ...float64) []rating.EnergyPoint {
	points := make([]rating.EnergyPoint, 0, len(minutesAndKWh)/2)
	for i := 0; i+1 < len(minutesAndKWh); i += 2 {
		points = append(points, reading(minutesAndKWh[i], minutesAndKWh[i+1]))
	}
	return points
}

// readingsWh are readings given as the minute of the session and the energy of the
// session so far in Wh: for the tests at the edge of a threshold, where a rounded number
// of kWh would decide the outcome.
func readingsWh(minutesAndWh ...float64) []rating.EnergyPoint {
	points := make([]rating.EnergyPoint, 0, len(minutesAndWh)/2)
	for i := 0; i+1 < len(minutesAndWh); i += 2 {
		points = append(points, rating.EnergyPoint{At: minute(minutesAndWh[i]), RegisterWh: registerBaseWh + minutesAndWh[i+1]})
	}
	return points
}

func ptr[T any](v T) *T {
	return &v
}

// idleAt is an IdleResult: idle since the minute of the session, for so many minutes.
func idleAt(sinceMinute, forMinutes float64) rating.IdleResult {
	return rating.IdleResult{IdleStartedAt: ptr(minute(sinceMinute)), Idle: time.Duration(forMinutes * float64(time.Minute))}
}

// detect runs DetectIdle with the default settings for a session that stopped at the
// minute stopMinute.
func detect(points []rating.EnergyPoint, stopMinute float64) rating.IdleResult {
	return rating.DetectIdle(points, sessionStart, ptr(minute(stopMinute)), minute(stopMinute+600), nil, maxPowerW, rating.DefaultIdleSettings())
}

func TestDetectIdleOfAnEndedSession(t *testing.T) {
	// a charger of 3.6 kW whose register moves in steps of 100 Wh, read every minute
	var slowSteps []rating.EnergyPoint
	for m := 0; m <= 60; m++ {
		slowSteps = append(slowSteps, reading(float64(m), 0.1*math.Floor(3*float64(m)/5)))
	}
	// an hour of charging, then a parked car under which the register moves by 100 Wh
	// every 40 minutes, read every minute
	parkedSteps := readings(0, 0, 30, 5.5, 60, 11)
	for m := 61; m <= 180; m++ {
		parkedSteps = append(parkedSteps, reading(float64(m), 11+0.1*math.Floor(float64(m-60)/40)))
	}

	for name, tc := range map[string]struct {
		points []rating.EnergyPoint
		stop   float64
		want   rating.IdleResult
	}{
		"idle starts when charging ends": {
			points: readings(0, 0, 30, 5.5, 60, 11, 120, 11, 150, 11), stop: 150, want: idleAt(60, 90),
		},
		"charged until the stop": {
			points: readings(0, 0, 30, 5.5, 60, 11), stop: 60, want: idleAt(60, 0),
		},
		"a slow charger is not idle": {
			points: readings(0, 0, 60, 3.7, 120, 7.4), stop: 120, want: idleAt(120, 0),
		},
		"a start delay and a pause in the middle are not idle time": {
			points: readings(0, 0, 5, 0, 35, 5, 65, 5, 95, 10, 125, 10), stop: 125, want: idleAt(95, 30),
		},
		"a gap without readings before the stop counts as idle": {
			points: readings(0, 0, 60, 11, 200, 11), stop: 200, want: idleAt(60, 140),
		},
		"a gap with energy is charging": {
			points: readings(0, 0, 10, 2, 70, 10), stop: 70, want: idleAt(70, 0),
		},
		"a draw below the threshold is idle": {
			// 150 W to keep the battery warm, at a connector whose threshold is 220 W
			points: readings(0, 0, 60, 11, 120, 11.15), stop: 120, want: idleAt(60, 60),
		},
		"register steps of 100 Wh under a slow charger are charging": {
			points: slowSteps, stop: 60, want: idleAt(60, 0),
		},
		"register steps under a parked car are idle": {
			points: parkedSteps, stop: 180, want: idleAt(60, 120),
		},
		"a session that never charged is idle from its start": {
			points: readings(0, 0, 62, 0), stop: 62, want: idleAt(0, 62),
		},
		"a single reading says nothing about charging": {
			points: readings(0, 0), stop: 45, want: idleAt(0, 45),
		},
		"readings in any order": {
			points: readings(120, 11, 0, 0, 60, 11, 30, 5.5), stop: 120, want: idleAt(60, 60),
		},
		"a reading of a clock that was wrong is ignored": {
			points: append([]rating.EnergyPoint{{At: time.Unix(0, 0).UTC(), RegisterWh: registerBaseWh}}, readings(0, 0, 60, 11, 90, 11)...),
			stop:   90, want: idleAt(60, 30),
		},
		"a reading long after the stop is ignored": {
			points: readings(0, 0, 60, 11, 90, 11, 400, 30), stop: 90, want: idleAt(60, 30),
		},
		"the idle time is never longer than the session": {
			// the reading of the minute 60 is within the tolerance after the stop
			points: readings(0, 0, 60, 11), stop: 30, want: idleAt(30, 0),
		},
		"charging before the start does not move the start of the idle time": {
			points: readings(-30, -5, -10, 0, 20, 0), stop: 20, want: idleAt(0, 20),
		},
		"readings that are not numbers are ignored": {
			points: append(readings(0, 0, 60, 11, 90, 11),
				rating.EnergyPoint{At: minute(30), RegisterWh: math.NaN()}, rating.EnergyPoint{At: minute(75), RegisterWh: math.Inf(1)}),
			stop: 90, want: idleAt(60, 30),
		},
		"no reading at all": {
			points: nil, stop: 90, want: rating.IdleResult{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, detect(tc.points, tc.stop))
		})
	}
}

// The reading of the stop closes the readings of a session: the meter stop of the
// transaction, which is known also when the last meter value came long before it.
func TestDetectIdleWithTheMeterStop(t *testing.T) {
	settings := rating.DefaultIdleSettings()
	stop := minute(180)

	t.Run("nothing was drawn after the last reading", func(t *testing.T) {
		got := rating.DetectIdle(readings(0, 0, 60, 11), sessionStart, &stop, stop, ptr(registerBaseWh+11_000), maxPowerW, settings)
		assert.Equal(t, idleAt(60, 120), got)
	})

	t.Run("energy between the last reading and the stop is charging", func(t *testing.T) {
		got := rating.DetectIdle(readings(0, 0, 60, 11), sessionStart, &stop, stop, ptr(registerBaseWh+33_000), maxPowerW, settings)
		assert.Equal(t, idleAt(180, 0), got)
	})

	t.Run("the meter start and the meter stop alone", func(t *testing.T) {
		got := rating.DetectIdle(readings(0, 0), sessionStart, &stop, stop, ptr(registerBaseWh+11_000), maxPowerW, settings)
		assert.Equal(t, idleAt(180, 0), got, "3.7 kW on average: charging")
	})

	t.Run("a meter stop without a stop is not used", func(t *testing.T) {
		got := rating.DetectIdle(readings(0, 0, 30, 5.5, 40, 5.5), sessionStart, nil, minute(55), ptr(registerBaseWh+50_000), maxPowerW, settings)
		assert.Equal(t, idleAt(30, 25), got)
	})
}

func TestDetectIdleOfARunningSession(t *testing.T) {
	settings := rating.DefaultIdleSettings()
	running := func(points []rating.EnergyPoint, nowMinute float64) rating.IdleResult {
		return rating.DetectIdle(points, sessionStart, nil, minute(nowMinute), nil, maxPowerW, settings)
	}

	assert.Equal(t, rating.IdleResult{}, running(readings(0, 0, 30, 5.5), 32), "it charged up to its newest reading")
	assert.Equal(t, idleAt(30, 25), running(readings(0, 0, 30, 5.5, 40, 5.5), 55), "idle up to now")
	assert.Equal(t, idleAt(0, 20), running(readings(0, 0, 10, 0), 20), "it has not charged yet")
	assert.Equal(t, rating.IdleResult{}, running(nil, 20), "nothing to tell from")
	assert.Equal(t, idleAt(30, 25), running(readings(0, 0, 30, 5.5, 40, 5.5, 300, 60), 55),
		"a reading that is timed long after now is ignored")
}

// A session charges from the higher of the minimum charging power and the share of the
// highest power of the connector.
func TestDetectIdleThreshold(t *testing.T) {
	// an hour at 11 kW, then an hour at 1 kW
	points := readings(0, 0, 60, 11, 120, 12)
	stop := minute(120)
	detectWith := func(maxPowerW float64, s rating.IdleSettings) rating.IdleResult {
		return rating.DetectIdle(points, sessionStart, &stop, stop, nil, maxPowerW, s)
	}
	defaults := rating.DefaultIdleSettings()

	assert.Equal(t, idleAt(120, 0), detectWith(22_000, defaults), "1 kW is above 1 % of 22 kW")
	assert.Equal(t, idleAt(60, 60), detectWith(150_000, defaults), "1 kW is below 1 % of 150 kW")
	assert.Equal(t, idleAt(120, 0), detectWith(0, defaults), "a highest power that is not known leaves the minimum of 200 W")
	assert.Equal(t, idleAt(120, 0), detectWith(100_000, defaults), "a power that is exactly the threshold is charging")
	assert.Equal(t, idleAt(60, 60), detectWith(100_001, defaults))

	higherMinimum := defaults
	higherMinimum.MinChargingPowerW = 1500
	assert.Equal(t, idleAt(60, 60), detectWith(22_000, higherMinimum))
	higherShare := defaults
	higherShare.ChargingPowerShare = 0.05
	assert.Equal(t, idleAt(60, 60), detectWith(22_000, higherShare), "1 kW is below 5 % of 22 kW")
}

// A reading is used when it is timed up to the tolerance before the start or after the
// end of the session.
func TestDetectIdleTimestampTolerance(t *testing.T) {
	settings := rating.DefaultIdleSettings()
	stop := minute(60)
	// the session drew nothing; the readings around it tell of charging before its start
	// and after its end
	within := []rating.EnergyPoint{reading(-60, -11), reading(0, 0), reading(60, 0), reading(120, 11)}
	got := rating.DetectIdle(within, sessionStart, &stop, stop, nil, maxPowerW, settings)
	assert.Equal(t, idleAt(60, 0), got, "the reading an hour after the stop is used, and the idle time ends with the session")

	beyond := []rating.EnergyPoint{reading(-61, -11), reading(0, 0), reading(60, 0), reading(121, 11)}
	got = rating.DetectIdle(beyond, sessionStart, &stop, stop, nil, maxPowerW, settings)
	assert.Equal(t, idleAt(0, 60), got)

	settings.TimestampTolerance = 0
	got = rating.DetectIdle(within, sessionStart, &stop, stop, nil, maxPowerW, settings)
	assert.Equal(t, idleAt(0, 60), got, "without a tolerance only the readings of the session count")
}

func TestDetectIdleLeavesTheReadingsAsTheyAre(t *testing.T) {
	points := readings(120, 11, 0, 0, 60, 11, 30, 5.5)
	before := append([]rating.EnergyPoint(nil), points...)

	detect(points, 120)
	rating.AveragePowerW(points, 200)
	rating.IsParkingNow(points, sessionStart, minute(130), maxPowerW, rating.DefaultIdleSettings())

	assert.Equal(t, before, points)
}

func TestAveragePowerW(t *testing.T) {
	for name, tc := range map[string]struct {
		points []rating.EnergyPoint
		want   *float64
	}{
		"an hour at 11 kW and then parked": {readings(0, 0, 60, 11, 120, 11), ptr(11_000.0)},
		"two powers, by their time":        {readings(0, 0, 60, 11, 90, 12.85), ptr(8566.666666666666)},
		"an interval at the minimum power is not counted": {
			readingsWh(0, 0, 60, 11_000, 120, 11_200), ptr(11_000.0),
		},
		"an interval just above the minimum power is counted": {
			readingsWh(0, 0, 60, 11_000, 120, 11_201), ptr(5600.5),
		},
		"no interval above the minimum power": {readings(0, 0, 60, 0.1, 120, 0.2), ptr(0.0)},
		"readings in any order":               {readings(60, 11, 0, 0, 120, 11), ptr(11_000.0)},
		"a register that went back":           {readings(0, 0, 60, 11, 90, 5, 150, 16), ptr(11_000.0)},
		"readings of the same moment":         {readings(0, 0, 60, 11, 60, 11, 60, 11.5), ptr(11_000.0)},
		// between every two readings: the minutes without a step are not counted, so a
		// charger of 3.6 kW under a register with steps of 100 Wh shows the power of a step
		"a register that moves in steps counts only the intervals with a step": {
			readings(0, 0, 1, 0, 2, 0.1, 3, 0.1, 4, 0.2, 5, 0.3), ptr(6000.0),
		},
		"one reading": {readings(0, 0), nil},
		"no reading":  {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := rating.AveragePowerW(tc.points, 200)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.InDelta(t, *tc.want, *got, 1e-6)
		})
	}
}

func TestIsParkingNow(t *testing.T) {
	settings := rating.DefaultIdleSettings()
	parked := readings(0, 0, 0.25, 0, 0.75, 0)

	assert.False(t, rating.IsParkingNow(parked, sessionStart, sessionStart.Add(30*time.Second), maxPowerW, settings),
		"not in the first minute of a session")
	assert.False(t, rating.IsParkingNow(parked, sessionStart, sessionStart.Add(59*time.Second), maxPowerW, settings))
	assert.True(t, rating.IsParkingNow(parked, sessionStart, sessionStart.Add(60*time.Second), maxPowerW, settings))
	assert.True(t, rating.IsParkingNow(parked, sessionStart, minute(5), maxPowerW, settings), "no power after five minutes")

	// 1 % of 22 kW is 220 W
	assert.True(t, rating.IsParkingNow(readingsWh(0, 0, 60, 5000, 120, 5220), sessionStart, minute(121), maxPowerW, settings), "exactly 1 %")
	assert.False(t, rating.IsParkingNow(readingsWh(0, 0, 60, 5000, 120, 5221), sessionStart, minute(121), maxPowerW, settings), "above 1 %")
	assert.False(t, rating.IsParkingNow(readings(0, 0, 5, 0.9), sessionStart, minute(5), maxPowerW, settings), "charging")
	assert.True(t, rating.IsParkingNow(readings(0, 0, 30, 5.5, 40, 5.5), sessionStart, minute(41), maxPowerW, settings),
		"the power between the two newest readings counts")
	assert.False(t, rating.IsParkingNow(readings(0, 0, 30, 0, 40, 2), sessionStart, minute(41), maxPowerW, settings))
	assert.True(t, rating.IsParkingNow(readings(40, 5.5, 0, 0, 30, 5.5), sessionStart, minute(41), maxPowerW, settings), "in any order")

	// the newest reading twice: the power is measured from the reading before it
	assert.False(t, rating.IsParkingNow(readings(0, 0, 30, 5.5, 40, 7, 40, 7), sessionStart, minute(41), maxPowerW, settings))
	assert.True(t, rating.IsParkingNow(readings(0, 0, 30, 5.5, 40, 5.5, 40, 5.5), sessionStart, minute(41), maxPowerW, settings))
	assert.False(t, rating.IsParkingNow(readings(40, 7, 40, 7), sessionStart, minute(41), maxPowerW, settings), "readings of one moment only")

	assert.False(t, rating.IsParkingNow(readings(0, 0), sessionStart, minute(5), maxPowerW, settings), "nothing to measure between")
	assert.False(t, rating.IsParkingNow(nil, sessionStart, minute(5), maxPowerW, settings))

	// without a highest power only a session that draws nothing is parked
	assert.True(t, rating.IsParkingNow(parked, sessionStart, minute(5), 0, settings))
	assert.False(t, rating.IsParkingNow(readings(0, 0, 2, 0.5, 5, 0.501), sessionStart, minute(5), 0, settings))

	// a reading of a clock that is wrong does not count
	assert.True(t, rating.IsParkingNow(readings(0, 0, 30, 5.5, 40, 5.5, 900, 80), sessionStart, minute(41), maxPowerW, settings))

	later := settings
	later.FreshSession = 10 * time.Minute
	assert.False(t, rating.IsParkingNow(parked, sessionStart, minute(5), maxPowerW, later))
}

// The idle settings of a tariff version replace the defaults of the manager, each on its
// own.
func TestMergeIdleSettings(t *testing.T) {
	defaults := rating.IdleSettings{ChargingPowerShare: 0.01, MinChargingPowerW: 200, TimestampTolerance: time.Hour, FreshSession: time.Minute}

	assert.Equal(t, defaults, rating.MergeIdleSettings(defaults, nil))
	assert.Equal(t, defaults, rating.MergeIdleSettings(defaults, &store.IdleDetection{}))

	share := defaults
	share.ChargingPowerShare = 0.05
	assert.Equal(t, share, rating.MergeIdleSettings(defaults, &store.IdleDetection{ChargingPowerShare: ptr(0.05)}))

	power := defaults
	power.MinChargingPowerW = 0
	assert.Equal(t, power, rating.MergeIdleSettings(defaults, &store.IdleDetection{MinChargingPowerW: ptr(0.0)}),
		"0 W is a setting, not the absence of one")

	both := rating.IdleSettings{ChargingPowerShare: 0.02, MinChargingPowerW: 500, TimestampTolerance: time.Hour, FreshSession: time.Minute}
	assert.Equal(t, both, rating.MergeIdleSettings(defaults, &store.IdleDetection{ChargingPowerShare: ptr(0.02), MinChargingPowerW: ptr(500.0)}))
}

func TestDefaultIdleSettings(t *testing.T) {
	assert.Equal(t, rating.IdleSettings{
		ChargingPowerShare: 0.01,
		MinChargingPowerW:  200,
		TimestampTolerance: time.Hour,
		FreshSession:       60 * time.Second,
	}, rating.DefaultIdleSettings())
}
