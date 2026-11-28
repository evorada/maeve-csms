// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"time"
)

// OutboxEvent is a domain event in the transactional outbox: it is stored in the same
// transaction as the change it describes, and published from there later.
type OutboxEvent struct {
	Id           string          // UUIDv7; CloudEvents id and Nats-Msg-Id
	Seq          int64           // assigned on append; strictly increasing
	PublishedSeq int64           // assigned when published; 0 = not yet published; replay cursor
	OrgId        string          // owning org (subject suffix, CloudEvents "orgid")
	Type         string          // short type without product prefix, e.g. "session.completed"
	Subject      string          // resource URN, e.g. "urn:cpms:<org_id>:session/<id>"
	Actor        string          // "station:<key>", "user:<sub>", "svc:<name>"
	TraceParent  string          // W3C traceparent of the producing span, may be empty
	Time         time.Time       // when the change happened
	Data         json.RawMessage // payload (CloudEvents "data")
	CreatedAt    time.Time
	PublishedAt  *time.Time
}

// MaxOutboxEventListLimit is the most events that one ListOutboxEvents call returns.
const MaxOutboxEventListLimit = 1000

// DefaultOutboxEventListLimit is the number of events that ListOutboxEvents returns when
// the filter sets no limit.
const DefaultOutboxEventListLimit = 100

// OutboxEventFilter selects the published events that ListOutboxEvents returns.
type OutboxEventFilter struct {
	AfterPublishedSeq int64    // return events with PublishedSeq > this
	OrgIds            []string // empty = all orgs
	Types             []string // empty = all types
	// Limit is 1..MaxOutboxEventListLimit: 0 or less is DefaultOutboxEventListLimit, more
	// than the maximum is the maximum.
	Limit int
}

// EffectiveLimit is the number of events that a list with the filter returns at most.
func (f OutboxEventFilter) EffectiveLimit() int {
	switch {
	case f.Limit <= 0:
		return DefaultOutboxEventListLimit
	case f.Limit > MaxOutboxEventListLimit:
		return MaxOutboxEventListLimit
	default:
		return f.Limit
	}
}

// OutboxPublishFunc publishes events in the given order and returns how many leading
// events were published successfully (and the error that stopped it, if any).
type OutboxPublishFunc func(ctx context.Context, events []*OutboxEvent) (published int, err error)

type OutboxStore interface {
	// AppendOutboxEvents stores events, joining the transaction in ctx if any. Sets Seq and
	// CreatedAt.
	AppendOutboxEvents(ctx context.Context, events ...*OutboxEvent) error
	// RelayOutboxEvents claims up to limit unpublished events in Seq order, calls publish,
	// and marks the published prefix (PublishedAt, PublishedSeq in the same order).
	// Only one relay batch runs at a time across all replicas; a caller that cannot get
	// the relay lock returns (0, nil) immediately. Returns the number marked published and
	// publish's error. It never joins a transaction in ctx.
	RelayOutboxEvents(ctx context.Context, limit int, publish OutboxPublishFunc) (int, error)
	// ListOutboxEvents returns published events ordered by PublishedSeq (replay).
	ListOutboxEvents(ctx context.Context, filter OutboxEventFilter) ([]*OutboxEvent, error)
	// PurgePublishedOutboxEvents deletes events published before the given time and
	// returns how many it deleted.
	PurgePublishedOutboxEvents(ctx context.Context, publishedBefore time.Time) (int64, error)
}
