// SPDX-License-Identifier: Apache-2.0

// Package tariffs works on the tariffs of the store, which are kept in the OCPI 2.2.1
// model with a few extensions: it validates the content of a tariff version before it is
// stored, converts the simple tariff form (a start fee, a price per kWh, a time and a
// parking price per step with a grace period, and parking windows by weekday and time of
// day) into a tariff version, and selects the tariff that a transaction is charged with
// (Selector).
package tariffs
