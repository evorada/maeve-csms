// SPDX-License-Identifier: Apache-2.0

// Package nats exchanges OCPP messages with the gateway over core NATS.
//
// Messages from charge stations arrive on
// <prefix>.in.<ocpp16|ocpp201>.<org token>.<station token> and messages to charge
// stations are published on <prefix>.out.<ocpp16|ocpp201>.<org token>.<station token>.
// The subject helpers in subjects.go define how a station key becomes the two last
// tokens; the gateway has a copy of them and of their test vectors.
package nats
