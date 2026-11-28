// SPDX-License-Identifier: Apache-2.0

package tariffs_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

func ptr[T any](v T) *T {
	return &v
}

// validVersion is a tariff version that breaks no rule, with every kind of field set:
// element 0 has a FLAT, an ENERGY and a TIME component, element 1 a PARKING_TIME
// component with restrictions.
func validVersion() *store.TariffVersion {
	vat := ptr(money.Percent(2100))
	return &store.TariffVersion{
		Currency:      "EUR",
		Type:          ptr(store.TariffTypeRegular),
		AltText:       []store.DisplayText{{Language: "en", Text: "0.30 per kWh"}},
		AltUrl:        ptr("https://example.com/tariffs/standard"),
		MinPrice:      &money.Price{ExclVat: 5000, InclVat: 6050},
		MaxPrice:      &money.Price{ExclVat: 500000, InclVat: 605000},
		StartDateTime: ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		EndDateTime:   ptr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
		Elements: []store.TariffElement{
			{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentFlat, Price: 10000, Vat: vat},
				{Type: store.PriceComponentEnergy, Price: 3000, Vat: vat, StepSize: 1},
				{Type: store.PriceComponentTime, Vat: vat, StepSize: 300, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(1000))},
			}},
			{
				PriceComponents: []store.PriceComponent{
					{Type: store.PriceComponentParkingTime, Price: 20000, StepSize: 900, GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(5000))},
				},
				Restrictions: &store.TariffRestrictions{
					StartTime:   ptr("08:00"),
					EndTime:     ptr("20:00"),
					StartDate:   ptr("2026-01-01"),
					EndDate:     ptr("2026-12-31"),
					MinKwh:      ptr(1.5),
					MaxKwh:      ptr(80.0),
					MinCurrent:  ptr(6.0),
					MaxCurrent:  ptr(32.0),
					MinPower:    ptr(3.7),
					MaxPower:    ptr(22.0),
					MinDuration: ptr(int32(600)),
					MaxDuration: ptr(int32(86400)),
					DayOfWeek:   []store.DayOfWeek{store.Monday, store.Friday},
					Reservation: ptr(store.ReservationRestrictionReservationExpires),
				},
			},
		},
		IdleDetection: &store.IdleDetection{ChargingPowerShare: ptr(0.01), MinChargingPowerW: ptr(200.0)},
	}
}

func fields(invalid []tariffs.ValidationError) []string {
	names := make([]string, len(invalid))
	for i, e := range invalid {
		names[i] = e.Field
	}
	return names
}

func TestValidateAcceptsAValidVersion(t *testing.T) {
	v := validVersion()
	assert.Empty(t, tariffs.Validate(v))
	// the price per hour of the TIME component follows from its price per step
	assert.Equal(t, money.Amount(12000), v.Elements[0].PriceComponents[2].Price)

	// a version that was validated is not changed when it is validated again
	normalized, err := json.Marshal(v)
	require.NoError(t, err)
	assert.Empty(t, tariffs.Validate(v))
	again, err := json.Marshal(v)
	require.NoError(t, err)
	assert.JSONEq(t, string(normalized), string(again))

	minimal := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, StepSize: 1},
	}}}}
	assert.Empty(t, tariffs.Validate(minimal), "a free tariff without anything optional")
}

