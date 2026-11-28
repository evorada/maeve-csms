// SPDX-License-Identifier: Apache-2.0

package assets

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// ValidationError is a field of an asset that breaks a rule. Field is the name of the
// field in the JSON of the asset API, e.g. "coordinates.latitude" or
// "connectors[0].standard".
type ValidationError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ValidationErrors are the fields of an asset that break a rule, in the order of the
// fields. The Validate functions return them as their error.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	parts := make([]string, len(e))
	for i, v := range e {
		parts[i] = v.Field + ": " + v.Reason
	}
	return "invalid asset: " + strings.Join(parts, "; ")
}

var (
	countryPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
	latitudePattern  = regexp.MustCompile(`^-?[0-9]{1,2}\.[0-9]{5,7}$`)
	longitudePattern = regexp.MustCompile(`^-?[0-9]{1,3}\.[0-9]{5,7}$`)
	hourMinute       = regexp.MustCompile(`^([0-1][0-9]|2[0-3]):[0-5][0-9]$`)
	// eMI3 EVSE id: country, operator, "E" and the power outlet id, the "*" separators
	// optional (the OCPI 2.2.1 format of evse_id); at most 38 characters.
	evseIdPattern = regexp.MustCompile(`^[A-Z]{2}\*?[A-Z0-9]{3}\*?E[A-Z0-9*]{1,30}$`)
)

// OCPI 2.2.1 enums.
var (
	parkingTypes = set("ALONG_MOTORWAY", "PARKING_GARAGE", "PARKING_LOT", "ON_DRIVEWAY",
		"ON_STREET", "UNDERGROUND_GARAGE")
	facilities = set("HOTEL", "RESTAURANT", "CAFE", "MALL", "SUPERMARKET", "SPORT",
		"RECREATION_AREA", "NATURE", "MUSEUM", "BIKE_SHARING", "BUS_STOP", "TAXI_STAND",
		"TRAM_STOP", "METRO_STATION", "TRAIN_STATION", "AIRPORT", "PARKING_LOT",
		"CARPOOL_PARKING", "FUEL_STATION", "WIFI")
	connectorTypes = set("CHADEMO", "CHAOJI",
		"DOMESTIC_A", "DOMESTIC_B", "DOMESTIC_C", "DOMESTIC_D", "DOMESTIC_E", "DOMESTIC_F",
		"DOMESTIC_G", "DOMESTIC_H", "DOMESTIC_I", "DOMESTIC_J", "DOMESTIC_K", "DOMESTIC_L",
		"GBT_AC", "GBT_DC",
		"IEC_60309_2_single_16", "IEC_60309_2_three_16", "IEC_60309_2_three_32", "IEC_60309_2_three_64",
		"IEC_62196_T1", "IEC_62196_T1_COMBO", "IEC_62196_T2", "IEC_62196_T2_COMBO",
		"IEC_62196_T3A", "IEC_62196_T3C",
		"NEMA_5_20", "NEMA_6_30", "NEMA_6_50", "NEMA_10_30", "NEMA_10_50", "NEMA_14_30", "NEMA_14_50",
		"PANTOGRAPH_BOTTOM_UP", "PANTOGRAPH_TOP_DOWN", "TESLA_R", "TESLA_S")
	connectorFormats = set("SOCKET", "CABLE")
	powerTypes       = set("AC_1_PHASE", "AC_2_PHASE", "AC_2_PHASE_SPLIT", "AC_3_PHASE", "DC")
	evseStatuses     = set(string(store.AssetEvseStatusActive), string(store.AssetEvseStatusPlanned),
		string(store.AssetEvseStatusRemoved))
)

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

// Lengths of the OCPI 2.2.1 fields, in characters.
const (
	maxAddressLength     = 45
	maxCityLength        = 45
	maxPostalCodeLength  = 10
	maxStateLength       = 20
	maxNameLength        = 255
	maxOcpiIdLength      = 36
	maxExternalRefs      = 3
	maxExternalRefLength = 255
	// The asset model keeps the OCPI length of physical_reference.
	maxPhysicalReferenceLength = 16
)

