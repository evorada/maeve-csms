// SPDX-License-Identifier: Apache-2.0

// Package events turns the domain events of the manager into rows of the transactional
// outbox (store.OutboxStore). Business code emits an event with the ctx of the store
// transaction (store.Transactor.RunInTx) that makes the change it describes, so the
// event is stored if and only if the change is. Publishing the stored events is not
// done here.
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
