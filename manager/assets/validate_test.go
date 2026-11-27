// SPDX-License-Identifier: Apache-2.0

package assets_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func ptr[T any](v T) *T {
	return &v
}

func validLocation() *store.AssetLocation {
	return &store.AssetLocation{
		OrgId:       "0193b2c4-0000-7000-8000-0000000000a1",
		PartyRef:    "party-a",
		OcpiId:      "LOC-1",
		Name:        ptr("Plaza"),
		Address:     "Calle Mayor 1",
		City:        "Madrid",
		PostalCode:  ptr("28013"),
		State:       ptr("Madrid"),
		Country:     "ESP",
		Latitude:    "40.416775",
		Longitude:   "-3.7037902",
		TimeZone:    "Europe/Madrid",
		ParkingType: ptr("ON_STREET"),
		Facilities:  []string{"CAFE", "WIFI"},
		OpeningTimes: &store.OpeningTimes{
			RegularHours: []store.RegularHours{{Weekday: 1, PeriodBegin: "08:00", PeriodEnd: "23:59"}},
			ExceptionalClosings: []store.ExceptionalPeriod{{
				PeriodBegin: time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC),
				PeriodEnd:   time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC),
			}},
		},
		ExternalRefs: []string{"a", "b", strings.Repeat("c", 255)},
	}
}

func validConnector(ocppConnectorId int) *store.AssetConnector {
	return &store.AssetConnector{
		OcppConnectorId:  ocppConnectorId,
		Standard:         "IEC_62196_T2_COMBO",
		Format:           "CABLE",
		PowerType:        "DC",
		MaxVoltage:       920,
		MaxAmperage:      200,
		MaxElectricPower: ptr(int32(150000)),
	}
}

func validEvse() *store.AssetEvse {
	return &store.AssetEvse{
		OcppEvseId:        1,
		EvseId:            ptr("ES*CCM*ECP001*1"),
		PhysicalReference: ptr(strings.Repeat("P", 16)),
		Status:            store.AssetEvseStatusPlanned,
		Connectors:        []*store.AssetConnector{validConnector(1), validConnector(2)},
	}
}

// assertInvalid checks that err holds exactly one validation error, for the field.
func assertInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var invalid assets.ValidationErrors
	if assert.True(t, errors.As(err, &invalid), "%v", err) && assert.Len(t, invalid, 1, "%v", err) {
		assert.Equal(t, field, invalid[0].Field)
		assert.NotEmpty(t, invalid[0].Reason)
	}
}

