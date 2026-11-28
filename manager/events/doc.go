// SPDX-License-Identifier: Apache-2.0

// Package events turns the domain events of the manager into rows of the transactional
// outbox (store.OutboxStore). Business code emits an event with the ctx of the store
// transaction (store.Transactor.RunInTx) that makes the change it describes, so the
// event is stored if and only if the change is.
//
// The Relay publishes the stored events to JetStream in outbox order, as CloudEvents
// 1.0 JSON (ToCloudEvent) on "cpms.<type>.<org_id>" (Subject), with the event id as
// Nats-Msg-Id so that the stream drops a message that a retry sends again. PurgeJob
// deletes the published events after a retention period. EnsureStream and CheckStream
// set up or check the stream at startup.
//
// # The catalogue
//
// AllTypes lists the event types (Type* constants); each has a payload type
// (*Data structs) and is about a resource (Resource* constants). The connectivity
// tracker emits station.connected and station.disconnected, the OCPP 1.6 handlers
// connector.status_changed and session.started, session.updated and session.completed.
//
// # The envelope
//
// An event is stored with:
//
//   - a UUIDv7 as its id;
//   - the org that owns it: Event.OrgId, or the org of the charge station of
//     Event.StationKey (OrgResolver; the manager uses the tenant resolver, which gives a
//     station that moved to another org the org it has now);
//   - its subject, the URN of the resource it is about (URN);
//   - its actor: Event.Actor, else the actor of the ctx (WithActor), else
//     "station:<station key>" when the event has a station, else DefaultActor;
//   - the W3C traceparent of the span in the ctx, when there is one;
//   - its time (Event.Time, else now) and its data as JSON.
//
// # Errors
//
// An event that is not valid, or whose org cannot be found, fails Emit, and with it the
// transaction of the caller. A storage without an outbox (store.ErrNotImplemented) is
// not an error: the events are dropped and a warning is logged once.
package events
