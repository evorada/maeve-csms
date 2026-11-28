// SPDX-License-Identifier: Apache-2.0

package events_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
	clocktest "k8s.io/utils/clock/testing"
)

func testStreamConfig() events.StreamConfig {
	return events.StreamConfig{
		Name:       events.DefaultStreamName,
		Subjects:   []string{"cpms.>"},
		MaxAge:     events.DefaultStreamMaxAge,
		Replicas:   1,
		Duplicates: events.DefaultDuplicateWindow,
	}
}

// newJetStream connects to an embedded server; with jetStream false the server has no
// JetStream.
func newJetStream(t *testing.T, jetStream bool) jetstream.JetStream {
	t.Helper()
	_, url := natstest.RunServer(t, jetStream)
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	require.NoError(t, err)
	return js
}

func TestEnsureStreamCreatesTheStream(t *testing.T) {
	ctx := context.Background()
	js := newJetStream(t, true)

	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))

	stream, err := js.Stream(ctx, "CPMS_EVENTS")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"cpms.>"}, info.Config.Subjects)
	assert.Equal(t, 720*time.Hour, info.Config.MaxAge)
	assert.Equal(t, 1, info.Config.Replicas)
	assert.Equal(t, 2*time.Minute, info.Config.Duplicates)
	assert.Equal(t, jetstream.LimitsPolicy, info.Config.Retention)
	assert.Equal(t, jetstream.FileStorage, info.Config.Storage)
	assert.Equal(t, jetstream.DiscardOld, info.Config.Discard)

	_, err = js.Publish(ctx, "cpms.session.updated."+testOrgId, []byte(`{}`))
	require.NoError(t, err)

	// a second call keeps the stream and what it holds
	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))
	again, err := js.Stream(ctx, "CPMS_EVENTS")
	require.NoError(t, err)
	againInfo, err := again.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, info.Created, againInfo.Created)
	assert.Equal(t, uint64(1), againInfo.State.Msgs)
	assert.Equal(t, info.Config.Subjects, againInfo.Config.Subjects)
}

func TestEnsureStreamWithoutJetStream(t *testing.T) {
	js := newJetStream(t, false)
	assert.Error(t, events.EnsureStream(context.Background(), js, testStreamConfig()))
}

func TestCheckStream(t *testing.T) {
	ctx := context.Background()
	js := newJetStream(t, true)

	err := events.CheckStream(ctx, js, testStreamConfig())
	require.ErrorIs(t, err, jetstream.ErrStreamNotFound)
	assert.ErrorContains(t, err, "CPMS_EVENTS")

	// a stream that captures only some of the subjects is not enough
	_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: "CPMS_EVENTS", Subjects: []string{"cpms.session.>", "cpms.*"}})
	require.NoError(t, err)
	err = events.CheckStream(ctx, js, testStreamConfig())
	require.Error(t, err)
	assert.ErrorContains(t, err, "does not capture cpms.>")

	// the stream of the infrastructure may capture more
	_, err = js.UpdateStream(ctx, jetstream.StreamConfig{Name: "CPMS_EVENTS", Subjects: []string{"cpms.>", "other.>"}})
	require.NoError(t, err)
	require.NoError(t, events.CheckStream(ctx, js, testStreamConfig()))

	// CheckStream does not create or change the stream
	require.NoError(t, js.DeleteStream(ctx, "CPMS_EVENTS"))
	require.ErrorIs(t, events.CheckStream(ctx, js, testStreamConfig()), jetstream.ErrStreamNotFound)
	_, err = js.Stream(ctx, "CPMS_EVENTS")
	assert.ErrorIs(t, err, jetstream.ErrStreamNotFound)
}

