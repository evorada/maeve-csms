// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const envelopeOrgId = "0193b2c4-1f2e-7c3d-8a4b-5c6d7e8f9a0b"

// connectedEvent is a fixed outbox event, as the relay gets it from the store.
func connectedEvent() *store.OutboxEvent {
	return &store.OutboxEvent{
		Id:          "01937f5e-8c2a-7d43-9b1e-4f0a6c2d9e10",
		Seq:         7,
		OrgId:       envelopeOrgId,
		Type:        "station.connected",
		Subject:     "urn:cpms:" + envelopeOrgId + ":station/k7f2.CP001",
		Actor:       "station:k7f2.CP001",
		TraceParent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		Time:        time.Date(2026, 11, 26, 11, 15, 3, 120_000_000, time.FixedZone("CET", 3600)),
		Data:        json.RawMessage(`{"station_key":"k7f2.CP001","station_id":"CP001"}`),
	}
}

func TestToCloudEventGoldenJSON(t *testing.T) {
	body, err := json.Marshal(events.ToCloudEvent(connectedEvent(), events.Product))
	require.NoError(t, err)

	// exactly these attributes, in this order; the time in UTC
	const want = `{"specversion":"1.0",` +
		`"id":"01937f5e-8c2a-7d43-9b1e-4f0a6c2d9e10",` +
		`"source":"urn:cpms",` +
		`"type":"cpms.station.connected",` +
		`"subject":"urn:cpms:0193b2c4-1f2e-7c3d-8a4b-5c6d7e8f9a0b:station/k7f2.CP001",` +
		`"time":"2026-11-26T10:15:03.12Z",` +
		`"datacontenttype":"application/json",` +
		`"orgid":"0193b2c4-1f2e-7c3d-8a4b-5c6d7e8f9a0b",` +
		`"actor":"station:k7f2.CP001",` +
		`"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",` +
		`"data":{"station_key":"k7f2.CP001","station_id":"CP001"}}`
	assert.Equal(t, want, string(body))
}

func TestToCloudEventWithoutTraceParent(t *testing.T) {
	ev := connectedEvent()
	ev.TraceParent = ""

	body, err := json.Marshal(events.ToCloudEvent(ev, events.Product))
	require.NoError(t, err)

	var attributes map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &attributes))
	assert.NotContains(t, attributes, "traceparent")
	assert.ElementsMatch(t,
		[]string{"specversion", "id", "source", "type", "subject", "time", "datacontenttype", "orgid", "actor", "data"},
		keys(attributes))
}

func TestToCloudEventUsesTheProduct(t *testing.T) {
	ce := events.ToCloudEvent(connectedEvent(), "other")
	assert.Equal(t, "urn:other", ce.Source)
	assert.Equal(t, "other.station.connected", ce.Type)
}

func TestSubject(t *testing.T) {
	ev := connectedEvent()
	assert.Equal(t, "cpms.station.connected."+envelopeOrgId, events.Subject("cpms", ev))

	ev.Type = "session.completed"
	ev.OrgId = "org_1"
	assert.Equal(t, "csms.session.completed.org_1", events.Subject("csms", ev))
}

func keys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
