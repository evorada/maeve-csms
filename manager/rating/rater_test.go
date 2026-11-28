// SPDX-License-Identifier: Apache-2.0

package rating_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

// ratingFixture is a file of testdata: a tariff version, a session and what the session
// costs with it. The amounts of the fixtures were worked out by hand.
type ratingFixture struct {
	Id          string `json:"id"`
	Description string `json:"description"`
	// SimpleFixture names a fixture of the simple tariff form (package tariffs) that
	// converts to Tariff.
	SimpleFixture string              `json:"simple_fixture"`
	Tariff        store.TariffVersion `json:"tariff"`
	// Rounding is left out for the default rounding.
	Rounding *struct {
		Scale int                `json:"scale"`
		Mode  money.RoundingMode `json:"mode"`
	} `json:"rounding"`
	Session  rating.Session  `json:"session"`
	Expected json.RawMessage `json:"expected"`
}

// fixtureTariffId is the tariff that the sessions of the fixtures are rated with.
const fixtureTariffId = "0193b2c4-0000-7000-8000-00000000f001"

// resultDocument is the JSON form of a rating.Result in the fixtures.
type resultDocument struct {
	Currency        string                  `json:"currency"`
	Final           bool                    `json:"final"`
	TotalEnergyWh   int64                   `json:"total_energy_wh"`
	TotalTimeS      int64                   `json:"total_time_s"`
	ChargingTimeS   int64                   `json:"charging_time_s"`
	ParkingTimeS    int64                   `json:"parking_time_s"`
	Breakdown       rating.PriceBreakdown   `json:"breakdown"`
	ChargingPeriods []rating.ChargingPeriod `json:"charging_periods"`
}

func resultJSON(t *testing.T, result *rating.Result) string {
	t.Helper()
	document, err := json.Marshal(resultDocument{
		Currency:        result.Currency,
		Final:           result.Final,
		TotalEnergyWh:   result.TotalEnergyWh,
		TotalTimeS:      result.TotalTimeS,
		ChargingTimeS:   result.ChargingTimeS,
		ParkingTimeS:    result.ParkingTimeS,
		Breakdown:       result.Breakdown,
		ChargingPeriods: result.ChargingPeriods,
	})
	require.NoError(t, err)
	return string(document)
}

func loadRatingFixtures(t *testing.T) map[string]ratingFixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	require.NoError(t, err)
	fixtures := map[string]ratingFixture{}
	for _, file := range files {
		document, err := os.ReadFile(file) // #nosec G304 -- a file of the test data
		require.NoError(t, err)
		var fixture ratingFixture
		require.NoError(t, json.Unmarshal(document, &fixture), file)
		fixtures[filepath.Base(file)] = fixture
	}
	return fixtures
}

func (f ratingFixture) rater() rating.Rater {
	if f.Rounding == nil {
		return rating.Rater{}
	}
	return rating.Rater{Rounding: rating.Rounding{Scale: f.Rounding.Scale, Mode: f.Rounding.Mode}}
}

// Every fixture rates to the amounts, the steps, the grace seconds and the charging
// periods that it documents; the tariff of a fixture is a valid tariff version; and the
// tariff in the simple form that a fixture names converts to the tariff of the fixture
// and so rates to the same result.
func TestRateFixtures(t *testing.T) {
	fixtures := loadRatingFixtures(t)
	ids := map[string]bool{}
	for file, fixture := range fixtures {
		t.Run(file, func(t *testing.T) {
			ids[fixture.Id] = true
			assert.NotEmpty(t, fixture.Description)

			version := fixture.Tariff
			require.Empty(t, tariffs.Validate(&version))
			assert.Equal(t, fixture.Tariff, version, "the tariff of a fixture is in its stored form")

			result, err := fixture.rater().Rate(fixtureTariffId, &version, fixture.Session)
			require.NoError(t, err)
			assert.JSONEq(t, string(fixture.Expected), resultJSON(t, result))
			assert.Equal(t, fixtureTariffId, result.TariffId)

			if fixture.SimpleFixture == "" {
				return
			}
			document, err := os.ReadFile(filepath.Join("..", "tariffs", "testdata", "simple", fixture.SimpleFixture)) // #nosec G304 -- a file of the test data
			require.NoError(t, err)
			var simple struct {
				Simple tariffs.SimpleTariff `json:"simple"`
			}
			require.NoError(t, json.Unmarshal(document, &simple))
			converted, err := tariffs.FromSimple(simple.Simple)
			require.NoError(t, err)
			assert.Equal(t, &version, converted)

			result, err = fixture.rater().Rate(fixtureTariffId, converted, fixture.Session)
			require.NoError(t, err)
			assert.JSONEq(t, string(fixture.Expected), resultJSON(t, result))
		})
	}
	for _, id := range []string{"A", "A-running", "A-half-even", "B", "C", "D", "E", "F", "S1", "S6", "S8"} {
		assert.True(t, ids[id], "no fixture for %s", id)
	}
	assert.Len(t, fixtures, 22)
}

// Every shape of the simple tariff form has a fixture that rates it. S5b is S5 with one
// more window, which the fixture of two parking windows covers.
func TestRateFixturesCoverTheSimpleForm(t *testing.T) {
	named := map[string]bool{}
	for _, fixture := range loadRatingFixtures(t) {
		named[fixture.SimpleFixture] = true
	}
	files, err := filepath.Glob(filepath.Join("..", "tariffs", "testdata", "simple", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 9)
	for _, file := range files {
		if name := filepath.Base(file); name != "S5b-parking-windows-two-times.json" {
			assert.True(t, named[name], "no rating fixture for %s", name)
		}
	}
}

// S5b: parking is charged in the windows of two times of day. A Saturday in Europe/Madrid:
// idle from 13:30 local time, the grace period ends at 13:45 and the window at 14:00.
func TestRateTheSimpleFormWithTwoWindows(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "tariffs", "testdata", "simple", "S5b-parking-windows-two-times.json")) // #nosec G304 -- a file of the test data
	require.NoError(t, err)
	var simple struct {
		Simple tariffs.SimpleTariff `json:"simple"`
	}
	require.NoError(t, json.Unmarshal(document, &simple))
	version, err := tariffs.FromSimple(simple.Simple)
	require.NoError(t, err)

	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	saturday := time.Date(2026, 10, 3, 12, 30, 0, 0, madrid)
	session := rating.Session{
		Start:         saturday,
		Stop:          ptr(saturday.Add(150 * time.Minute)),
		Location:      madrid,
		Samples:       []rating.Sample{{At: saturday.Add(time.Hour), EnergyWh: 7400}},
		TotalEnergyWh: ptr(7400.0),
		IdleStartedAt: ptr(saturday.Add(time.Hour)),
	}
	result, err := rating.Rater{}.Rate("", version, session)
	require.NoError(t, err)

	// as example B: 15 minutes are charged as 2 steps of 0.20
	assert.Equal(t, money.Price{ExclVat: 2_2200, InclVat: 2_6900}, result.Breakdown.Energy.Cost)
	assert.Equal(t, rating.TimeBreakdown{
		TotalS: 5400, GraceS: 900, BillableS: 900, StepSizeS: 600, Steps: 2, Minutes: 90,
		Cost: money.Price{ExclVat: 4000, InclVat: 4800},
	}, result.Breakdown.Parking)
	assert.Equal(t, money.Price{ExclVat: 2_6200, InclVat: 3_1700}, result.Breakdown.Total)

	// the same session on a Sunday: no window, no parking price
	sunday := saturday.AddDate(0, 0, 1)
	session.Start, session.Stop, session.IdleStartedAt = sunday, ptr(sunday.Add(150*time.Minute)), ptr(sunday.Add(time.Hour))
	session.Samples = []rating.Sample{{At: sunday.Add(time.Hour), EnergyWh: 7400}}
	result, err = rating.Rater{}.Rate("", version, session)
	require.NoError(t, err)
	assert.Equal(t, rating.TimeBreakdown{TotalS: 5400, GraceS: 900, Minutes: 90}, result.Breakdown.Parking)
	assert.Equal(t, money.Price{ExclVat: 2_2200, InclVat: 2_6900}, result.Breakdown.Total)
}

