// SPDX-License-Identifier: Apache-2.0

package tariffs_test

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

// simpleFixture is a file of testdata/simple: a tariff in the simple form and the tariff
// version that it converts to.
type simpleFixture struct {
	Id          string               `json:"id"`
	Description string               `json:"description"`
	Simple      tariffs.SimpleTariff `json:"simple"`
	Expected    json.RawMessage      `json:"expected"`
}

func loadSimpleFixtures(t *testing.T) map[string]simpleFixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "simple", "*.json"))
	require.NoError(t, err)
	fixtures := map[string]simpleFixture{}
	for _, file := range files {
		document, err := os.ReadFile(file) // #nosec G304 -- a file of the test data
		require.NoError(t, err)
		var fixture simpleFixture
		require.NoError(t, json.Unmarshal(document, &fixture), file)
		fixtures[filepath.Base(file)] = fixture
	}
	return fixtures
}

// Every shape of the simple tariff form converts to the tariff version of its fixture,
// which is valid.
func TestFromSimpleFixtures(t *testing.T) {
	fixtures := loadSimpleFixtures(t)
	ids := map[string]bool{}
	for file, fixture := range fixtures {
		t.Run(file, func(t *testing.T) {
			ids[fixture.Id] = true
			assert.NotEmpty(t, fixture.Description)

			v, err := tariffs.FromSimple(fixture.Simple)
			require.NoError(t, err)
			document, err := json.Marshal(v)
			require.NoError(t, err)
			assert.JSONEq(t, string(fixture.Expected), string(document))

			// the expected version is a tariff version as it is, and valid
			expected := &store.TariffVersion{}
			require.NoError(t, json.Unmarshal(fixture.Expected, expected))
			assert.Equal(t, expected, v)
			assert.Empty(t, tariffs.Validate(expected))
			assert.Equal(t, expected, v, "a valid version is not changed by its validation")
		})
	}
	for _, id := range []string{"S1", "S2", "S3", "S4", "S5", "S5b", "S6", "S8"} {
		assert.True(t, ids[id], "no fixture for %s", id)
	}
	assert.Len(t, fixtures, 9)
}

