// SPDX-License-Identifier: Apache-2.0

// Package eventstest helps tests check the domain events that code emits: that each one
// is emitted inside a store transaction, and what reached the outbox. Only tests import
// it.
package eventstest

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"k8s.io/utils/clock"
)

// Store is a store with transactions and an outbox, such as the in-memory store.
type Store interface {
	store.Transactor
	store.OutboxStore
}

type txKey struct{}

// Transactor runs the transactions of Store and marks the ctx that it gives fn, so that
// Emitter can tell whether an event is emitted inside a transaction.
type Transactor struct {
	Store store.Transactor
	// CommitErr, when set, fails every outermost transaction whose fn returned nil, as a
	// commit that fails: what fn stored in the outbox is dropped.
	CommitErr error
}

func (t *Transactor) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return t.Store.RunInTx(ctx, func(ctx context.Context) error {
		nested := InTx(ctx)
		if err := fn(context.WithValue(ctx, txKey{}, true)); err != nil {
			return err
		}
		if nested {
			return nil
		}
		return t.CommitErr
	})
}

// InTx reports whether ctx is the ctx of a transaction of a Transactor.
func InTx(ctx context.Context) bool {
	inTx, _ := ctx.Value(txKey{}).(bool)
	return inTx
}

// Emitter hands the events to Emitter and counts those emitted outside a transaction
// of a Transactor. When Err is set, every call fails with it instead.
type Emitter struct {
	Emitter events.Emitter
	Err     error

	mu        sync.Mutex
	outsideTx int
}

func (e *Emitter) Emit(ctx context.Context, evs ...events.Event) error {
	if !InTx(ctx) {
		e.mu.Lock()
		e.outsideTx += len(evs)
		e.mu.Unlock()
	}
	if e.Err != nil {
		return e.Err
	}
	return e.Emitter.Emit(ctx, evs...)
}

// OutsideTx returns the number of events emitted outside a transaction.
func (e *Emitter) OutsideTx() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.outsideTx
}

// Fixture is a Transactor and an Emitter that stores the events in the outbox of one
// store.
type Fixture struct {
	Tx     *Transactor
	Events *Emitter
	outbox store.OutboxStore
}

// New returns a Fixture for s whose events belong to the org orgId and have the time of
// clk when they name none.
func New(s Store, orgId string, clk clock.PassiveClock) *Fixture {
	return &Fixture{
		Tx: &Transactor{Store: s},
		Events: &Emitter{Emitter: &events.OutboxEmitter{
			Store:   s,
			Orgs:    events.StaticOrgResolver{OrgId: orgId},
			Clock:   clk,
			Product: events.Product,
		}},
		outbox: s,
	}
}

// Engine returns e with the transactions of the Fixture, for code that takes its
// transactor from a store.Engine (the OCPP routers).
func (f *Fixture) Engine(e store.Engine) store.Engine {
	return txEngine{Engine: e, tx: f.Tx}
}

type txEngine struct {
	store.Engine
	tx *Transactor
}

func (e txEngine) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return e.tx.RunInTx(ctx, fn)
}

// Take returns the events that reached the outbox since the last Take, in outbox order,
// and fails the test when an event was emitted outside a transaction. It takes them
// with a relay pass, so they are published afterwards.
func (f *Fixture) Take(t testing.TB) []*store.OutboxEvent {
	t.Helper()
	require.Zero(t, f.Events.OutsideTx(), "every event is emitted inside a store transaction")
	var taken []*store.OutboxEvent
	for {
		n, err := f.outbox.RelayOutboxEvents(context.Background(), store.MaxOutboxEventListLimit,
			func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
				taken = append(taken, evs...)
				return len(evs), nil
			})
		require.NoError(t, err)
		if n < store.MaxOutboxEventListLimit {
			return taken
		}
	}
}

// Data decodes the payload of an event.
func Data[T any](t testing.TB, ev *store.OutboxEvent) T {
	t.Helper()
	var data T
	require.NoError(t, json.Unmarshal(ev.Data, &data), "data of %s: %s", ev.Type, ev.Data)
	return data
}

// Types returns the types of events, in their order.
func Types(evs []*store.OutboxEvent) []string {
	types := make([]string, len(evs))
	for i, ev := range evs {
		types[i] = ev.Type
	}
	return types
}