// rateStart is the start of the sessions of the tests below: a Thursday at 10:00 UTC.
var rateStart = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func rateMinute(m float64) time.Time {
	return rateStart.Add(time.Duration(m * float64(time.Minute)))
}

func amount(t *testing.T, s string) money.Amount {
	t.Helper()
	a, err := money.ParseAmount(s)
	require.NoError(t, err)
	return a
}

func price(t *testing.T, excl, incl string) money.Price {
	t.Helper()
	return money.Price{ExclVat: amount(t, excl), InclVat: amount(t, incl)}
}

// energyAt is a price component for energy without VAT.
func energyAt(t *testing.T, perKWh string) store.PriceComponent {
	t.Helper()
	return store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, perKWh), StepSize: 1}
}

func tariffOf(elements ...store.TariffElement) *store.TariffVersion {
	return &store.TariffVersion{Currency: "EUR", Elements: elements}
}

func elementOf(restrictions *store.TariffRestrictions, components ...store.PriceComponent) store.TariffElement {
	return store.TariffElement{PriceComponents: components, Restrictions: restrictions}
}

// endedSession is a session from rateStart that stopped after so many minutes with the
// energy totalWh. Without samples the energy rises evenly.
func endedSession(minutes, totalWh float64, samples ...rating.Sample) rating.Session {
	return rating.Session{Start: rateStart, Stop: ptr(rateMinute(minutes)), Samples: samples, TotalEnergyWh: &totalWh}
}

func sampleAt(minute, wh float64) rating.Sample {
	return rating.Sample{At: rateMinute(minute), EnergyWh: wh}
}

func rate(t *testing.T, v *store.TariffVersion, s rating.Session) *rating.Result {
	t.Helper()
	require.Empty(t, tariffs.Validate(v), "the tariff of the test is not valid")
	result, err := rating.Rater{}.Rate("", v, s)
	require.NoError(t, err)
	return result
}

// A grace period runs on the clock from the start of the idle time, whether or not a
// priced window is open, and only the time after it is charged.
func TestRateGracePeriodRunsOnTheClock(t *testing.T) {
	parking := store.PriceComponent{
		Type: store.PriceComponentParkingTime, StepSize: 600, GracePeriodS: ptr(int32(900)), PricePerStep: ptr(amount(t, "0.20")),
	}
	version := tariffOf(elementOf(&store.TariffRestrictions{StartTime: ptr("09:00"), EndTime: ptr("20:00")}, parking))
	start := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)
	session := func(idleAt, stop string) rating.Session {
		return rating.Session{
			Start:         start,
			Stop:          ptr(mustTime(t, "2026-10-01T"+stop+":00Z")),
			IdleStartedAt: ptr(mustTime(t, "2026-10-01T"+idleAt+":00Z")),
			TotalEnergyWh: ptr(0.0),
		}
	}

	t.Run("a grace period that ends in the window", func(t *testing.T) {
		// idle from 08:50: the grace period ends at 09:05, five minutes into the window
		result := rate(t, version, session("08:50", "09:35"))
		assert.Equal(t, rating.TimeBreakdown{
			TotalS: 2700, GraceS: 900, BillableS: 1800, StepSizeS: 600, Steps: 3, Minutes: 45, Cost: price(t, "0.60", "0.60"),
		}, result.Breakdown.Parking)
	})

	t.Run("a grace period before the window is used up", func(t *testing.T) {
		// idle from 08:00: the grace period ends at 08:15, the window opens at 09:00
		result := rate(t, version, session("08:00", "09:20"))
		assert.Equal(t, rating.TimeBreakdown{
			TotalS: 4800, GraceS: 900, BillableS: 1200, StepSizeS: 600, Steps: 2, Minutes: 80, Cost: price(t, "0.40", "0.40"),
		}, result.Breakdown.Parking)
		assert.Equal(t, []rating.ChargingPeriod{
			{StartDateTime: start, Dimensions: []rating.Dimension{{Type: "TIME", Volume: 1}}},
			{StartDateTime: start.Add(60 * time.Minute), Dimensions: []rating.Dimension{{Type: "PARKING_TIME", Volume: 0.25}}},
			{StartDateTime: start.Add(75 * time.Minute), Dimensions: []rating.Dimension{{Type: "PARKING_TIME", Volume: 0.75}}},
			{StartDateTime: start.Add(120 * time.Minute), Dimensions: []rating.Dimension{{Type: "PARKING_TIME", Volume: 0.3333}}},
		}, result.ChargingPeriods)
	})

	t.Run("idle time that ends in the grace period costs nothing", func(t *testing.T) {
		result := rate(t, version, session("10:00", "10:10"))
		assert.Equal(t, rating.TimeBreakdown{TotalS: 600, GraceS: 600, Minutes: 10}, result.Breakdown.Parking)
		assert.Equal(t, money.Price{}, result.Breakdown.Total)
	})

	t.Run("the grace period of a dimension is that of its first component", func(t *testing.T) {
		// a tariff that is not valid: the components of a valid one have the same grace
		// period. The first component, which has none, decides
		withoutGrace := parking
		withoutGrace.GracePeriodS = nil
		mixed := tariffOf(
			elementOf(&store.TariffRestrictions{StartTime: ptr("06:00"), EndTime: ptr("07:00")}, withoutGrace),
			elementOf(nil, parking),
		)
		tariffs.Normalize(mixed)
		result, err := rating.Rater{}.Rate("", mixed, session("10:00", "10:10"))
		require.NoError(t, err)
		assert.Equal(t, rating.TimeBreakdown{
			TotalS: 600, BillableS: 600, StepSizeS: 600, Steps: 1, Minutes: 10, Cost: price(t, "0.20", "0.20"),
		}, result.Breakdown.Parking)
	})
}

func mustTime(t *testing.T, text string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	require.NoError(t, err)
	return at
}

