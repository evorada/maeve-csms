// SPDX-License-Identifier: Apache-2.0

package events

import (
	"encoding/json"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// CloudEventContentType is the content type of a CloudEvent in structured mode JSON:
// the Content-Type header of every published event.
const CloudEventContentType = "application/cloudevents+json"

// CloudEvent is the structured-mode CloudEvents 1.0 JSON envelope of a domain event,
// with the extension attributes orgid, actor and traceparent.
type CloudEvent struct {
	SpecVersion     string          `json:"specversion"`     // "1.0"
	Id              string          `json:"id"`              // OutboxEvent.Id
	Source          string          `json:"source"`          // "urn:<product>"
	Type            string          `json:"type"`            // "<product>.<type>"
	Subject         string          `json:"subject"`         // URN of the resource
	Time            time.Time       `json:"time"`            // RFC 3339, UTC
	DataContentType string          `json:"datacontenttype"` // "application/json"
	OrgId           string          `json:"orgid"`
	Actor           string          `json:"actor"`
	TraceParent     string          `json:"traceparent,omitempty"`
	Data            json.RawMessage `json:"data"`
}

// ToCloudEvent returns the CloudEvents envelope of an outbox event of a product, e.g.
// source "urn:cpms" and type "cpms.session.completed" for the product "cpms".
func ToCloudEvent(ev *store.OutboxEvent, product string) CloudEvent {
	return CloudEvent{
		SpecVersion:     "1.0",
		Id:              ev.Id,
		Source:          "urn:" + product,
		Type:            product + "." + ev.Type,
		Subject:         ev.Subject,
		Time:            ev.Time.UTC(),
		DataContentType: "application/json",
		OrgId:           ev.OrgId,
		Actor:           ev.Actor,
		TraceParent:     ev.TraceParent,
		Data:            ev.Data,
	}
}

// Subject returns the NATS subject of an outbox event, "<prefix>.<type>.<org_id>", e.g.
// "cpms.session.completed.<org_id>" for the prefix "cpms". The type and the org id of
// an outbox event are valid subject tokens (the emitter checks them).
func Subject(prefix string, ev *store.OutboxEvent) string {
	return prefix + "." + ev.Type + "." + ev.OrgId
}
