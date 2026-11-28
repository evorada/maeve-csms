// SPDX-License-Identifier: Apache-2.0

// Package tariffs works on the tariffs of the store, which are kept in the OCPI 2.2.1
// model with a few extensions: it validates the content of a tariff version before it is
// stored, and converts the simple tariff form (a start fee, a price per kWh, a time and
// a parking price per step with a grace period, and parking windows by weekday and time
// of day) into a tariff version.
package tariffs
