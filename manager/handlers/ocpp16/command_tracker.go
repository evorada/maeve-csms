// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

// CommandTracker follows the commands sent to the stations to their effect: it learns
// about the transactions that the stations start and stop and about the connector
// statuses that they report. Its methods log their own errors.
type CommandTracker interface {
	// OnTransactionStarted is told about a transaction that a station started and
	// whose idTag was accepted. transactionId is the id the transaction is stored with,
	// ocppTransactionId the id the station got.
	OnTransactionStarted(ctx context.Context, stationKey string, connectorId int, idTag, transactionId string, ocppTransactionId int)
	// OnTransactionStopped is told about a transaction that a station stopped.
	OnTransactionStopped(ctx context.Context, stationKey string, ocppTransactionId int, reason string)
	// OnConnectorStatus is told about the status (OCPP 1.6 ChargePointStatus) of a
	// connector of a station.
	OnConnectorStatus(ctx context.Context, stationKey string, connectorId int, status string)
}

// trackCommand tells the tracker, when there is one, about what a station did, once
// the message of the station is handled. Nothing that goes wrong in the tracker, a
// panic included, reaches the handling of the message: charging never depends on it.
func trackCommand(ctx context.Context, tracker CommandTracker, chargeStationId, message string, tell func(CommandTracker)) {
	if tracker == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("following the commands of a station panicked", slog.String("chargeStationId", chargeStationId),
				slog.String("message", message), "panic", r)
			trace.SpanFromContext(ctx).AddEvent("command tracker panicked")
		}
	}()
	tell(tracker)
}