// Charging time is charged while the session charges, from the end of its grace period,
// which starts with the session. The time after the session stopped charging is parking
// time.
func TestRateChargingTime(t *testing.T) {
	// 0.10 per 5 minutes, the first 30 minutes free
	version := tariffOf(elementOf(nil, store.PriceComponent{
		Type: store.PriceComponentTime, StepSize: 300, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(amount(t, "0.10")),
	}))

	t.Run("the idle time is not charging time", func(t *testing.T) {
		session := endedSession(120, 0)
		session.IdleStartedAt = ptr(rateMinute(60))
		result := rate(t, version, session)
		assert.Equal(t, rating.TimeBreakdown{
			TotalS: 3600, GraceS: 1800, BillableS: 1800, StepSizeS: 300, Steps: 6, Minutes: 60, Cost: price(t, "0.60", "0.60"),
		}, result.Breakdown.Time)
		assert.Equal(t, rating.TimeBreakdown{TotalS: 3600, Minutes: 60}, result.Breakdown.Parking)
		assert.Equal(t, int64(7200), result.TotalTimeS)
		assert.Equal(t, int64(3600), result.ChargingTimeS)
		assert.Equal(t, int64(3600), result.ParkingTimeS)
	})

	t.Run("a session that stops charging in the grace period", func(t *testing.T) {
		session := endedSession(60, 0)
		session.IdleStartedAt = ptr(rateMinute(20))
		result := rate(t, version, session)
		assert.Equal(t, rating.TimeBreakdown{TotalS: 1200, GraceS: 1200, Minutes: 20}, result.Breakdown.Time)
		assert.Equal(t, money.Price{}, result.Breakdown.Total)
	})

	t.Run("a started step is a whole step", func(t *testing.T) {
		// 30 minutes and 1 second after the grace period
		session := rating.Session{Start: rateStart, Stop: ptr(rateStart.Add(60*time.Minute + time.Second))}
		result := rate(t, version, session)
		assert.Equal(t, rating.TimeBreakdown{
			TotalS: 3601, GraceS: 1800, BillableS: 1801, StepSizeS: 300, Steps: 7, Minutes: 60.0167, Cost: price(t, "0.70", "0.70"),
		}, result.Breakdown.Time)
	})
}

// A time component with a price per step is charged by that price, not by the price per
// hour that was derived from it and rounded: 0.10 per 7 minutes is 0.8571 per hour.
func TestRatePricePerStepIsExact(t *testing.T) {
	version := tariffOf(elementOf(nil, store.PriceComponent{
		Type: store.PriceComponentTime, StepSize: 420, PricePerStep: ptr(amount(t, "0.10")),
	}))
	require.Empty(t, tariffs.Validate(version))
	require.Equal(t, amount(t, "0.8571"), version.Elements[0].PriceComponents[0].Price)

	// 100 steps: 10.0000, where 11 h 40 min at 0.8571 per hour would be 9.9995
	rater := rating.Rater{Rounding: rating.Rounding{Scale: 4, Mode: money.RoundHalfUp}}
	result, err := rater.Rate("", version, rating.Session{Start: rateStart, Stop: ptr(rateStart.Add(42000 * time.Second))})
	require.NoError(t, err)
	assert.Equal(t, price(t, "10", "10"), result.Breakdown.Time.Cost)
	assert.Equal(t, int64(100), result.Breakdown.Time.Steps)

	// without a price per step the price per hour counts: 1.20 per hour in steps of 5 minutes
	perHour := tariffOf(elementOf(nil, store.PriceComponent{Type: store.PriceComponentTime, StepSize: 300, Price: amount(t, "1.20")}))
	result = rate(t, perHour, endedSession(82, 0))
	assert.Equal(t, int64(17), result.Breakdown.Time.Steps)
	assert.Equal(t, price(t, "1.70", "1.70"), result.Breakdown.Time.Cost)
}

// A flat fee is charged once, with the first segment in which an element with a flat fee
// applies.
func TestRateFlatFee(t *testing.T) {
	vat := ptr(money.Percent(2100))
	flat := func(price string) store.PriceComponent {
		return store.PriceComponent{Type: store.PriceComponentFlat, Price: amount(t, price), Vat: vat}
	}

	t.Run("once for a session of many segments", func(t *testing.T) {
		version := tariffOf(elementOf(nil, flat("1.00"), energyAt(t, "0.30")))
		result := rate(t, version, endedSession(60, 10000, sampleAt(10, 1000), sampleAt(20, 4000), sampleAt(40, 9000)))
		assert.Equal(t, price(t, "1.00", "1.21"), result.Breakdown.Flat)
		assert.Equal(t, price(t, "4.00", "4.21"), result.Breakdown.Total)
	})

	t.Run("of an element that applies later in the session", func(t *testing.T) {
		// a fee for a session that lasts more than 10 minutes
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinDuration: ptr(int32(600))}, flat("2.00")),
			elementOf(nil, energyAt(t, "0")),
		)
		assert.Equal(t, money.Price{}, rate(t, version, endedSession(5, 1000)).Breakdown.Flat)
		assert.Equal(t, money.Price{}, rate(t, version, endedSession(10, 1000)).Breakdown.Flat)
		assert.Equal(t, price(t, "2.00", "2.42"), rate(t, version, endedSession(15, 1000)).Breakdown.Flat)
	})

	t.Run("the fee of the first element that applies at that moment", func(t *testing.T) {
		// a night fee from 22:00 and a day fee: a session from 21:30 to 22:30 pays the day
		// fee, and not the night fee as well
		night := &store.TariffRestrictions{StartTime: ptr("22:00"), EndTime: ptr("06:00")}
		version := tariffOf(elementOf(night, flat("5.00")), elementOf(nil, flat("1.00")))
		session := rating.Session{Start: rateStart.Add(690 * time.Minute), Stop: ptr(rateStart.Add(750 * time.Minute))}
		assert.Equal(t, price(t, "1.00", "1.21"), rate(t, version, session).Breakdown.Flat)

		// and a session from 22:10 the night fee
		session = rating.Session{Start: rateStart.Add(730 * time.Minute), Stop: ptr(rateStart.Add(750 * time.Minute))}
		assert.Equal(t, price(t, "5.00", "6.05"), rate(t, version, session).Breakdown.Flat)
	})
}

// A restriction on the current looks at the newest sample at or before the start of a
// segment. Without a measured current it does not hold.
func TestRateCurrentRestrictions(t *testing.T) {
	withCurrent := func(minute, wh float64, current *float64) rating.Sample {
		return rating.Sample{At: rateMinute(minute), EnergyWh: wh, CurrentA: current}
	}
	// 16 A in the first half hour (5 kWh), 40 A in the second (7 kWh)
	session := endedSession(60, 12000, withCurrent(0, 0, ptr(16.0)), withCurrent(30, 5000, ptr(40.0)))

	t.Run("from a current", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinCurrent: ptr(32.0)}, energyAt(t, "0.50")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		assert.Equal(t, price(t, "5.00", "5.00"), rate(t, version, session).Breakdown.Total)
	})

	t.Run("the limits: from the lower one, below the upper one", func(t *testing.T) {
		for _, tc := range []struct {
			min, max *float64
			total    string
		}{
			{min: ptr(16.0), total: "6.00"},                 // both halves
			{min: ptr(16.1), total: "5.00"},                 // the second half
			{max: ptr(40.0), total: "4.60"},                 // the first half
			{max: ptr(40.1), total: "6.00"},                 // both
			{max: ptr(16.0), total: "3.60"},                 // none
			{min: ptr(16.0), max: ptr(40.0), total: "4.60"}, // the first half
		} {
			version := tariffOf(
				elementOf(&store.TariffRestrictions{MinCurrent: tc.min, MaxCurrent: tc.max}, energyAt(t, "0.50")),
				elementOf(nil, energyAt(t, "0.30")),
			)
			assert.Equal(t, price(t, tc.total, tc.total), rate(t, version, session).Breakdown.Total, "min %v max %v", tc.min, tc.max)
		}
	})

	t.Run("a newer sample without a current", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinCurrent: ptr(10.0)}, energyAt(t, "0.50")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		unmeasured := endedSession(60, 12000, withCurrent(0, 0, ptr(16.0)), withCurrent(30, 5000, nil))
		// 5 kWh at 0.50, then the current is not known: 7 kWh at 0.30
		assert.Equal(t, price(t, "4.60", "4.60"), rate(t, version, unmeasured).Breakdown.Total)
	})
}

