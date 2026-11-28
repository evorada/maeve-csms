// SPDX-License-Identifier: Apache-2.0

// Package rating works out the facts that the price of a charging session depends on.
//
// It tells how long a session charged and how long it stayed plugged in afterwards (idle
// time) from the readings of the energy register of the connector: DetectIdle for a
// session that runs or has ended, and Analyzer, which stores the result with a
// transaction that has ended. The power of a session is measured between two readings
// that differ, so a charger that charges slowly, or whose register moves in coarse steps,
// is not taken for idle.
package rating