func TestValidate(t *testing.T) {
	component := func(v *store.TariffVersion, element, i int) *store.PriceComponent {
		return &v.Elements[element].PriceComponents[i]
	}
	restrictions := func(v *store.TariffVersion) *store.TariffRestrictions {
		return v.Elements[1].Restrictions
	}
	const (
		flat    = "elements[0].price_components[0]."
		energy  = "elements[0].price_components[1]."
		charged = "elements[0].price_components[2]."
		parking = "elements[1].price_components[0]."
		when    = "elements[1].restrictions."
	)

	for name, tc := range map[string]struct {
		change func(v *store.TariffVersion)
		want   []string
	}{
		// 1. currency, elements and price components
		"currency in small letters": {func(v *store.TariffVersion) { v.Currency = "eur" }, []string{"currency"}},
		"currency of 2 letters":     {func(v *store.TariffVersion) { v.Currency = "EU" }, []string{"currency"}},
		"currency of 4 letters":     {func(v *store.TariffVersion) { v.Currency = "EURO" }, []string{"currency"}},
		"no currency":               {func(v *store.TariffVersion) { v.Currency = "" }, []string{"currency"}},
		"type that is not known":    {func(v *store.TariffVersion) { v.Type = ptr(store.TariffType("PROFILE_SLOW")) }, []string{"type"}},
		"no type":                   {func(v *store.TariffVersion) { v.Type = nil }, nil},
		"no elements":               {func(v *store.TariffVersion) { v.Elements = nil }, []string{"elements"}},
		"element without price components": {func(v *store.TariffVersion) { v.Elements[1].PriceComponents = nil },
			[]string{"elements[1].price_components"}},
		"two components of one type in an element": {func(v *store.TariffVersion) {
			v.Elements[0].PriceComponents = append(v.Elements[0].PriceComponents, store.PriceComponent{Type: store.PriceComponentEnergy, Price: 2500, StepSize: 1})
		}, []string{"elements[0].price_components[3].type"}},
		"the same type in two elements": {func(v *store.TariffVersion) {
			v.Elements[1].PriceComponents = append(v.Elements[1].PriceComponents, store.PriceComponent{Type: store.PriceComponentEnergy, Price: 2500, StepSize: 1})
		}, nil},
		"component type that is not known": {func(v *store.TariffVersion) { component(v, 0, 1).Type = "ENERGY_EXPORT" }, []string{energy + "type"}},

		// 2. price, VAT and step size
		"negative price":       {func(v *store.TariffVersion) { component(v, 0, 1).Price = -1 }, []string{energy + "price"}},
		"price of 0":           {func(v *store.TariffVersion) { component(v, 0, 1).Price = 0 }, nil},
		"negative VAT":         {func(v *store.TariffVersion) { component(v, 0, 1).Vat = ptr(money.Percent(-1)) }, []string{energy + "vat"}},
		"VAT above 100":        {func(v *store.TariffVersion) { component(v, 0, 1).Vat = ptr(money.Percent(10001)) }, []string{energy + "vat"}},
		"VAT of 0":             {func(v *store.TariffVersion) { component(v, 0, 1).Vat = ptr(money.Percent(0)) }, nil},
		"VAT of 100":           {func(v *store.TariffVersion) { component(v, 0, 1).Vat = ptr(money.Percent(10000)) }, nil},
		"no VAT":               {func(v *store.TariffVersion) { component(v, 0, 1).Vat = nil }, nil},
		"negative energy step": {func(v *store.TariffVersion) { component(v, 0, 1).StepSize = -1 }, []string{energy + "step_size"}},
		"negative time step":   {func(v *store.TariffVersion) { component(v, 0, 2).StepSize = -300 }, []string{charged + "step_size"}},
		"negative parking step": {func(v *store.TariffVersion) { component(v, 1, 0).StepSize = -900 },
			[]string{parking + "step_size"}},
		"any step of a FLAT component": {func(v *store.TariffVersion) { component(v, 0, 0).StepSize = -5 }, nil},

		// 3. the extensions are for the time dimensions, with one grace period each
		"grace period of an ENERGY component": {func(v *store.TariffVersion) { component(v, 0, 1).GracePeriodS = ptr(int32(60)) },
			[]string{energy + "grace_period_s"}},
		"grace period of a FLAT component": {func(v *store.TariffVersion) { component(v, 0, 0).GracePeriodS = ptr(int32(0)) },
			[]string{flat + "grace_period_s"}},
		"price per step of an ENERGY component": {func(v *store.TariffVersion) { component(v, 0, 1).PricePerStep = ptr(money.Amount(100)) },
			[]string{energy + "price_per_step"}},
		"price per step of a FLAT component": {func(v *store.TariffVersion) { component(v, 0, 0).PricePerStep = ptr(money.Amount(100)) },
			[]string{flat + "price_per_step"}},
		"negative grace period": {func(v *store.TariffVersion) { component(v, 0, 2).GracePeriodS = ptr(int32(-1)) },
			[]string{charged + "grace_period_s"}},
		"two grace periods of charging time": {func(v *store.TariffVersion) {
			v.Elements[1].PriceComponents = append(v.Elements[1].PriceComponents,
				store.PriceComponent{Type: store.PriceComponentTime, Price: 12000, StepSize: 60, GracePeriodS: ptr(int32(900))})
		}, []string{"elements[1].price_components[1].grace_period_s"}},
		"charging time with and without a grace period": {func(v *store.TariffVersion) {
			v.Elements[1].PriceComponents = append(v.Elements[1].PriceComponents,
				store.PriceComponent{Type: store.PriceComponentTime, Price: 12000, StepSize: 60})
		}, []string{"elements[1].price_components[1].grace_period_s"}},
		"two grace periods of parking time": {func(v *store.TariffVersion) {
			v.Elements = append(v.Elements, store.TariffElement{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentParkingTime, Price: 20000, StepSize: 900, GracePeriodS: ptr(int32(600))},
			}})
		}, []string{"elements[2].price_components[0].grace_period_s"}},
		"the same grace period in two elements": {func(v *store.TariffVersion) {
			v.Elements = append(v.Elements, store.TariffElement{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentParkingTime, Price: 30000, StepSize: 900, GracePeriodS: ptr(int32(1800))},
			}})
		}, nil},
		"no grace period and one of 0 are the same": {func(v *store.TariffVersion) {
			component(v, 1, 0).GracePeriodS = nil
			v.Elements = append(v.Elements, store.TariffElement{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentParkingTime, Price: 30000, StepSize: 900, GracePeriodS: ptr(int32(0))},
			}})
		}, nil},
		"another grace period for parking than for charging": {func(v *store.TariffVersion) { component(v, 1, 0).GracePeriodS = ptr(int32(600)) }, nil},

		// 4. the price per hour follows from the price per step
		"price per hour that differs from the price per step": {func(v *store.TariffVersion) { component(v, 1, 0).Price = 20002 },
			[]string{parking + "price"}},
		"price per hour below the price per step": {func(v *store.TariffVersion) { component(v, 1, 0).Price = 19998 },
			[]string{parking + "price"}},
		"price per hour that is 0.0001 off": {func(v *store.TariffVersion) { component(v, 1, 0).Price = 20001 }, nil},
		"negative price per step": {func(v *store.TariffVersion) { component(v, 0, 2).PricePerStep = ptr(money.Amount(-1)) },
			[]string{charged + "price_per_step"}},
		"price per step of 0": {func(v *store.TariffVersion) { component(v, 0, 2).PricePerStep = ptr(money.Amount(0)) }, nil},

		// 5. restrictions
		"start time 24:00":          {func(v *store.TariffVersion) { restrictions(v).StartTime = ptr("24:00") }, []string{when + "start_time"}},
		"start time without a zero": {func(v *store.TariffVersion) { restrictions(v).StartTime = ptr("8:00") }, []string{when + "start_time"}},
		"end time with minute 60":   {func(v *store.TariffVersion) { restrictions(v).EndTime = ptr("20:60") }, []string{when + "end_time"}},
		"end time with seconds":     {func(v *store.TariffVersion) { restrictions(v).EndTime = ptr("20:00:00") }, []string{when + "end_time"}},
		"end of the day":            {func(v *store.TariffVersion) { restrictions(v).EndTime = ptr("00:00") }, nil},
		"last minute of the day":    {func(v *store.TariffVersion) { restrictions(v).EndTime = ptr("23:59") }, nil},
		"window past midnight": {func(v *store.TariffVersion) {
			restrictions(v).StartTime, restrictions(v).EndTime = ptr("22:00"), ptr("06:00")
		}, nil},
		"start date that does not exist":     {func(v *store.TariffVersion) { restrictions(v).StartDate = ptr("2026-02-30") }, []string{when + "start_date"}},
		"end date in another form":           {func(v *store.TariffVersion) { restrictions(v).EndDate = ptr("31-12-2026") }, []string{when + "end_date"}},
		"end date with a time":               {func(v *store.TariffVersion) { restrictions(v).EndDate = ptr("2026-12-31T00:00:00Z") }, []string{when + "end_date"}},
		"end date before the start date":     {func(v *store.TariffVersion) { restrictions(v).EndDate = ptr("2025-12-31") }, []string{when + "end_date"}},
		"end date that is the start date":    {func(v *store.TariffVersion) { restrictions(v).EndDate = ptr("2026-01-01") }, nil},
		"less energy at most than at least":  {func(v *store.TariffVersion) { restrictions(v).MaxKwh = ptr(1.0) }, []string{when + "max_kwh"}},
		"as much energy at most as at least": {func(v *store.TariffVersion) { restrictions(v).MaxKwh = ptr(1.5) }, nil},
		"less current at most than at least": {func(v *store.TariffVersion) { restrictions(v).MaxCurrent = ptr(5.9) }, []string{when + "max_current"}},
		"less power at most than at least":   {func(v *store.TariffVersion) { restrictions(v).MinPower = ptr(22.1) }, []string{when + "max_power"}},
		"shorter at most than at least":      {func(v *store.TariffVersion) { restrictions(v).MaxDuration = ptr(int32(599)) }, []string{when + "max_duration"}},
		"only a least duration":              {func(v *store.TariffVersion) { restrictions(v).MaxDuration = nil }, nil},
		"day that is not known": {func(v *store.TariffVersion) { restrictions(v).DayOfWeek = []store.DayOfWeek{store.Monday, "FUNDAY"} },
			[]string{when + "day_of_week[1]"}},
		"day in small letters": {func(v *store.TariffVersion) { restrictions(v).DayOfWeek = []store.DayOfWeek{"monday"} },
			[]string{when + "day_of_week[0]"}},
		"day that is listed twice": {func(v *store.TariffVersion) {
			restrictions(v).DayOfWeek = []store.DayOfWeek{store.Monday, store.Sunday, store.Monday}
		}, []string{when + "day_of_week[2]"}},
		"every day": {func(v *store.TariffVersion) {
			restrictions(v).DayOfWeek = []store.DayOfWeek{store.Monday, store.Tuesday, store.Wednesday, store.Thursday, store.Friday, store.Saturday, store.Sunday}
		}, nil},
		"reservation that is not known": {func(v *store.TariffVersion) { restrictions(v).Reservation = ptr(store.ReservationRestriction("MAYBE")) },
			[]string{when + "reservation"}},
		"restrictions without fields": {func(v *store.TariffVersion) { v.Elements[1].Restrictions = &store.TariffRestrictions{} }, nil},

		// 6. the limits of the price and the period of the tariff
		"negative least price":            {func(v *store.TariffVersion) { v.MinPrice.ExclVat = -1 }, []string{"min_price.excl_vat"}},
		"negative least price with VAT":   {func(v *store.TariffVersion) { v.MinPrice.InclVat = -1 }, []string{"min_price.incl_vat"}},
		"negative highest price":          {func(v *store.TariffVersion) { v.MaxPrice = &money.Price{ExclVat: -1, InclVat: -1} }, []string{"max_price.excl_vat", "max_price.incl_vat", "max_price.excl_vat"}},
		"highest price below the least":   {func(v *store.TariffVersion) { v.MaxPrice.ExclVat = 4999 }, []string{"max_price.excl_vat"}},
		"highest price that is the least": {func(v *store.TariffVersion) { v.MaxPrice.ExclVat = 5000 }, nil},
		"only a least price":              {func(v *store.TariffVersion) { v.MaxPrice = nil }, nil},
		"end that is the start":           {func(v *store.TariffVersion) { v.EndDateTime = v.StartDateTime }, []string{"end_date_time"}},
		"end before the start": {func(v *store.TariffVersion) { v.EndDateTime = ptr(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) },
			[]string{"end_date_time"}},
		"only an end": {func(v *store.TariffVersion) { v.StartDateTime = nil }, nil},

		// 7. idle detection
		"power share of 0":        {func(v *store.TariffVersion) { v.IdleDetection.ChargingPowerShare = ptr(0.0) }, []string{"idle_detection.charging_power_share"}},
		"negative power share":    {func(v *store.TariffVersion) { v.IdleDetection.ChargingPowerShare = ptr(-0.1) }, []string{"idle_detection.charging_power_share"}},
		"power share above 1":     {func(v *store.TariffVersion) { v.IdleDetection.ChargingPowerShare = ptr(1.01) }, []string{"idle_detection.charging_power_share"}},
		"power share of 1":        {func(v *store.TariffVersion) { v.IdleDetection.ChargingPowerShare = ptr(1.0) }, nil},
		"negative least power":    {func(v *store.TariffVersion) { v.IdleDetection.MinChargingPowerW = ptr(-1.0) }, []string{"idle_detection.min_charging_power_w"}},
		"least power of 0":        {func(v *store.TariffVersion) { v.IdleDetection.MinChargingPowerW = ptr(0.0) }, nil},
		"idle detection defaults": {func(v *store.TariffVersion) { v.IdleDetection = &store.IdleDetection{} }, nil},
		"no idle detection":       {func(v *store.TariffVersion) { v.IdleDetection = nil }, nil},

		// several rules, in the order of the fields
		"several fields": {func(v *store.TariffVersion) {
			v.Currency = "€"
			component(v, 0, 1).Price = -1
			component(v, 1, 0).StepSize = -1
			restrictions(v).StartTime = ptr("noon")
			v.IdleDetection.MinChargingPowerW = ptr(-1.0)
		}, []string{"currency", energy + "price", parking + "step_size", when + "start_time", "idle_detection.min_charging_power_w"}},
	} {
		t.Run(name, func(t *testing.T) {
			v := validVersion()
			tc.change(v)
			invalid := tariffs.Validate(v)
			if tc.want == nil {
				assert.Empty(t, invalid)
				return
			}
			assert.Equal(t, tc.want, fields(invalid))
			for _, e := range invalid {
				assert.NotEmpty(t, e.Reason, e.Field)
			}
		})
	}
}