// A restriction on the power looks at the power of the segment: the lower limit is part
// of what it allows, the upper limit is not.
func TestRatePowerRestrictions(t *testing.T) {
	// half an hour at 60 kW (30 kWh), half an hour at 20 kW (10 kWh)
	session := endedSession(60, 40000, sampleAt(30, 30000))
	for _, tc := range []struct {
		min, max *float64
		total    string
	}{
		{min: ptr(60.0), total: "18.00"},                 // the first half at 0.50
		{min: ptr(60.5), total: "12.00"},                 // none
		{min: ptr(20.0), total: "20.00"},                 // both
		{max: ptr(60.0), total: "14.00"},                 // the second half
		{max: ptr(20.0), total: "12.00"},                 // none
		{min: ptr(20.0), max: ptr(60.0), total: "14.00"}, // the second half
	} {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinPower: tc.min, MaxPower: tc.max}, energyAt(t, "0.50")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		assert.Equal(t, price(t, tc.total, tc.total), rate(t, version, session).Breakdown.Total, "min %v max %v", tc.min, tc.max)
	}
}

// A restriction on the energy looks at the energy that the session had taken at the start
// of a segment, and the session is cut where it reaches a limit.
func TestRateEnergyRestrictions(t *testing.T) {
	// 10 kWh in an hour, evenly
	session := endedSession(60, 10000)

	t.Run("up to an amount", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MaxKwh: ptr(5.0)}, energyAt(t, "0.20")),
			elementOf(nil, energyAt(t, "0.40")),
		)
		result := rate(t, version, session)
		assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
		require.Len(t, result.ChargingPeriods, 2)
		assert.Equal(t, rateMinute(30), result.ChargingPeriods[1].StartDateTime)
	})

	t.Run("between two amounts", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinKwh: ptr(2.5), MaxKwh: ptr(7.5)}, energyAt(t, "0.20")),
			elementOf(nil, energyAt(t, "0.40")),
		)
		result := rate(t, version, session)
		// 2.5 kWh at 0.40, 5 kWh at 0.20, 2.5 kWh at 0.40
		assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
		require.Len(t, result.ChargingPeriods, 3)
		assert.Equal(t, rateMinute(15), result.ChargingPeriods[1].StartDateTime)
		assert.Equal(t, rateMinute(45), result.ChargingPeriods[2].StartDateTime)
	})

	t.Run("an amount that the session does not reach", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinKwh: ptr(10.0)}, energyAt(t, "0.20")),
			elementOf(nil, energyAt(t, "0.40")),
		)
		result := rate(t, version, session)
		assert.Equal(t, price(t, "4.00", "4.00"), result.Breakdown.Total)
		assert.Len(t, result.ChargingPeriods, 1)
	})

	t.Run("an amount that is reached while the register stands still", func(t *testing.T) {
		// 5 kWh after 20 minutes, nothing for 20 minutes, then 5 kWh more: the session has
		// 5 kWh from minute 20 on
		version := tariffOf(
			elementOf(&store.TariffRestrictions{MinKwh: ptr(5.0)}, energyAt(t, "0.20")),
			elementOf(nil, energyAt(t, "0.40")),
		)
		result := rate(t, version, endedSession(60, 10000, sampleAt(20, 5000), sampleAt(40, 5000)))
		assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
	})
}

