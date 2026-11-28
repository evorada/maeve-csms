// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// SyncTriggers runs SyncTriggersOnce every runEvery until ctx is cancelled.
func SyncTriggers(ctx context.Context,
	tracer trace.Tracer,
	engine store.Engine,
	clock clock.PassiveClock,
	v16CallMaker,
	dataTransferCallMaker,
	v201CallMaker handlers.CallMaker,
	runEvery,
	retryAfter time.Duration) {
	var previousChargeStationId string
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down sync triggers")
			return
		case <-time.After(runEvery):
			previousChargeStationId = SyncTriggersOnce(ctx, tracer, engine, clock, v16CallMaker, dataTransferCallMaker, v201CallMaker,
				previousChargeStationId, retryAfter)
		}
	}
}

// SyncTriggersOnce sends the pending trigger messages of the next page of charge stations
// after the charge station after, and returns where the next pass starts (as
// SyncSettingsOnce does). A trigger message that is sent is not sent again before
// retryAfter.
func SyncTriggersOnce(ctx context.Context,
	tracer trace.Tracer,
	engine store.Engine,
	clock clock.PassiveClock,
	v16CallMaker,
	dataTransferCallMaker,
	v201CallMaker handlers.CallMaker,
	after string,
	retryAfter time.Duration) (next string) {
	ctx, span := tracer.Start(ctx, "sync triggers", trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("sync.trigger.previous", after)))
	defer span.End()
	triggerMessages, err := engine.ListChargeStationTriggerMessages(ctx, 50, after)
	if err != nil {
		span.RecordError(err)
		return after
	}
	if len(triggerMessages) > 0 {
		next = triggerMessages[len(triggerMessages)-1].ChargeStationId
	}
	span.SetAttributes(attribute.Int("sync.trigger.count", len(triggerMessages)))
	for _, pendingTriggerMessage := range triggerMessages {
		// only pending trigger messages are (re)sent: a trigger message that the
		// charge station refused stays in the store with its final status
		if pendingTriggerMessage.TriggerStatus != store.TriggerStatusPending {
			continue
		}
		func() {
			ctx, span := tracer.Start(ctx, "sync trigger", trace.WithSpanKind(trace.SpanKindInternal),
				trace.WithAttributes(
					attribute.String("chargeStationId", pendingTriggerMessage.ChargeStationId),
					attribute.String("sync.trigger.status", string(pendingTriggerMessage.TriggerStatus)),
					attribute.String("sync.trigger.message", string(pendingTriggerMessage.TriggerMessage)),
					attribute.String("sync.trigger.after", pendingTriggerMessage.SendAfter.Format(time.RFC3339)),
				))
			defer span.End()
			details, err := engine.LookupChargeStationRuntimeDetails(ctx, pendingTriggerMessage.ChargeStationId)
			if err != nil {
				span.RecordError(err)
				return
			}
			if details == nil {
				span.RecordError(fmt.Errorf("no runtime details for charge station"))
				return
			}

			csId := pendingTriggerMessage.ChargeStationId
			if clock.Now().After(pendingTriggerMessage.SendAfter) {
				span.SetAttributes(attribute.String("sync.trigger.ocpp_version", details.OcppVersion))
				err = engine.SetChargeStationTriggerMessage(ctx, csId, &store.ChargeStationTriggerMessage{
					TriggerMessage: pendingTriggerMessage.TriggerMessage,
					ConnectorId:    pendingTriggerMessage.ConnectorId,
					TriggerStatus:  store.TriggerStatusPending,
					SendAfter:      clock.Now().Add(retryAfter),
				})
				if err != nil {
					span.RecordError(err)
					return
				}

				if details.OcppVersion == "1.6" {
					if pendingTriggerMessage.TriggerMessage == store.TriggerMessageBootNotification ||
						pendingTriggerMessage.TriggerMessage == store.TriggerMessageDiagnosticStatusNotification ||
						pendingTriggerMessage.TriggerMessage == store.TriggerMessageFirmwareStatusNotification ||
						pendingTriggerMessage.TriggerMessage == store.TriggerMessageHeartbeat ||
						pendingTriggerMessage.TriggerMessage == store.TriggerMessageMeterValues ||
						pendingTriggerMessage.TriggerMessage == store.TriggerMessageStatusNotification {
						err = v16CallMaker.Send(ctx, csId, &ocpp16.TriggerMessageJson{
							RequestedMessage: ocpp16.TriggerMessageJsonRequestedMessage(pendingTriggerMessage.TriggerMessage),
							ConnectorId:      pendingTriggerMessage.ConnectorId,
						})
					} else {
						var evse *ocpp201.EVSEType
						if pendingTriggerMessage.ConnectorId != nil {
							evse = &ocpp201.EVSEType{
								Id: *pendingTriggerMessage.ConnectorId,
							}
						}
						err = dataTransferCallMaker.Send(ctx, csId, &ocpp201.TriggerMessageRequestJson{
							RequestedMessage: ocpp201.MessageTriggerEnumType(pendingTriggerMessage.TriggerMessage),
							Evse:             evse,
						})
					}
				} else {
					var evse *ocpp201.EVSEType
					if pendingTriggerMessage.ConnectorId != nil {
						evse = &ocpp201.EVSEType{
							Id: *pendingTriggerMessage.ConnectorId,
						}
					}
					err = v201CallMaker.Send(ctx, csId, &ocpp201.TriggerMessageRequestJson{
						RequestedMessage: ocpp201.MessageTriggerEnumType(pendingTriggerMessage.TriggerMessage),
						Evse:             evse,
					})
				}

				if err != nil {
					span.RecordError(err)
				}
			}
		}()
	}
	return next
}
