// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"net/textproto"

	"github.com/nats-io/nats.go"
)

// headerCarrier carries the trace context in the headers of a NATS message.
//
// NATS headers are case-sensitive. propagation.HeaderCarrier would write the HTTP
// canonical form ("Traceparent"); this carrier writes the keys as the propagator gives
// them, which is the W3C form ("traceparent"), and reads either form. The manager has
// the same carrier.
type headerCarrier nats.Header

func (c headerCarrier) Get(key string) string {
	if v := nats.Header(c).Get(key); v != "" {
		return v
	}
	return nats.Header(c).Get(textproto.CanonicalMIMEHeaderKey(key))
}

func (c headerCarrier) Set(key, value string) {
	nats.Header(c).Set(key, value)
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