// The times of day, the dates and the weekdays of the restrictions are read on the clock
// of the location of the session.
func TestRateTimeOfDayRestrictions(t *testing.T) {
	// 1 kW from 21:00 to 01:00 UTC
	evening := time.Date(2026, 10, 1, 21, 0, 0, 0, time.UTC)
	session := rating.Session{Start: evening, Stop: ptr(evening.Add(4 * time.Hour)), TotalEnergyWh: ptr(4000.0)}
	for name, tc := range map[string]struct {
		restrictions store.TariffRestrictions
		total        string
	}{
		"an end of 00:00 is the end of the day":                  {store.TariffRestrictions{StartTime: ptr("22:00"), EndTime: ptr("00:00")}, "0.80"},
		"only a start lasts to the end of the day":               {store.TariffRestrictions{StartTime: ptr("22:00")}, "0.80"},
		"only an end starts with the day":                        {store.TariffRestrictions{EndTime: ptr("00:30")}, "1.10"},
		"a window past midnight":                                 {store.TariffRestrictions{StartTime: ptr("23:00"), EndTime: ptr("00:30")}, "0.90"},
		"the end is not part of the window":                      {store.TariffRestrictions{StartTime: ptr("20:00"), EndTime: ptr("21:00")}, "1.20"},
		"the start is part of the window":                        {store.TariffRestrictions{StartTime: ptr("21:00"), EndTime: ptr("21:30")}, "1.10"},
		"a window of no length":                                  {store.TariffRestrictions{StartTime: ptr("22:00"), EndTime: ptr("22:00")}, "1.20"},
		"the whole day":                                          {store.TariffRestrictions{StartTime: ptr("00:00"), EndTime: ptr("00:00")}, "0.40"},
		"a weekday":                                              {store.TariffRestrictions{DayOfWeek: []store.DayOfWeek{store.Thursday}}, "0.60"},
		"another weekday":                                        {store.TariffRestrictions{DayOfWeek: []store.DayOfWeek{store.Friday, store.Sunday}}, "1.00"},
		"from a date":                                            {store.TariffRestrictions{StartDate: ptr("2026-10-02")}, "1.00"},
		"until a date, which is not part of the range":           {store.TariffRestrictions{EndDate: ptr("2026-10-02")}, "0.60"},
		"a range of dates":                                       {store.TariffRestrictions{StartDate: ptr("2026-10-01"), EndDate: ptr("2026-10-02")}, "0.60"},
		"a date and a time of day":                               {store.TariffRestrictions{StartDate: ptr("2026-10-02"), EndTime: ptr("00:30")}, "1.10"},
		"a range of dates before the session":                    {store.TariffRestrictions{StartDate: ptr("2026-09-01"), EndDate: ptr("2026-10-01")}, "1.20"},
		"a time that is not a time of day never applies":         {store.TariffRestrictions{StartTime: ptr("25:00")}, "1.20"},
		"a date that is not a date never applies":                {store.TariffRestrictions{StartDate: ptr("yesterday")}, "1.20"},
		"nor does an end date that is not a date":                {store.TariffRestrictions{EndDate: ptr("tomorrow")}, "1.20"},
		"a reservation that has expired is not a session either": {store.TariffRestrictions{Reservation: ptr(store.ReservationRestrictionReservationExpires)}, "1.20"},
	} {
		t.Run(name, func(t *testing.T) {
			version := tariffOf(elementOf(&tc.restrictions, energyAt(t, "0.10")), elementOf(nil, energyAt(t, "0.30")))
			result, err := rating.Rater{}.Rate("", version, session)
			require.NoError(t, err)
			assert.Equal(t, price(t, tc.total, tc.total), result.Breakdown.Total)
		})
	}

	t.Run("in the time zone of the location", func(t *testing.T) {
		// 21:00 UTC is 23:00 in Madrid: the day changes after one hour
		madrid, err := time.LoadLocation("Europe/Madrid")
		require.NoError(t, err)
		local := session
		local.Location = madrid
		version := tariffOf(
			elementOf(&store.TariffRestrictions{StartDate: ptr("2026-10-02")}, energyAt(t, "0.10")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		result := rate(t, version, local)
		assert.Equal(t, price(t, "0.60", "0.60"), result.Breakdown.Total)
		require.Len(t, result.ChargingPeriods, 2)
		assert.True(t, result.ChargingPeriods[1].StartDateTime.Equal(evening.Add(time.Hour)))
	})
}

// A date range that covers one day of a session that lasts 26 hours, in Europe/Madrid.
func TestRateDateRange(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	version := tariffOf(
		elementOf(&store.TariffRestrictions{StartDate: ptr("2026-10-02"), EndDate: ptr("2026-10-03")}, energyAt(t, "0.10")),
		elementOf(nil, energyAt(t, "0.30")),
	)
	// from 23:00 on 1 October to 01:00 on 3 October local time, at 1 kW
	start := time.Date(2026, 10, 1, 23, 0, 0, 0, madrid)
	session := rating.Session{Start: start, Stop: ptr(start.Add(26 * time.Hour)), Location: madrid, TotalEnergyWh: ptr(26000.0)}
	result := rate(t, version, session)
	// 1 kWh at 0.30, 24 kWh at 0.10, 1 kWh at 0.30
	assert.Equal(t, price(t, "3.00", "3.00"), result.Breakdown.Total)
	require.Len(t, result.ChargingPeriods, 3)
	assert.Equal(t, []rating.Dimension{{Type: "ENERGY", Volume: 24}, {Type: "TIME", Volume: 24}}, result.ChargingPeriods[1].Dimensions)
}

// The night in which the clocks are set forward, 2026-03-29 in Europe/Madrid: at 02:00
// winter time (01:00 UTC) it is 03:00. The clock never shows a time between 02:00 and
// 03:00.
func TestRateTheNightTheClocksAreSetForward(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	// from 01:00 winter time to 05:00 summer time: 3 hours at 10 kW
	start := time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC)
	session := rating.Session{Start: start, Stop: ptr(start.Add(3 * time.Hour)), Location: madrid, TotalEnergyWh: ptr(30000.0)}

	t.Run("a window in the hour that does not exist", func(t *testing.T) {
		version := tariffOf(
			elementOf(&store.TariffRestrictions{StartTime: ptr("02:00"), EndTime: ptr("03:00")}, energyAt(t, "0.10")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		result := rate(t, version, session)
		assert.Equal(t, price(t, "9.00", "9.00"), result.Breakdown.Total)
		assert.Len(t, result.ChargingPeriods, 1)
	})

	t.Run("a window that starts in the hour that does not exist", func(t *testing.T) {
		// 02:30 to 04:00: it is in force from the moment the clock jumps to 03:00
		version := tariffOf(
			elementOf(&store.TariffRestrictions{StartTime: ptr("02:30"), EndTime: ptr("04:00")}, energyAt(t, "0.10")),
			elementOf(nil, energyAt(t, "0.30")),
		)
		result := rate(t, version, session)
		// 10 kWh at 0.30, 10 kWh at 0.10, 10 kWh at 0.30
		assert.Equal(t, price(t, "7.00", "7.00"), result.Breakdown.Total)
		require.Len(t, result.ChargingPeriods, 3)
		assert.True(t, result.ChargingPeriods[1].StartDateTime.Equal(start.Add(time.Hour)))
		assert.True(t, result.ChargingPeriods[2].StartDateTime.Equal(start.Add(2*time.Hour)))
	})
}

// The curve of the energy is built from the samples that tell about the session: those
// before its start or after its end, those that are not a number and those that show less
// energy than a sample before them are left out, in whatever order they come.
func TestRateSamples(t *testing.T) {
	// 0.20 per kWh in the first half hour, 0.40 after it
	version := tariffOf(
		elementOf(&store.TariffRestrictions{MaxDuration: ptr(int32(1800))}, energyAt(t, "0.20")),
		elementOf(nil, energyAt(t, "0.40")),
	)
	clean := endedSession(60, 10000, sampleAt(0, 0), sampleAt(30, 5000))
	want := rate(t, version, clean)
	require.Equal(t, price(t, "3.00", "3.00"), want.Breakdown.Total)

	t.Run("samples that do not tell about the session", func(t *testing.T) {
		messy := endedSession(60, 10000,
			sampleAt(45, 4000),       // less than the sample before it
			sampleAt(70, 20000),      // after the end
			sampleAt(30, 5000),       // the one that counts
			sampleAt(20, math.NaN()), // not a number
			sampleAt(-5, 99999),      // before the start
			sampleAt(0, 700),         // at the start: the session has taken nothing yet
			sampleAt(10, math.Inf(1)),
			sampleAt(15, -3),
		)
		got := rate(t, version, messy)
		assert.Equal(t, want.Breakdown, got.Breakdown)
		assert.Equal(t, int64(10000), got.TotalEnergyWh)
	})

	t.Run("no sample counts for more than the total of an ended session", func(t *testing.T) {
		got := rate(t, version, endedSession(60, 10000, sampleAt(30, 5000), sampleAt(50, 12000)))
		assert.Equal(t, want.Breakdown, got.Breakdown)
		assert.Equal(t, int64(10000), got.TotalEnergyWh)
	})

	t.Run("two samples of the same moment: the higher one", func(t *testing.T) {
		got := rate(t, version, endedSession(60, 10000, sampleAt(30, 4000), sampleAt(30, 5000)))
		assert.Equal(t, want.Breakdown, got.Breakdown)
		got = rate(t, version, endedSession(60, 10000, sampleAt(30, 5000), sampleAt(30, 4000)))
		assert.Equal(t, want.Breakdown, got.Breakdown)
	})

	t.Run("an ended session without a total ends with its newest sample", func(t *testing.T) {
		session := rating.Session{Start: rateStart, Stop: ptr(rateMinute(60)), Samples: []rating.Sample{sampleAt(30, 5000), sampleAt(45, 8000)}}
		got := rate(t, version, session)
		assert.Equal(t, int64(8000), got.TotalEnergyWh)
		// 5 kWh at 0.20, 3 kWh at 0.40
		assert.Equal(t, price(t, "2.20", "2.20"), got.Breakdown.Total)
	})

	t.Run("a session that runs ends with its newest sample, whatever total it is given", func(t *testing.T) {
		session := rating.Session{
			Start: rateStart, Now: rateMinute(50), TotalEnergyWh: ptr(99999.0),
			Samples: []rating.Sample{sampleAt(30, 5000), sampleAt(45, 8000)},
		}
		got := rate(t, version, session)
		assert.False(t, got.Final)
		assert.Equal(t, int64(8000), got.TotalEnergyWh)
		assert.Equal(t, int64(3000), got.TotalTimeS)
		assert.Equal(t, price(t, "2.20", "2.20"), got.Breakdown.Total)
	})

	t.Run("a session without samples and without a total has no energy", func(t *testing.T) {
		got := rate(t, version, rating.Session{Start: rateStart, Stop: ptr(rateMinute(60))})
		assert.Equal(t, int64(0), got.TotalEnergyWh)
		assert.Equal(t, money.Price{}, got.Breakdown.Total)
		assert.Equal(t, []rating.ChargingPeriod{
			{StartDateTime: rateStart, Dimensions: []rating.Dimension{{Type: "TIME", Volume: 0.5}}},
			{StartDateTime: rateMinute(30), Dimensions: []rating.Dimension{{Type: "TIME", Volume: 0.5}}},
		}, got.ChargingPeriods)
	})

	t.Run("the noise of a conversion is not energy", func(t *testing.T) {
		// 7.4 kWh that became 7400.000000000001 Wh are 7400 Wh, not 7401
		perWh := tariffOf(elementOf(nil, energyAt(t, "0.30")))
		got := rate(t, perWh, endedSession(60, 7400.000000000001))
		assert.Equal(t, rating.EnergyBreakdown{Wh: 7400, BilledWh: 7400, Cost: price(t, "2.22", "2.22")}, got.Breakdown.Energy)
	})

	t.Run("energy that is taken while the session is idle is charged as energy", func(t *testing.T) {
		perWh := tariffOf(elementOf(nil, energyAt(t, "0.30")))
		session := endedSession(120, 10100, sampleAt(60, 10000))
		session.IdleStartedAt = ptr(rateMinute(60))
		got := rate(t, perWh, session)
		assert.Equal(t, price(t, "3.03", "3.03"), got.Breakdown.Energy.Cost)
		assert.Equal(t, []rating.ChargingPeriod{
			{StartDateTime: rateStart, Dimensions: []rating.Dimension{{Type: "ENERGY", Volume: 10}, {Type: "TIME", Volume: 1}}},
			{StartDateTime: rateMinute(60), Dimensions: []rating.Dimension{{Type: "ENERGY", Volume: 0.1}, {Type: "PARKING_TIME", Volume: 1}}},
		}, got.ChargingPeriods)
	})
}

// The idle time of a session lies in the session.
func TestRateIdleTimeAtTheEdges(t *testing.T) {
	// 0.50 per 15 minutes of parking
	version := tariffOf(elementOf(nil, store.PriceComponent{
		Type: store.PriceComponentParkingTime, StepSize: 900, PricePerStep: ptr(amount(t, "0.50")),
	}))
	for name, tc := range map[string]struct {
		idleAt   *time.Time
		parkingS int64
		total    string
	}{
		"not idle":                               {nil, 0, "0"},
		"idle from before the start":             {ptr(rateMinute(-30)), 3600, "2.00"},
		"idle from the start":                    {ptr(rateMinute(0)), 3600, "2.00"},
		"idle for the last quarter of an hour":   {ptr(rateMinute(45)), 900, "0.50"},
		"charged until the stop":                 {ptr(rateMinute(60)), 0, "0"},
		"an idle time that starts after the end": {ptr(rateMinute(90)), 0, "0"},
	} {
		t.Run(name, func(t *testing.T) {
			session := endedSession(60, 0)
			session.IdleStartedAt = tc.idleAt
			result := rate(t, version, session)
			assert.Equal(t, tc.parkingS, result.ParkingTimeS)
			assert.Equal(t, 3600-tc.parkingS, result.ChargingTimeS)
			assert.Equal(t, int64(3600), result.TotalTimeS)
			assert.Equal(t, price(t, tc.total, tc.total), result.Breakdown.Total)
		})
	}
}

// A session that ends at the moment at which it starts takes its energy in no time: it
// pays for the energy and the flat fee, has no power, and its only charging period has no
// time.
func TestRateASessionWithoutDuration(t *testing.T) {
	version := tariffOf(
		elementOf(&store.TariffRestrictions{MinPower: ptr(0.0)}, energyAt(t, "0.90")),
		// it starts with no energy taken
		elementOf(&store.TariffRestrictions{MinKwh: ptr(0.5)}, energyAt(t, "0.60")),
		elementOf(nil, store.PriceComponent{Type: store.PriceComponentFlat, Price: amount(t, "1.00")}, energyAt(t, "0.30"),
			store.PriceComponent{Type: store.PriceComponentTime, Price: amount(t, "6.00"), StepSize: 60}),
	)
	result := rate(t, version, endedSession(0, 1000, sampleAt(0, 400)))
	assert.Equal(t, price(t, "1.00", "1.00"), result.Breakdown.Flat)
	assert.Equal(t, rating.EnergyBreakdown{Wh: 1000, BilledWh: 1000, Cost: price(t, "0.30", "0.30")}, result.Breakdown.Energy)
	assert.Equal(t, rating.TimeBreakdown{}, result.Breakdown.Time)
	assert.Equal(t, price(t, "1.30", "1.30"), result.Breakdown.Total)
	assert.Equal(t, []rating.ChargingPeriod{
		{StartDateTime: rateStart, Dimensions: []rating.Dimension{{Type: "ENERGY", Volume: 1}}},
	}, result.ChargingPeriods)
	assert.Equal(t, int64(0), result.TotalTimeS)

	// and one without energy has nothing to list
	result = rate(t, version, endedSession(0, 0))
	assert.Equal(t, []rating.ChargingPeriod{{StartDateTime: rateStart, Dimensions: []rating.Dimension{}}}, result.ChargingPeriods)
}

func TestRateRejectsWhatCannotBeRated(t *testing.T) {
	version := tariffOf(elementOf(nil, energyAt(t, "0.30")))

	_, err := rating.Rater{}.Rate("", nil, endedSession(60, 1000))
	assert.ErrorContains(t, err, "no tariff version")

	_, err = rating.Rater{}.Rate("", version, endedSession(-1, 1000))
	assert.ErrorContains(t, err, "before its start")

	// a session that runs, with a clock that is behind its start
	_, err = rating.Rater{}.Rate("", version, rating.Session{Start: rateStart, Now: rateMinute(-1)})
	assert.ErrorContains(t, err, "before its start")
}

// A tariff version without elements has no price: the session costs nothing, and its
// charging periods still say what happened.
func TestRateWithoutPrices(t *testing.T) {
	session := endedSession(120, 11000, sampleAt(60, 11000))
	session.IdleStartedAt = ptr(rateMinute(60))
	result, err := rating.Rater{}.Rate("", &store.TariffVersion{}, session)
	require.NoError(t, err)
	assert.Equal(t, money.Price{}, result.Breakdown.Total)
	assert.Equal(t, rating.EnergyBreakdown{Wh: 11000}, result.Breakdown.Energy)
	assert.Equal(t, []rating.ChargingPeriod{
		{StartDateTime: rateStart, Dimensions: []rating.Dimension{{Type: "ENERGY", Volume: 11}, {Type: "TIME", Volume: 1}}},
		{StartDateTime: rateMinute(60), Dimensions: []rating.Dimension{{Type: "PARKING_TIME", Volume: 1}}},
	}, result.ChargingPeriods)
	assert.Equal(t, int64(11000), result.TotalEnergyWh)
}

// Each part of a price is rounded on its own, to the decimals and with the mode of the
// Rater. A Rater that sets no rounding rounds to 2 decimals, a half away from zero.
func TestRateRounding(t *testing.T) {
	vat := ptr(money.Percent(2100))
	// 22.5 kWh at 0.35: 7.875, and 9.52875 with VAT
	version := tariffOf(elementOf(nil, store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, "0.35"), Vat: vat, StepSize: 1}))
	session := endedSession(60, 22500)
	for _, tc := range []struct {
		rounding   rating.Rounding
		excl, incl string
	}{
		{rating.Rounding{}, "7.88", "9.53"},
		{rating.DefaultRounding(), "7.88", "9.53"},
		{rating.Rounding{Scale: 2, Mode: money.RoundHalfEven}, "7.88", "9.53"},
		{rating.Rounding{Scale: 0, Mode: money.RoundHalfUp}, "8", "10"},
		{rating.Rounding{Scale: 1, Mode: money.RoundHalfUp}, "7.9", "9.5"},
		{rating.Rounding{Scale: 3, Mode: money.RoundHalfUp}, "7.875", "9.529"},
		{rating.Rounding{Scale: 4, Mode: money.RoundHalfUp}, "7.875", "9.5288"},
		{rating.Rounding{Scale: 4, Mode: money.RoundHalfEven}, "7.875", "9.5288"},
		{rating.Rounding{Scale: 0, Mode: money.RoundHalfEven}, "8", "10"},
	} {
		result, err := rating.Rater{Rounding: tc.rounding}.Rate("", version, session)
		require.NoError(t, err)
		assert.Equal(t, price(t, tc.excl, tc.incl), result.Breakdown.Total, "%+v", tc.rounding)
	}

	// a half: 2.5 kWh at 0.05 are 0.125
	half := tariffOf(elementOf(nil, energyAt(t, "0.05")))
	up, err := rating.Rater{}.Rate("", half, endedSession(60, 2500))
	require.NoError(t, err)
	assert.Equal(t, price(t, "0.13", "0.13"), up.Breakdown.Total)
	even, err := rating.Rater{Rounding: rating.Rounding{Scale: 2, Mode: money.RoundHalfEven}}.Rate("", half, endedSession(60, 2500))
	require.NoError(t, err)
	assert.Equal(t, price(t, "0.12", "0.12"), even.Breakdown.Total)

	assert.Equal(t, rating.Rounding{Scale: 2, Mode: money.RoundHalfUp}, rating.DefaultRounding())
}

// The total is the sum of the rounded parts, not the rounded sum of the parts.
func TestRateTotalIsTheSumOfTheRoundedParts(t *testing.T) {
	// a fee of 0.004, 1 kWh at 0.004, an hour of charging at 0.004: each part rounds to 0
	version := tariffOf(elementOf(nil,
		store.PriceComponent{Type: store.PriceComponentFlat, Price: amount(t, "0.004")},
		energyAt(t, "0.004"),
		store.PriceComponent{Type: store.PriceComponentTime, Price: amount(t, "0.004"), StepSize: 1},
	))
	result := rate(t, version, endedSession(60, 1000))
	assert.Equal(t, money.Price{}, result.Breakdown.Total)

	// and with 0.005 each part rounds to 0.01
	version = tariffOf(elementOf(nil,
		store.PriceComponent{Type: store.PriceComponentFlat, Price: amount(t, "0.005")},
		energyAt(t, "0.005"),
		store.PriceComponent{Type: store.PriceComponentTime, Price: amount(t, "0.005"), StepSize: 1},
	))
	result = rate(t, version, endedSession(60, 1000))
	assert.Equal(t, price(t, "0.03", "0.03"), result.Breakdown.Total)
}

// The minimum and the maximum price of a tariff hold the total; a total at one of them is
// left as it is.
func TestRateMinAndMaxPrice(t *testing.T) {
	version := tariffOf(elementOf(nil, energyAt(t, "0.25")))
	version.MinPrice = ptr(price(t, "1.00", "1.21"))
	version.MaxPrice = ptr(price(t, "5.00", "6.05"))

	at := func(wh float64) rating.PriceBreakdown { return rate(t, version, endedSession(60, wh)).Breakdown }

	exactlyMin := at(4000)
	assert.Equal(t, price(t, "1.00", "1.00"), exactlyMin.Total)
	assert.Nil(t, exactlyMin.Adjustment)
	assert.False(t, exactlyMin.MinPriceApplied)

	below := at(3960)
	assert.Equal(t, price(t, "1.00", "1.21"), below.Total)
	assert.Equal(t, ptr(price(t, "0.01", "0.22")), below.Adjustment)
	assert.True(t, below.MinPriceApplied)
	assert.False(t, below.MaxPriceApplied)
	assert.Equal(t, price(t, "0.99", "0.99"), below.Energy.Cost, "the parts stay what they are")

	exactlyMax := at(20000)
	assert.Equal(t, price(t, "5.00", "5.00"), exactlyMax.Total)
	assert.Nil(t, exactlyMax.Adjustment)

	above := at(20040)
	assert.Equal(t, price(t, "5.00", "6.05"), above.Total)
	assert.Equal(t, ptr(price(t, "-0.01", "1.04")), above.Adjustment)
	assert.True(t, above.MaxPriceApplied)
	assert.False(t, above.MinPriceApplied)

	// a session that runs is held to them too
	running := rate(t, version, rating.Session{Start: rateStart, Now: rateMinute(5)})
	assert.Equal(t, price(t, "1.00", "1.21"), running.Breakdown.Total)
	assert.False(t, running.Final)
}

// The VAT of a term is that of the price component that the term was charged with.
func TestRateVatPerComponent(t *testing.T) {
	// 5 kWh at 0.20 with 10% VAT, then 5 kWh at 0.40 with 21% VAT
	version := tariffOf(
		elementOf(&store.TariffRestrictions{MaxKwh: ptr(5.0)},
			store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, "0.20"), Vat: ptr(money.Percent(1000)), StepSize: 1}),
		elementOf(nil,
			store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, "0.40"), Vat: ptr(money.Percent(2100)), StepSize: 1}),
	)
	result := rate(t, version, endedSession(60, 10000))
	// 1.00 + 2.00, and 1.10 + 2.42
	assert.Equal(t, price(t, "3.00", "3.52"), result.Breakdown.Energy.Cost)

	// a VAT of 0% is a VAT
	version.Elements[0].PriceComponents[0].Vat = ptr(money.Percent(0))
	result = rate(t, version, endedSession(60, 10000))
	assert.Equal(t, price(t, "3.00", "3.42"), result.Breakdown.Energy.Cost)
}