func TestValidateNormalizesTheStepSize(t *testing.T) {
	v := validVersion()
	for i := range v.Elements {
		for j := range v.Elements[i].PriceComponents {
			v.Elements[i].PriceComponents[j].StepSize = 0
		}
	}
	// without a step size the price per hour follows from a step of 1 second
	v.Elements[1].PriceComponents[0].Price = 0

	assert.Empty(t, tariffs.Validate(v))
	assert.Equal(t, int32(0), v.Elements[0].PriceComponents[0].StepSize, "the step of a FLAT component is left as it is")
	assert.Equal(t, int32(1), v.Elements[0].PriceComponents[1].StepSize, "ENERGY")
	assert.Equal(t, int32(1), v.Elements[0].PriceComponents[2].StepSize, "TIME")
	assert.Equal(t, int32(1), v.Elements[1].PriceComponents[0].StepSize, "PARKING_TIME")
	assert.Equal(t, money.Amount(3600*1000), v.Elements[0].PriceComponents[2].Price)
	assert.Equal(t, money.Amount(3600*5000), v.Elements[1].PriceComponents[0].Price)
}

func TestValidateDerivesThePricePerHour(t *testing.T) {
	for name, tc := range map[string]struct {
		pricePerStep money.Amount
		stepSize     int32
		given        money.Amount
		want         string
	}{
		"0.50 per 15 minutes":               {5000, 900, 0, "2.0000"},
		"0.10 per 7 minutes":                {1000, 420, 0, "0.8571"},
		"0.10 per 5 minutes":                {1000, 300, 0, "1.2000"},
		"0.20 per 10 minutes":               {2000, 600, 0, "1.2000"},
		"0.25 per 45 minutes":               {2500, 2700, 0, "0.3333"},
		"0.50 per 45 minutes":               {5000, 2700, 0, "0.6667"},
		"0.0001 per hour":                   {1, 3600, 0, "0.0001"},
		"free steps":                        {0, 900, 0, "0.0000"},
		"the price per hour given as it is": {1000, 420, 8571, "0.8571"},
		"the price per hour given 1 above":  {1000, 420, 8572, "0.8571"},
		"the price per hour given 1 below":  {1000, 420, 8570, "0.8571"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, componentType := range []store.PriceComponentType{store.PriceComponentTime, store.PriceComponentParkingTime} {
				v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
					{Type: componentType, Price: tc.given, StepSize: tc.stepSize, PricePerStep: ptr(tc.pricePerStep)},
				}}}}
				assert.Empty(t, tariffs.Validate(v))
				assert.Equal(t, tc.want, v.Elements[0].PriceComponents[0].Price.String(), string(componentType))
				assert.Equal(t, tc.pricePerStep, *v.Elements[0].PriceComponents[0].PricePerStep)
			}
		})
	}

	t.Run("a price that does not follow is left as it is, and reported", func(t *testing.T) {
		v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentTime, Price: 10000, StepSize: 420, PricePerStep: ptr(money.Amount(1000))},
		}}}}
		invalid := tariffs.Validate(v)
		require.Len(t, invalid, 1)
		assert.Equal(t, "elements[0].price_components[0].price", invalid[0].Field)
		assert.Contains(t, invalid[0].Reason, "0.8571")
		assert.Equal(t, money.Amount(10000), v.Elements[0].PriceComponents[0].Price)
	})

	t.Run("a price per step of another dimension changes no price", func(t *testing.T) {
		v := &store.TariffVersion{Currency: "EUR", Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: 3000, StepSize: 1, PricePerStep: ptr(money.Amount(1000))},
		}}}}
		assert.Equal(t, []string{"elements[0].price_components[0].price_per_step"}, fields(tariffs.Validate(v)))
		assert.Equal(t, money.Amount(3000), v.Elements[0].PriceComponents[0].Price)
	})
}

func TestNormalize(t *testing.T) {
	v := &store.TariffVersion{Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
		{Type: store.PriceComponentEnergy, Price: 3000},
		{Type: store.PriceComponentParkingTime, PricePerStep: ptr(money.Amount(5000)), StepSize: 900},
		{Type: "UNKNOWN"},
	}}}}
	tariffs.Normalize(v)
	assert.Equal(t, int32(1), v.Elements[0].PriceComponents[0].StepSize)
	assert.Equal(t, money.Amount(20000), v.Elements[0].PriceComponents[1].Price)
	assert.Equal(t, int32(0), v.Elements[0].PriceComponents[2].StepSize, "a component of a type that is not known is left as it is")

	tariffs.Normalize(&store.TariffVersion{}) // a version without elements
}
