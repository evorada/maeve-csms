// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"golang.org/x/exp/slog"
)

// waitTimeout bounds every wait for a message in these tests.
const waitTimeout = 5 * time.Second

// connect opens a connection to the test server that is closed at the end of the test.
func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	conn, err := natstransport.Connect(natstransport.ConnConfig{Urls: []string{url}, Name: "test"})
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

// publish sends a message to the manager as the gateway would, without trace headers.
func publish(t *testing.T, conn *nats.Conn, subject string, msg *transport.Message) {
	t.Helper()
	data, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, conn.Publish(subject, data))
}

func call(id string) *transport.Message {
	return &transport.Message{
		MessageType:    transport.MessageTypeCall,
		Action:         "Heartbeat",
		MessageId:      id,
		RequestPayload: json.RawMessage(`{}`),
	}
}

// received records what a handler gets.
type received struct {
	stationKey string
	messageId  string
}

// recorder is a transport.MessageHandler that records the messages it handles.
type recorder struct {
	mu  sync.Mutex
	got []received
	ch  chan received
}

func newRecorder() *recorder {
	return &recorder{ch: make(chan received, 1000)}
}

func (r *recorder) handler() transport.MessageHandler {
	return transport.MessageHandlerFunc(func(_ context.Context, stationKey string, msg *transport.Message) {
		rcv := received{stationKey: stationKey, messageId: msg.MessageId}
		r.mu.Lock()
		r.got = append(r.got, rcv)
		r.mu.Unlock()
		r.ch <- rcv
	})
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// next waits for the next handled message.
func (r *recorder) next(t *testing.T) received {
	t.Helper()
	select {
	case rcv := <-r.ch:
		return rcv
	case <-time.After(waitTimeout):
		t.Fatal("timeout waiting for a message to be handled")
		return received{}
	}
}

// captureLogs sends the default logger to a buffer until the end of the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	previous, previousOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() {
		// SetDefault also sends the output of the log package to the handler
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})
	return logs
}

// syncBuffer is a bytes.Buffer that the NATS goroutines and the test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
