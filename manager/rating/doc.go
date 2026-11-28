// SPDX-License-Identifier: Apache-2.0

// Package rating works out the price of a charging session and the facts that the price
// depends on.
//
// It tells how long a session charged and how long it stayed plugged in afterwards (idle
// time) from the readings of the energy register of the connector: DetectIdle for a
// session that runs or has ended, and Analyzer, which stores the result with a
// transaction that has ended. The power of a session is measured between two readings
// that differ, so a charger that charges slowly, or whose register moves in coarse steps,
// is not taken for idle.
//
// Rater works out what a session costs with a version of a tariff, by the rules of OCPI
// 2.2.1: the price components of the tariff elements whose restrictions hold, step sizes,
// the minimum and the maximum price and VAT, and the extensions of the tariff model, a
// grace period and a price per step for the time dimensions. It gives the price by its
// parts (PriceBreakdown) and the charging periods of the session. It reads nothing from
// the store: a Session holds all that the price is worked out from.
package rating