// The step of a dimension is that of the component that was charged last, and what the
// rounding adds is charged at the price and with the VAT of that component.
func TestRateEnergySteps(t *testing.T) {
	// 0.20 per kWh in steps of 1 Wh up to 5 kWh, then 0.40 per kWh in steps of 1 kWh
	version := tariffOf(
		elementOf(&store.TariffRestrictions{MaxKwh: ptr(5.0)}, energyAt(t, "0.20")),
		elementOf(nil, store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, "0.40"), Vat: ptr(money.Percent(1000)), StepSize: 1000}),
	)
	result := rate(t, version, endedSession(60, 7400))
	// 5 kWh at 0.20, 2.4 kWh at 0.40, and 0.6 kWh more at 0.40: 1.00 + 0.96 + 0.24; the
	// part at 0.40 has 10% VAT: 1.00 + 1.056 + 0.264
	assert.Equal(t, rating.EnergyBreakdown{Wh: 7400, BilledWh: 8000, Cost: price(t, "2.20", "2.32")}, result.Breakdown.Energy)

	// a session that stays in the first element is not rounded to the step of the second
	result = rate(t, version, endedSession(60, 4400))
	assert.Equal(t, rating.EnergyBreakdown{Wh: 4400, BilledWh: 4400, Cost: price(t, "0.88", "0.88")}, result.Breakdown.Energy)

	// and neither is a session that took nothing while the second element applied:
	// 4.4 kWh in the first half hour, at 0.20 in steps of 1 Wh, and nothing after it
	byDuration := tariffOf(
		elementOf(&store.TariffRestrictions{MaxDuration: ptr(int32(1800))}, energyAt(t, "0.20")),
		elementOf(nil, store.PriceComponent{Type: store.PriceComponentEnergy, Price: amount(t, "0.40"), StepSize: 1000}),
	)
	result = rate(t, byDuration, endedSession(60, 4400, sampleAt(30, 4400)))
	assert.Equal(t, rating.EnergyBreakdown{Wh: 4400, BilledWh: 4400, Cost: price(t, "0.88", "0.88")}, result.Breakdown.Energy)
}