func TestValidateLocation(t *testing.T) {
	require.NoError(t, assets.ValidateLocation(validLocation()))

	minimal := validLocation()
	minimal.OcpiId = "" // set by the store
	minimal.Name, minimal.PostalCode, minimal.State, minimal.ParkingType = nil, nil, nil, nil
	minimal.Facilities, minimal.ExternalRefs, minimal.OpeningTimes = nil, nil, nil
	assert.NoError(t, assets.ValidateLocation(minimal))

	allDay := validLocation()
	allDay.OpeningTimes = &store.OpeningTimes{TwentyFourSeven: true}
	assert.NoError(t, assets.ValidateLocation(allDay))

	for _, tc := range []struct {
		name   string
		change func(loc *store.AssetLocation)
		field  string
	}{
		{"no party", func(loc *store.AssetLocation) { loc.PartyRef = "" }, "party_ref"},
		{"country alpha-2", func(loc *store.AssetLocation) { loc.Country = "ES" }, "country"},
		{"country lowercase", func(loc *store.AssetLocation) { loc.Country = "esp" }, "country"},
		{"latitude with 4 decimals", func(loc *store.AssetLocation) { loc.Latitude = "40.4167" }, "coordinates.latitude"},
		{"latitude with 3 digits", func(loc *store.AssetLocation) { loc.Latitude = "140.416775" }, "coordinates.latitude"},
		{"longitude with 8 decimals", func(loc *store.AssetLocation) { loc.Longitude = "-3.70379021" }, "coordinates.longitude"},
		{"longitude with a comma", func(loc *store.AssetLocation) { loc.Longitude = "-3,703790" }, "coordinates.longitude"},
		{"no time zone", func(loc *store.AssetLocation) { loc.TimeZone = "" }, "time_zone"},
		{"unknown time zone", func(loc *store.AssetLocation) { loc.TimeZone = "Europe/Atlantis" }, "time_zone"},
		{"local time zone", func(loc *store.AssetLocation) { loc.TimeZone = "Local" }, "time_zone"},
		{"no address", func(loc *store.AssetLocation) { loc.Address = "" }, "address"},
		{"long address", func(loc *store.AssetLocation) { loc.Address = strings.Repeat("a", 46) }, "address"},
		{"no city", func(loc *store.AssetLocation) { loc.City = "" }, "city"},
		{"long city", func(loc *store.AssetLocation) { loc.City = strings.Repeat("c", 46) }, "city"},
		{"long postal code", func(loc *store.AssetLocation) { loc.PostalCode = ptr("12345678901") }, "postal_code"},
		{"long state", func(loc *store.AssetLocation) { loc.State = ptr(strings.Repeat("s", 21)) }, "state"},
		{"long name", func(loc *store.AssetLocation) { loc.Name = ptr(strings.Repeat("n", 256)) }, "name"},
		{"long OCPI id", func(loc *store.AssetLocation) { loc.OcpiId = strings.Repeat("i", 37) }, "ocpi_id"},
		{"unknown parking type", func(loc *store.AssetLocation) { loc.ParkingType = ptr("ROOFTOP") }, "parking_type"},
		{"unknown facility", func(loc *store.AssetLocation) { loc.Facilities = []string{"CAFE", "SPA"} }, "facilities[1]"},
		{"facility twice", func(loc *store.AssetLocation) { loc.Facilities = []string{"CAFE", "WIFI", "CAFE"} }, "facilities[2]"},
		{"four external refs", func(loc *store.AssetLocation) { loc.ExternalRefs = []string{"a", "b", "c", "d"} }, "external_refs"},
		{"long external ref", func(loc *store.AssetLocation) { loc.ExternalRefs = []string{strings.Repeat("r", 256)} }, "external_refs[0]"},
		{"twentyfourseven with regular hours", func(loc *store.AssetLocation) { loc.OpeningTimes.TwentyFourSeven = true }, "opening_times.regular_hours"},
		{"neither twentyfourseven nor regular hours", func(loc *store.AssetLocation) { loc.OpeningTimes.RegularHours = nil }, "opening_times.regular_hours"},
		{"weekday 0", func(loc *store.AssetLocation) { loc.OpeningTimes.RegularHours[0].Weekday = 0 }, "opening_times.regular_hours[0].weekday"},
		{"weekday 8", func(loc *store.AssetLocation) { loc.OpeningTimes.RegularHours[0].Weekday = 8 }, "opening_times.regular_hours[0].weekday"},
		{"begin without leading zero", func(loc *store.AssetLocation) { loc.OpeningTimes.RegularHours[0].PeriodBegin = "8:00" }, "opening_times.regular_hours[0].period_begin"},
		{"end 24:00", func(loc *store.AssetLocation) { loc.OpeningTimes.RegularHours[0].PeriodEnd = "24:00" }, "opening_times.regular_hours[0].period_end"},
		{"exceptional period that ends before it begins", func(loc *store.AssetLocation) {
			loc.OpeningTimes.ExceptionalClosings[0].PeriodEnd = loc.OpeningTimes.ExceptionalClosings[0].PeriodBegin
		}, "opening_times.exceptional_closings[0].period_end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc := validLocation()
			tc.change(loc)
			assertInvalid(t, assets.ValidateLocation(loc), tc.field)
		})
	}

	t.Run("every broken rule is reported", func(t *testing.T) {
		loc := validLocation()
		loc.Country = "ES"
		loc.City = ""
		var invalid assets.ValidationErrors
		require.ErrorAs(t, assets.ValidateLocation(loc), &invalid)
		assert.Len(t, invalid, 2)
		assert.Contains(t, invalid.Error(), "country")
	})
}

