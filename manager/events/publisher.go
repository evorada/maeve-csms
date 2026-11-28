// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"golang.org/x/exp/slog"
)

// DefaultPublishTimeout is how long JetStreamPublisher waits for the acknowledgement of
// one message when it has no timeout of its own.
const DefaultPublishTimeout = 5 * time.Second

// Publisher sends the messages of the relay.
type Publisher interface {
	// Publish sends one message and returns after the server acknowledged it. msgId
	// identifies the message, so that the server can drop a message sent twice.
	Publish(ctx context.Context, subject, msgId string, headers map[string]string, body []byte) error
}

var _ Publisher = (*JetStreamPublisher)(nil)

// JetStreamPublisher publishes to JetStream with msgId as Nats-Msg-Id: within the
// duplicate window of the stream, a message with an id that the stream already holds
// is acknowledged as a duplicate and not stored again.
type JetStreamPublisher struct {
	JS      jetstream.JetStream
	Timeout time.Duration // per publish; DefaultPublishTimeout when 0 or less
}

func (p *JetStreamPublisher) Publish(ctx context.Context, subject, msgId string, headers map[string]string, body []byte) error {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultPublishTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	msg := nats.NewMsg(subject)
	for name, value := range headers {
		// NATS headers are case-sensitive: Set keeps the name as given
		msg.Header.Set(name, value)
	}
	msg.Data = body

	ack, err := p.JS.PublishMsg(ctx, msg, jetstream.WithMsgID(msgId))
	if err != nil {
		return fmt.Errorf("publish message %s to %s: %w", msgId, subject, err)
	}
	if ack.Duplicate {
		slog.Debug("the stream already had the message", "msg_id", msgId, "subject", subject, "stream", ack.Stream)
	}
	return nil
}