// The time that is charged can end between two seconds, where the session reaches an
// amount of energy: the part of a second counts as a second.
func TestRateTimeFromAnAmountOfEnergy(t *testing.T) {
	// charging time at 3.60 per hour, per second, from 5 kWh on. 3 kWh after 15 minutes
	// and 10 kWh after an hour: 5 kWh are reached 771 3/7 seconds after the first sample
	version := tariffOf(elementOf(&store.TariffRestrictions{MinKwh: ptr(5.0)},
		store.PriceComponent{Type: store.PriceComponentTime, Price: amount(t, "3.60"), StepSize: 1}))
	result := rate(t, version, endedSession(60, 10000, sampleAt(15, 3000)))
	// 1928 4/7 seconds are charged as 1929
	assert.Equal(t, rating.TimeBreakdown{
		TotalS: 3600, BillableS: 1929, StepSizeS: 1, Steps: 1929, Minutes: 60, Cost: price(t, "1.93", "1.93"),
	}, result.Breakdown.Time)
}

// The price info map of example A.
func TestPriceInfo(t *testing.T) {
	fixture := loadRatingFixtures(t)["A-energy-and-parking.json"]
	result, err := rating.Rater{}.Rate(fixtureTariffId, &fixture.Tariff, fixture.Session)
	require.NoError(t, err)

	info := result.Breakdown.PriceInfo()
	assert.Equal(t, map[string]any{
		"startPrice":                 money.Amount(0),
		"energyPrice":                amount(t, "3.30"),
		"chargingTimePrice":          money.Amount(0),
		"parkingTimePrice":           amount(t, "2.50"),
		"totalChargingTimeInMinutes": 60.0,
		"totalChargingTimeSteps":     int64(0),
		"totalParkingTimeInMinutes":  100.0,
		"totalParkingTimeSteps":      int64(5),
		"totalPrice":                 amount(t, "5.80"),
	}, info)

	document, err := json.Marshal(info)
	require.NoError(t, err)
	assert.JSONEq(t, `{"startPrice":0,"energyPrice":3.3,"chargingTimePrice":0,"parkingTimePrice":2.5,
		"totalChargingTimeInMinutes":60,"totalChargingTimeSteps":0,"totalParkingTimeInMinutes":100,
		"totalParkingTimeSteps":5,"totalPrice":5.8}`, string(document))

	// the amounts are without VAT, also for a total that the minimum price raised
	minimum := loadRatingFixtures(t)["F-min-price.json"]
	result, err = rating.Rater{}.Rate(fixtureTariffId, &minimum.Tariff, minimum.Session)
	require.NoError(t, err)
	assert.Equal(t, amount(t, "1.00"), result.Breakdown.PriceInfo()["totalPrice"])
	assert.Equal(t, amount(t, "0.50"), result.Breakdown.PriceInfo()["energyPrice"])
}