func TestValidateEvse(t *testing.T) {
	require.NoError(t, assets.ValidateEvse(validEvse()))

	minimal := validEvse()
	minimal.EvseId, minimal.PhysicalReference, minimal.Status, minimal.Connectors = nil, nil, "", nil
	assert.NoError(t, assets.ValidateEvse(minimal))
	withoutSeparators := validEvse()
	withoutSeparators.EvseId = ptr("ESCCME12345")
	assert.NoError(t, assets.ValidateEvse(withoutSeparators))

	for _, tc := range []struct {
		name   string
		change func(evse *store.AssetEvse)
		field  string
	}{
		{"OCPP EVSE id 0", func(evse *store.AssetEvse) { evse.OcppEvseId = 0 }, "ocpp_evse_id"},
		{"EVSE id without E", func(evse *store.AssetEvse) { evse.EvseId = ptr("ES*CCM*CP001*1") }, "evse_id"},
		{"EVSE id lowercase", func(evse *store.AssetEvse) { evse.EvseId = ptr("es*ccm*ecp001*1") }, "evse_id"},
		{"EVSE id too long", func(evse *store.AssetEvse) { evse.EvseId = ptr("ES*CCM*E" + strings.Repeat("1", 31)) }, "evse_id"},
		{"long physical reference", func(evse *store.AssetEvse) { evse.PhysicalReference = ptr(strings.Repeat("P", 17)) }, "physical_reference"},
		{"unknown status", func(evse *store.AssetEvse) { evse.Status = "BROKEN" }, "status"},
		{"invalid connector", func(evse *store.AssetEvse) { evse.Connectors[1].Format = "PLUG" }, "connectors[1].format"},
		{"OCPP connector id twice", func(evse *store.AssetEvse) { evse.Connectors[1].OcppConnectorId = 1 }, "connectors[1].ocpp_connector_id"},
		{"OCPI connector id twice", func(evse *store.AssetEvse) {
			evse.Connectors[0].OcpiId = "A"
			evse.Connectors[1].OcpiId = "A"
		}, "connectors[1].ocpi_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evse := validEvse()
			tc.change(evse)
			assertInvalid(t, assets.ValidateEvse(evse), tc.field)
		})
	}
}

func TestValidateConnector(t *testing.T) {
	require.NoError(t, assets.ValidateConnector(validConnector(1)))
	for _, powerType := range []string{"AC_1_PHASE", "AC_2_PHASE", "AC_2_PHASE_SPLIT", "AC_3_PHASE"} {
		c := validConnector(1)
		c.PowerType = powerType
		assert.NoError(t, assets.ValidateConnector(c), powerType)
	}
	withoutPower := validConnector(1)
	withoutPower.MaxElectricPower = nil
	assert.NoError(t, assets.ValidateConnector(withoutPower))

	for _, tc := range []struct {
		name   string
		change func(c *store.AssetConnector)
		field  string
	}{
		{"OCPP connector id 0", func(c *store.AssetConnector) { c.OcppConnectorId = 0 }, "ocpp_connector_id"},
		{"long OCPI id", func(c *store.AssetConnector) { c.OcpiId = strings.Repeat("i", 37) }, "ocpi_id"},
		{"unknown standard", func(c *store.AssetConnector) { c.Standard = "TYPE2" }, "standard"},
		{"unknown format", func(c *store.AssetConnector) { c.Format = "PLUG" }, "format"},
		{"unknown power type", func(c *store.AssetConnector) { c.PowerType = "AC" }, "power_type"},
		{"voltage 0", func(c *store.AssetConnector) { c.MaxVoltage = 0 }, "max_voltage"},
		{"negative amperage", func(c *store.AssetConnector) { c.MaxAmperage = -1 }, "max_amperage"},
		{"power 0", func(c *store.AssetConnector) { c.MaxElectricPower = ptr(int32(0)) }, "max_electric_power"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validConnector(1)
			tc.change(c)
			assertInvalid(t, assets.ValidateConnector(c), tc.field)
		})
	}
}