// receive reads n messages of the stream from the start, in stream order.
func receive(t *testing.T, js jetstream.JetStream, n int) []jetstream.Msg {
	t.Helper()
	ctx := context.Background()
	consumer, err := js.OrderedConsumer(ctx, "CPMS_EVENTS", jetstream.OrderedConsumerConfig{FilterSubjects: []string{"cpms.>"}})
	require.NoError(t, err)
	batch, err := consumer.Fetch(n, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	var msgs []jetstream.Msg
	for msg := range batch.Messages() {
		msgs = append(msgs, msg)
	}
	require.NoError(t, batch.Error())
	return msgs
}

func TestRelayToJetStream(t *testing.T) {
	ctx := context.Background()
	js := newJetStream(t, true)
	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))

	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 3)
	relay := newRelay(s, &events.JetStreamPublisher{JS: js})
	require.NoError(t, relay.RunOnce(ctx))
	assert.Len(t, publishedEvents(t, s), 3)

	msgs := receive(t, js, 3)
	require.Len(t, msgs, 3)
	for i, msg := range msgs {
		ev := evs[i]
		assert.Equal(t, "cpms.session.updated."+testOrgId, msg.Subject())
		assert.Equal(t, ev.Id, msg.Headers().Get(jetstream.MsgIDHeader), "Nats-Msg-Id is the event id")
		assert.Equal(t, "application/cloudevents+json", msg.Headers().Get("Content-Type"))
		assert.Equal(t, ev.TraceParent, msg.Headers().Get("traceparent"))

		var ce events.CloudEvent
		require.NoError(t, json.Unmarshal(msg.Data(), &ce))
		assert.Equal(t, ev.Id, ce.Id)
		assert.Equal(t, "cpms.session.updated", ce.Type)
		assert.Equal(t, testOrgId, ce.OrgId)
		assert.JSONEq(t, string(ev.Data), string(ce.Data))
	}
}

func TestJetStreamDropsAnEventPublishedTwice(t *testing.T) {
	ctx := context.Background()
	js := newJetStream(t, true)
	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))

	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 2)
	publisher := &events.JetStreamPublisher{JS: js, Timeout: time.Second}

	// the first event went out before, but its batch could not be marked published
	body, err := json.Marshal(events.ToCloudEvent(evs[0], events.Product))
	require.NoError(t, err)
	require.NoError(t, publisher.Publish(ctx, events.Subject("cpms", evs[0]), evs[0].Id, nil, body))

	require.NoError(t, newRelay(s, publisher).RunOnce(ctx))

	stream, err := js.Stream(ctx, "CPMS_EVENTS")
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), info.State.Msgs, "the stream holds each event once")

	msgs := receive(t, js, 2)
	require.Len(t, msgs, 2)
	assert.Equal(t, evs[0].Id, msgs[0].Headers().Get(jetstream.MsgIDHeader))
	assert.Equal(t, evs[1].Id, msgs[1].Headers().Get(jetstream.MsgIDHeader))
}

func TestRelayKeepsTheEventsWhileTheStreamIsMissing(t *testing.T) {
	ctx := context.Background()
	js := newJetStream(t, true)

	s := inmemory.NewStore(clocktest.NewFakePassiveClock(testNow))
	evs := appendEvents(t, s, 2)
	relay := newRelay(s, &events.JetStreamPublisher{JS: js, Timeout: time.Second})

	err := relay.RunOnce(ctx)
	require.Error(t, err)
	assert.ErrorContains(t, err, evs[0].Id)
	assert.Empty(t, publishedEvents(t, s), "nothing is marked published without the stream")

	// once the stream exists, the next run publishes them
	require.NoError(t, events.EnsureStream(ctx, js, testStreamConfig()))
	require.NoError(t, relay.RunOnce(ctx))
	assert.Equal(t, ids(evs), ids(publishedEvents(t, s)))
	assert.Len(t, receive(t, js, 2), 2)
}

func TestJetStreamPublisherTimeout(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	js, err := jetstream.New(conn)
	require.NoError(t, err)

	// a subscriber that never answers stands in for a server that does not acknowledge
	silent, err := conn.SubscribeSync("cpms.>")
	require.NoError(t, err)
	require.NoError(t, conn.Flush())

	publisher := &events.JetStreamPublisher{JS: js, Timeout: 100 * time.Millisecond}
	const traceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	start := time.Now()
	err = publisher.Publish(context.Background(), "cpms.session.updated."+testOrgId, "id-1",
		map[string]string{"traceparent": traceParent}, []byte(`{}`))
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.ErrorContains(t, err, "id-1")
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	assert.Less(t, elapsed, 2*time.Second, "the publish timeout bounds the wait")

	msg, err := silent.NextMsg(time.Second)
	require.NoError(t, err)
	assert.Equal(t, "id-1", msg.Header.Get(jetstream.MsgIDHeader))
	assert.Equal(t, []string{traceParent}, msg.Header["traceparent"], "the header name is kept as given")
}