// validator collects the rules that the fields of an asset break.
type validator struct {
	prefix string
	errors ValidationErrors
}

func (v *validator) add(field, reason string, args ...any) {
	v.errors = append(v.errors, ValidationError{Field: v.prefix + field, Reason: fmt.Sprintf(reason, args...)})
}

func (v *validator) required(field, value string) bool {
	if value == "" {
		v.add(field, "is required")
		return false
	}
	return true
}

func (v *validator) maxLength(field, value string, limit int) {
	if utf8.RuneCountInString(value) > limit {
		v.add(field, "must be at most %d characters", limit)
	}
}

func (v *validator) optionalMaxLength(field string, value *string, limit int) {
	if value != nil {
		v.maxLength(field, *value, limit)
	}
}

func (v *validator) pattern(field, value string, pattern *regexp.Regexp, reason string) {
	if !pattern.MatchString(value) {
		v.add(field, "%s", reason)
	}
}

func (v *validator) oneOf(field, value string, values map[string]bool) {
	if !values[value] {
		v.add(field, "%q is not a known value", value)
	}
}

func (v *validator) positive(field string, value int64) {
	if value <= 0 {
		v.add(field, "must be greater than 0")
	}
}

// err returns the errors, or nil when there are none.
func (v *validator) err() error {
	if len(v.errors) == 0 {
		return nil
	}
	return v.errors
}

// ValidateLocation checks the fields of a location that its owner sets against the OCPI
// 2.2.1 rules. It returns ValidationErrors, or nil when the location is valid.
func ValidateLocation(loc *store.AssetLocation) error {
	v := &validator{}
	v.required("party_ref", loc.PartyRef)
	v.maxLength("ocpi_id", loc.OcpiId, maxOcpiIdLength)
	v.optionalMaxLength("name", loc.Name, maxNameLength)
	if v.required("address", loc.Address) {
		v.maxLength("address", loc.Address, maxAddressLength)
	}
	if v.required("city", loc.City) {
		v.maxLength("city", loc.City, maxCityLength)
	}
	v.optionalMaxLength("postal_code", loc.PostalCode, maxPostalCodeLength)
	v.optionalMaxLength("state", loc.State, maxStateLength)
	v.pattern("country", loc.Country, countryPattern, "must be an ISO 3166-1 alpha-3 code")
	v.pattern("coordinates.latitude", loc.Latitude, latitudePattern, "must be a decimal latitude with 5 to 7 decimals")
	v.pattern("coordinates.longitude", loc.Longitude, longitudePattern, "must be a decimal longitude with 5 to 7 decimals")
	validateTimeZone(v, loc.TimeZone)
	if loc.ParkingType != nil {
		v.oneOf("parking_type", *loc.ParkingType, parkingTypes)
	}
	seen := make(map[string]bool, len(loc.Facilities))
	for i, facility := range loc.Facilities {
		field := fmt.Sprintf("facilities[%d]", i)
		v.oneOf(field, facility, facilities)
		if seen[facility] {
			v.add(field, "%q is listed twice", facility)
		}
		seen[facility] = true
	}
	if len(loc.ExternalRefs) > maxExternalRefs {
		v.add("external_refs", "must have at most %d entries", maxExternalRefs)
	}
	for i, ref := range loc.ExternalRefs {
		v.maxLength(fmt.Sprintf("external_refs[%d]", i), ref, maxExternalRefLength)
	}
	if loc.OpeningTimes != nil {
		validateOpeningTimes(v, loc.OpeningTimes)
	}
	return v.err()
}

// validateTimeZone accepts the names of the IANA time zone database. time.LoadLocation
// also accepts "" and "Local", which are not.
func validateTimeZone(v *validator, timeZone string) {
	if !v.required("time_zone", timeZone) {
		return
	}
	if _, err := time.LoadLocation(timeZone); err != nil || timeZone == "Local" {
		v.add("time_zone", "must be an IANA time zone, e.g. Europe/Madrid")
	}
}