// A session is written as JSON and read back as it was, with its time zone by name.
func TestSessionJSON(t *testing.T) {
	for file, fixture := range loadRatingFixtures(t) {
		document, err := json.Marshal(fixture.Session)
		require.NoError(t, err, file)
		var back rating.Session
		require.NoError(t, json.Unmarshal(document, &back), file)
		assert.Equal(t, fixture.Session, back, file)
	}

	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	session := rating.Session{
		Start:         rateStart,
		Now:           rateMinute(45),
		Location:      madrid,
		Samples:       []rating.Sample{{At: rateMinute(30), EnergyWh: 5500.5, CurrentA: ptr(16.0)}},
		IdleStartedAt: ptr(rateMinute(40)),
	}
	document, err := json.Marshal(session)
	require.NoError(t, err)
	assert.JSONEq(t, `{"start":"2026-10-01T10:00:00Z","now":"2026-10-01T10:45:00Z","time_zone":"Europe/Madrid",
		"samples":[{"at":"2026-10-01T10:30:00Z","energy_wh":5500.5,"current_a":16}],
		"idle_started_at":"2026-10-01T10:40:00Z"}`, string(document))

	// UTC and no time zone are the same, and a session without samples has an empty list
	document, err = json.Marshal(rating.Session{Start: rateStart, Stop: ptr(rateMinute(5)), Location: time.UTC, TotalEnergyWh: ptr(12.0)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"start":"2026-10-01T10:00:00Z","stop":"2026-10-01T10:05:00Z","samples":[],"total_energy_wh":12}`, string(document))

	var unknown rating.Session
	err = json.Unmarshal([]byte(`{"start":"2026-10-01T10:00:00Z","time_zone":"Mars/Olympus_Mons","samples":[]}`), &unknown)
	assert.ErrorContains(t, err, "Mars/Olympus_Mons")
	assert.Error(t, json.Unmarshal([]byte(`{"start":"not a time"}`), &unknown))
}

// Rate changes neither the tariff version nor the session that it is given.
func TestRateLeavesItsInput(t *testing.T) {
	fixture := loadRatingFixtures(t)["B-parking-window.json"]
	versionBefore, err := json.Marshal(fixture.Tariff)
	require.NoError(t, err)
	// the samples in another order than that of time
	session := fixture.Session
	session.Samples = []rating.Sample{session.Samples[2], session.Samples[0], session.Samples[3], session.Samples[1]}
	sessionBefore, err := json.Marshal(session)
	require.NoError(t, err)

	result, err := rating.Rater{}.Rate(fixtureTariffId, &fixture.Tariff, session)
	require.NoError(t, err)
	assert.JSONEq(t, string(fixture.Expected), resultJSON(t, result))

	versionAfter, err := json.Marshal(fixture.Tariff)
	require.NoError(t, err)
	sessionAfter, err := json.Marshal(session)
	require.NoError(t, err)
	assert.Equal(t, string(versionBefore), string(versionAfter))
	assert.Equal(t, string(sessionBefore), string(sessionAfter))
}