func TestFromSimple(t *testing.T) {
	element := func(t *testing.T, v *store.TariffVersion, i int) store.TariffElement {
		t.Helper()
		require.Greater(t, len(v.Elements), i)
		return v.Elements[i]
	}
	vat21 := ptr(money.Percent(2100))

	t.Run("every part of the form", func(t *testing.T) {
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{
			Currency:                  "EUR",
			TariffType:                "REGULAR",
			StartTariff:               ptr(0.5),
			Price:                     ptr(0.345),
			Vat:                       ptr(21.0),
			IsVatRelevant:             ptr(true),
			TimeTariff:                ptr(0.1),
			TimeStepSizeMinutes:       ptr(7),
			TimeGracePeriodMinutes:    ptr(20),
			ParkingTariff:             ptr(0.5),
			ParkingStepSizeMinutes:    ptr(15),
			ParkingGracePeriodMinutes: ptr(30),
		})
		require.NoError(t, err)
		assert.Equal(t, "EUR", v.Currency)
		assert.Equal(t, ptr(store.TariffTypeRegular), v.Type)
		require.Len(t, v.Elements, 1)
		assert.Nil(t, v.Elements[0].Restrictions)
		assert.Equal(t, []store.PriceComponent{
			{Type: store.PriceComponentFlat, Price: 5000, Vat: vat21, StepSize: 1},
			{Type: store.PriceComponentEnergy, Price: 3450, Vat: vat21, StepSize: 1},
			{Type: store.PriceComponentTime, Price: 8571, Vat: vat21, StepSize: 420, GracePeriodS: ptr(int32(1200)), PricePerStep: ptr(money.Amount(1000))},
			{Type: store.PriceComponentParkingTime, Price: 20000, Vat: vat21, StepSize: 900, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(5000))},
		}, v.Elements[0].PriceComponents)
		assert.Empty(t, tariffs.Validate(v))
	})

	t.Run("the type", func(t *testing.T) {
		for tariffType, want := range map[string]store.TariffType{
			"AD_HOC_PAYMENT": store.TariffTypeAdHocPayment,
			"REGULAR":        store.TariffTypeRegular,
			"":               store.TariffTypeRegular,
			"SOMETHING":      store.TariffTypeRegular,
		} {
			v, err := tariffs.FromSimple(tariffs.SimpleTariff{Currency: "EUR", TariffType: tariffType, Price: ptr(0.3)})
			require.NoError(t, err, tariffType)
			assert.Equal(t, &want, v.Type, tariffType)
		}
	})

	t.Run("VAT only when it is relevant and set", func(t *testing.T) {
		for name, tc := range map[string]struct {
			vat      *float64
			relevant *bool
			want     *money.Percent
		}{
			"relevant":              {ptr(21.0), ptr(true), ptr(money.Percent(2100))},
			"relevant, with half":   {ptr(5.5), ptr(true), ptr(money.Percent(550))},
			"relevant, of 0":        {ptr(0.0), ptr(true), ptr(money.Percent(0))},
			"not relevant":          {ptr(21.0), ptr(false), nil},
			"relevance not said":    {ptr(21.0), nil, nil},
			"relevant, but not set": {nil, ptr(true), nil},
		} {
			v, err := tariffs.FromSimple(tariffs.SimpleTariff{
				Currency: "EUR", StartTariff: ptr(1.0), Price: ptr(0.3), Vat: tc.vat, IsVatRelevant: tc.relevant,
				TimeTariff: ptr(0.1), ParkingTariff: ptr(0.2),
				ParkingTariffRestrictions: []tariffs.SimpleRestriction{{DayOfWeek: ptr(1), StartTime: "08:00", EndTime: "20:00"}},
			})
			require.NoError(t, err, name)
			components := 0
			for _, e := range v.Elements {
				for _, c := range e.PriceComponents {
					assert.Equal(t, tc.want, c.Vat, "%s: %s", name, c.Type)
					components++
				}
			}
			assert.Equal(t, 4, components, name)
		}
	})

	t.Run("prices that are not above 0", func(t *testing.T) {
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{
			Currency: "EUR", StartTariff: ptr(0.0), Price: ptr(0.0), TimeTariff: ptr(0.0), ParkingTariff: ptr(0.0),
			ParkingTariffRestrictions: []tariffs.SimpleRestriction{{DayOfWeek: ptr(1), StartTime: "08:00", EndTime: "20:00"}},
		})
		require.NoError(t, err)
		require.Len(t, v.Elements, 1, "no parking price, no parking windows")
		assert.Equal(t, []store.PriceComponent{{Type: store.PriceComponentEnergy, StepSize: 1}}, v.Elements[0].PriceComponents,
			"only the price per kWh stays when it is 0")
	})

	t.Run("step size and grace period", func(t *testing.T) {
		for name, tc := range map[string]struct {
			step, grace *int
			wantStep    int32
			wantGrace   *int32
			wantPrice   money.Amount
		}{
			"neither":           {nil, nil, 60, nil, 60000},
			"a step of 0":       {ptr(0), ptr(0), 60, nil, 60000},
			"a step of 1":       {ptr(1), ptr(1), 60, ptr(int32(60)), 60000},
			"a negative grace":  {ptr(30), ptr(-5), 1800, nil, 2000},
			"an hour and a day": {ptr(60), ptr(1440), 3600, ptr(int32(86400)), 1000},
		} {
			v, err := tariffs.FromSimple(tariffs.SimpleTariff{
				Currency: "EUR", TimeTariff: ptr(0.1), TimeStepSizeMinutes: tc.step, TimeGracePeriodMinutes: tc.grace,
				ParkingTariff: ptr(0.1), ParkingStepSizeMinutes: tc.step, ParkingGracePeriodMinutes: tc.grace,
			})
			require.NoError(t, err, name)
			require.Len(t, element(t, v, 0).PriceComponents, 2, name)
			for _, c := range v.Elements[0].PriceComponents {
				assert.Equal(t, tc.wantStep, c.StepSize, "%s: %s", name, c.Type)
				assert.Equal(t, tc.wantGrace, c.GracePeriodS, "%s: %s", name, c.Type)
				assert.Equal(t, ptr(money.Amount(1000)), c.PricePerStep, "%s: %s", name, c.Type)
				assert.Equal(t, tc.wantPrice, c.Price, "%s: %s", name, c.Type)
			}
		}
	})

	t.Run("parking windows", func(t *testing.T) {
		window := func(day int, start, end string) tariffs.SimpleRestriction {
			return tariffs.SimpleRestriction{DayOfWeek: ptr(day), StartTime: start, EndTime: end}
		}
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{
			Currency: "EUR", Price: ptr(0.3), Vat: ptr(21.0), IsVatRelevant: ptr(true),
			ParkingTariff: ptr(0.2), ParkingStepSizeMinutes: ptr(10), ParkingGracePeriodMinutes: ptr(15),
			ParkingTariffRestrictions: []tariffs.SimpleRestriction{
				window(6, "09:00", "14:00"),
				window(1, "08:00", "20:00"),
				window(0, "09:00", "14:00"),
				{StartTime: "08:00", EndTime: "20:00"},  // no day
				{DayOfWeek: ptr(2), EndTime: "20:00"},   // no start
				{DayOfWeek: ptr(2), StartTime: "08:00"}, // no end
				window(7, "08:00", "20:00"),             // no day of the week
				window(-1, "08:00", "20:00"),            //
				window(5, "08:00", "20:00"),
				window(1, "08:00", "20:00"), // named twice
				window(3, "22:00", "06:00"),
			},
		})
		require.NoError(t, err)
		require.Len(t, v.Elements, 4)
		assert.Equal(t, []store.PriceComponent{{Type: store.PriceComponentEnergy, Price: 3000, Vat: vat21, StepSize: 1}}, v.Elements[0].PriceComponents)
		assert.Nil(t, v.Elements[0].Restrictions)

		parking := store.PriceComponent{Type: store.PriceComponentParkingTime, Price: 12000, Vat: vat21, StepSize: 600,
			GracePeriodS: ptr(int32(900)), PricePerStep: ptr(money.Amount(2000))}
		for i, want := range []*store.TariffRestrictions{
			{StartTime: ptr("09:00"), EndTime: ptr("14:00"), DayOfWeek: []store.DayOfWeek{store.Saturday, store.Sunday}},
			{StartTime: ptr("08:00"), EndTime: ptr("20:00"), DayOfWeek: []store.DayOfWeek{store.Monday, store.Friday}},
			{StartTime: ptr("22:00"), EndTime: ptr("06:00"), DayOfWeek: []store.DayOfWeek{store.Wednesday}},
		} {
			got := v.Elements[i+1]
			assert.Equal(t, []store.PriceComponent{parking}, got.PriceComponents, "element %d", i+1)
			assert.Equal(t, want, got.Restrictions, "element %d", i+1)
		}

		// the elements do not share what they point to
		*v.Elements[1].PriceComponents[0].GracePeriodS = 1
		*v.Elements[1].PriceComponents[0].PricePerStep = 1
		*v.Elements[1].PriceComponents[0].Vat = 1
		assert.Equal(t, parking, v.Elements[2].PriceComponents[0])
		assert.Equal(t, vat21, v.Elements[0].PriceComponents[0].Vat)
	})

	t.Run("windows of which none can be used", func(t *testing.T) {
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{
			Currency: "EUR", Price: ptr(0.3), ParkingTariff: ptr(0.2),
			ParkingTariffRestrictions: []tariffs.SimpleRestriction{{StartTime: "08:00", EndTime: "20:00"}, {DayOfWeek: ptr(9)}},
		})
		require.NoError(t, err)
		require.Len(t, v.Elements, 1, "parking is charged at any time")
		require.Len(t, v.Elements[0].PriceComponents, 2)
		assert.Equal(t, store.PriceComponentParkingTime, v.Elements[0].PriceComponents[1].Type)
	})

	t.Run("only parking in windows", func(t *testing.T) {
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{
			Currency: "EUR", ParkingTariff: ptr(0.2),
			ParkingTariffRestrictions: []tariffs.SimpleRestriction{{DayOfWeek: ptr(0), StartTime: "00:00", EndTime: "00:00"}},
		})
		require.NoError(t, err)
		require.Len(t, v.Elements, 1, "no element without price components")
		assert.Equal(t, &store.TariffRestrictions{StartTime: ptr("00:00"), EndTime: ptr("00:00"), DayOfWeek: []store.DayOfWeek{store.Sunday}},
			v.Elements[0].Restrictions)
	})

	t.Run("floats become amounts by their decimal text", func(t *testing.T) {
		for price, want := range map[float64]money.Amount{
			0.1 + 0.2:  3000, // 0.30000000000000004
			0.29:       2900,
			1.005:      10050,
			0.00005:    1, // half up
			0.00004999: 0,
			0.12345:    1235,
			123456.789: 1234567890,
		} {
			v, err := tariffs.FromSimple(tariffs.SimpleTariff{Currency: "EUR", Price: ptr(price)})
			require.NoError(t, err)
			assert.Equal(t, want, v.Elements[0].PriceComponents[0].Price, "%v", price)
		}
		v, err := tariffs.FromSimple(tariffs.SimpleTariff{Currency: "EUR", Price: ptr(0.3), Vat: ptr(7.7 + 0.005), IsVatRelevant: ptr(true)})
		require.NoError(t, err)
		assert.Equal(t, ptr(money.Percent(771)), v.Elements[0].PriceComponents[0].Vat, "a VAT percentage has 2 decimals, rounded half up")
	})

	t.Run("forms that give no valid tariff version", func(t *testing.T) {
		for name, form := range map[string]tariffs.SimpleTariff{
			"nothing":                           {},
			"no price":                          {Currency: "EUR"},
			"no currency":                       {Price: ptr(0.3)},
			"a currency that is not":            {Currency: "euro", Price: ptr(0.3)},
			"a negative price":                  {Currency: "EUR", Price: ptr(-0.3)},
			"a price that is no number":         {Currency: "EUR", Price: ptr(math.NaN())},
			"a start fee without an end":        {Currency: "EUR", StartTariff: ptr(math.Inf(1))},
			"a VAT that is no number":           {Currency: "EUR", Price: ptr(0.3), Vat: ptr(math.NaN()), IsVatRelevant: ptr(true)},
			"a VAT above 100":                   {Currency: "EUR", Price: ptr(0.3), Vat: ptr(121.0), IsVatRelevant: ptr(true)},
			"a negative step":                   {Currency: "EUR", TimeTariff: ptr(0.1), TimeStepSizeMinutes: ptr(-5)},
			"a step beyond what can be counted": {Currency: "EUR", ParkingTariff: ptr(0.1), ParkingStepSizeMinutes: ptr(math.MaxInt32)},
			"a grace period beyond it":          {Currency: "EUR", ParkingTariff: ptr(0.1), ParkingGracePeriodMinutes: ptr(math.MaxInt32)},
			"a window with a time that is not": {Currency: "EUR", ParkingTariff: ptr(0.1),
				ParkingTariffRestrictions: []tariffs.SimpleRestriction{{DayOfWeek: ptr(1), StartTime: "8", EndTime: "20:00"}}},
		} {
			v, err := tariffs.FromSimple(form)
			assert.Error(t, err, name)
			assert.Nil(t, v, name)
		}
	})

	t.Run("the JSON of the form", func(t *testing.T) {
		var form tariffs.SimpleTariff
		require.NoError(t, json.Unmarshal([]byte(`{
			"currency": "EUR", "tariff_type": "AD_HOC_PAYMENT", "start_tariff": 1, "price": 0.45, "vat": 21, "is_vat_relevant": true,
			"time_tariff": 0.1, "time_step_size_in_minutes": 5, "time_grace_period_in_minutes": 30,
			"parking_tariff": 0.5, "parking_step_size_in_minutes": 15, "parking_grace_period_in_minutes": 30,
			"parking_tariff_restrictions": [{"dayOfWeek": 0, "startTime": "08:00", "endTime": "20:00"}, {"startTime": "08:00", "endTime": "20:00"}]
		}`), &form))
		assert.Equal(t, tariffs.SimpleTariff{
			Currency: "EUR", TariffType: "AD_HOC_PAYMENT", StartTariff: ptr(1.0), Price: ptr(0.45), Vat: ptr(21.0), IsVatRelevant: ptr(true),
			TimeTariff: ptr(0.1), TimeStepSizeMinutes: ptr(5), TimeGracePeriodMinutes: ptr(30),
			ParkingTariff: ptr(0.5), ParkingStepSizeMinutes: ptr(15), ParkingGracePeriodMinutes: ptr(30),
			ParkingTariffRestrictions: []tariffs.SimpleRestriction{
				{DayOfWeek: ptr(0), StartTime: "08:00", EndTime: "20:00"},
				{StartTime: "08:00", EndTime: "20:00"},
			},
		}, form)
	})
}