func validateOpeningTimes(v *validator, openingTimes *store.OpeningTimes) {
	switch {
	case openingTimes.TwentyFourSeven && len(openingTimes.RegularHours) > 0:
		v.add("opening_times.regular_hours", "must be empty when twentyfourseven is true")
	case !openingTimes.TwentyFourSeven && len(openingTimes.RegularHours) == 0:
		v.add("opening_times.regular_hours", "is required when twentyfourseven is false")
	}
	for i, hours := range openingTimes.RegularHours {
		field := fmt.Sprintf("opening_times.regular_hours[%d]", i)
		if hours.Weekday < 1 || hours.Weekday > 7 {
			v.add(field+".weekday", "must be 1 (Monday) to 7 (Sunday)")
		}
		v.pattern(field+".period_begin", hours.PeriodBegin, hourMinute, "must be a time HH:MM")
		v.pattern(field+".period_end", hours.PeriodEnd, hourMinute, "must be a time HH:MM")
	}
	validatePeriods(v, "opening_times.exceptional_openings", openingTimes.ExceptionalOpenings)
	validatePeriods(v, "opening_times.exceptional_closings", openingTimes.ExceptionalClosings)
}

func validatePeriods(v *validator, field string, periods []store.ExceptionalPeriod) {
	for i, period := range periods {
		if !period.PeriodEnd.After(period.PeriodBegin) {
			v.add(fmt.Sprintf("%s[%d].period_end", field, i), "must be after period_begin")
		}
	}
}

// ValidateEvse checks the fields of an EVSE that its owner sets, and its connectors. It
// returns ValidationErrors, or nil when the EVSE is valid.
func ValidateEvse(evse *store.AssetEvse) error {
	v := &validator{}
	v.positive("ocpp_evse_id", int64(evse.OcppEvseId))
	if evse.EvseId != nil {
		v.pattern("evse_id", *evse.EvseId, evseIdPattern, "must be an eMI3 EVSE id, e.g. ES*CCM*E12345*1")
	}
	v.optionalMaxLength("physical_reference", evse.PhysicalReference, maxPhysicalReferenceLength)
	if evse.Status != "" {
		v.oneOf("status", string(evse.Status), evseStatuses)
	}
	ocppIds := make(map[int]bool, len(evse.Connectors))
	ocpiIds := make(map[string]bool, len(evse.Connectors))
	for i, c := range evse.Connectors {
		cv := &validator{prefix: fmt.Sprintf("connectors[%d].", i)}
		validateConnector(cv, c)
		if ocppIds[c.OcppConnectorId] {
			cv.add("ocpp_connector_id", "%d is used by another connector of the EVSE", c.OcppConnectorId)
		}
		ocppIds[c.OcppConnectorId] = true
		if c.OcpiId != "" {
			if ocpiIds[c.OcpiId] {
				cv.add("ocpi_id", "%q is used by another connector of the EVSE", c.OcpiId)
			}
			ocpiIds[c.OcpiId] = true
		}
		v.errors = append(v.errors, cv.errors...)
	}
	return v.err()
}

// ValidateConnector checks the fields of a connector that its owner sets. It returns
// ValidationErrors, or nil when the connector is valid.
func ValidateConnector(c *store.AssetConnector) error {
	v := &validator{}
	validateConnector(v, c)
	return v.err()
}

func validateConnector(v *validator, c *store.AssetConnector) {
	v.positive("ocpp_connector_id", int64(c.OcppConnectorId))
	v.maxLength("ocpi_id", c.OcpiId, maxOcpiIdLength)
	v.oneOf("standard", c.Standard, connectorTypes)
	v.oneOf("format", c.Format, connectorFormats)
	v.oneOf("power_type", c.PowerType, powerTypes)
	v.positive("max_voltage", int64(c.MaxVoltage))
	v.positive("max_amperage", int64(c.MaxAmperage))
	if c.MaxElectricPower != nil {
		v.positive("max_electric_power", int64(*c.MaxElectricPower))
	}
}
