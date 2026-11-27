// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/propagation"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// Product is the product that the domain events of the manager belong to. It is the
// product part of the URN of a resource.
const Product = "cpms"

// DefaultActor is the actor of an event that names none and has no charge station.
const DefaultActor = "svc:maeve"

var (
	// typePattern is a type of at least two dot-separated words, e.g. "session.completed".
	typePattern = regexp.MustCompile(`^[a-z][a-z_]*(\.[a-z][a-z_]*)+$`)
	// orgIdPattern is an org id that can be one token of a NATS subject.
	orgIdPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Event is one domain event as produced by business code.
type Event struct {
	Type         string    // short type, e.g. "session.completed"
	OrgId        string    // owning org; resolved from StationKey when empty
	StationKey   string    // "{org_key}.{stationId}"; used for OrgId and the default actor
	ResourceType string    // "station", "connector", "session", "command"
	ResourceId   string    // e.g. station key, "<station_key>#<connector>", transaction id
	Actor        string    // optional: "station:<key>", "user:<sub>", "svc:<name>"
	Time         time.Time // optional, defaults to Clock.Now()
	Data         any       // JSON-marshalled payload (required; use struct{}{} for none)
}

// Emitter appends events to the outbox. Call it with the ctx of a store transaction
// (store.Transactor.RunInTx) so the events commit or roll back with the state change.
// The events of one call are stored all or none.
type Emitter interface {
	Emit(ctx context.Context, events ...Event) error
}

var (
	_ Emitter = (*OutboxEmitter)(nil)
	_ Emitter = NopEmitter{}
)

// OutboxEmitter is the Emitter that stores the events in the outbox of the store. It
// must not be copied after first use.
type OutboxEmitter struct {
	Store   store.OutboxStore
	Orgs    OrgResolver
	Clock   clock.PassiveClock // nil is the real clock
	Product string             // Product
	NewId   func() string      // nil is a new UUIDv7

	// noOutbox logs, once, that the store has no outbox.
	noOutbox sync.Once
}

// Emit checks the events and stores them with one AppendOutboxEvents call. When one of
// them is not valid or its org cannot be found, none is stored and the error returned.
func (e *OutboxEmitter) Emit(ctx context.Context, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	if e.Product == "" {
		return errors.New("emit events: the emitter has no product")
	}

	traceParent := traceParentFrom(ctx)
	ctxActor := ActorFrom(ctx)
	outbox := make([]*store.OutboxEvent, len(events))
	for i, ev := range events {
		stored, err := e.outboxEvent(ctx, ev, traceParent, ctxActor)
		if err != nil {
			return fmt.Errorf("emit event %q: %w", ev.Type, err)
		}
		outbox[i] = stored
	}

	err := e.Store.AppendOutboxEvents(ctx, outbox...)
	if errors.Is(err, store.ErrNotImplemented) {
		e.noOutbox.Do(func() {
			slog.Warn("domain events are dropped: the storage has no outbox", "err", err)
		})
		return nil
	}
	if err != nil {
		return fmt.Errorf("emit events: %w", err)
	}
	return nil
}

func (e *OutboxEmitter) outboxEvent(ctx context.Context, ev Event, traceParent, ctxActor string) (*store.OutboxEvent, error) {
	if !typePattern.MatchString(ev.Type) {
		return nil, errors.New("invalid event type")
	}
	if ev.ResourceType == "" || ev.ResourceId == "" {
		return nil, errors.New("the event has no resource type or no resource id")
	}

	orgId, err := e.orgId(ctx, ev)
	if err != nil {
		return nil, err
	}
	if !orgIdPattern.MatchString(orgId) {
		return nil, fmt.Errorf("invalid org id %q", orgId)
	}

	data, err := json.Marshal(ev.Data)
	if err != nil {
		return nil, fmt.Errorf("marshal data: %w", err)
	}
	if string(data) == "null" {
		return nil, errors.New("the event has no data (struct{}{} is an event without data)")
	}

	id, err := e.newId()
	if err != nil {
		return nil, err
	}

	eventTime := ev.Time
	if eventTime.IsZero() {
		eventTime = e.now()
	}

	return &store.OutboxEvent{
		Id:          id,
		OrgId:       orgId,
		Type:        ev.Type,
		Subject:     URN(e.Product, orgId, ev.ResourceType, ev.ResourceId),
		Actor:       actorOf(ev, ctxActor),
		TraceParent: traceParent,
		Time:        eventTime.UTC(),
		Data:        data,
	}, nil
}

// orgId returns the org of an event: its own, else the org of its charge station.
func (e *OutboxEmitter) orgId(ctx context.Context, ev Event) (string, error) {
	if ev.OrgId != "" {
		return ev.OrgId, nil
	}
	if ev.StationKey == "" {
		return "", fmt.Errorf("the event has neither an org id nor a station key: %w", ErrUnknownOrg)
	}
	if e.Orgs == nil {
		return "", errors.New("the emitter has no org resolver")
	}
	orgId, err := e.Orgs.OrgIdForStation(ctx, ev.StationKey)
	if err != nil {
		return "", fmt.Errorf("org of charge station %s: %w", ev.StationKey, err)
	}
	if orgId == "" {
		return "", fmt.Errorf("org of charge station %s: %w", ev.StationKey, ErrUnknownOrg)
	}
	return orgId, nil
}

func (e *OutboxEmitter) newId() (string, error) {
	if e.NewId != nil {
		return e.NewId(), nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("new event id: %w", err)
	}
	return id.String(), nil
}

func (e *OutboxEmitter) now() time.Time {
	if e.Clock == nil {
		return time.Now()
	}
	return e.Clock.Now()
}

// actorOf returns who caused the event: the actor of the event, else the actor of the
// ctx, else its charge station, else DefaultActor.
func actorOf(ev Event, ctxActor string) string {
	switch {
	case ev.Actor != "":
		return ev.Actor
	case ctxActor != "":
		return ctxActor
	case ev.StationKey != "":
		return StationActor(ev.StationKey)
	default:
		return DefaultActor
	}
}

// traceParentFrom returns the W3C traceparent of the span in ctx, or "" when ctx has no
// valid span.
func traceParentFrom(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// NopEmitter drops every event. It is the Emitter when events are disabled or the
// storage has no outbox (Firestore).
type NopEmitter struct{}

func (NopEmitter) Emit(context.Context, ...Event) error {
	return nil
}
