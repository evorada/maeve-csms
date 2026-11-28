// SPDX-License-Identifier: Apache-2.0

// Package connectivity keeps track of whether the websockets of the charge stations are
// open, from the connection events of the gateways and the messages of the stations.
package connectivity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

const (
	// DefaultSeenWriteEvery is how often, at most, the messages of one station update
	// its LastSeenAt.
	DefaultSeenWriteEvery = 30 * time.Second
	// DefaultStaleAfter is how long a connected station may show no sign of life before
	// the sweep marks it disconnected.
	DefaultStaleAfter = 5 * time.Minute
	// DefaultSweepInterval is the pause between two sweeps.
	DefaultSweepInterval = 30 * time.Second
	// DefaultGatewayAliveInterval is the default interval of the gateway's alive events
	// (its --conn-alive-interval flag). StaleAfter must exceed the interval that the
	// gateways use, with a margin for delivery and clock skew.
	DefaultGatewayAliveInterval = 2 * time.Minute
)

// SweepJobName is the name of the job that SweepJob returns.
const SweepJobName = "connectivity-sweep"

// ConnectionObserver is told about each station whose new connection the Tracker
// recorded.
type ConnectionObserver interface {
	// OnStationConnected must return at once: the connection events of the stations
	// wait for it.
	OnStationConnected(ctx context.Context, stationKey string)
}

// Tracker records the connectivity of the charge stations in the store. It never delays
// or fails the handling of OCPP messages: store errors are logged. A station that the
// sweep marks disconnected is not disconnected anywhere; only its status changes.
//
// Every change of the connectivity of a station is stored in one transaction with its
// domain event (station.connected or station.disconnected): when the event cannot be
// stored, the change is not stored either.
//
// A Tracker must not be copied after first use.
type Tracker struct {
	Store store.ConnectivityStore
	// Tx runs a change and its event in one transaction.
	Tx store.Transactor
	// Events receives the events of the changes.
	Events events.Emitter
	// Clock is nil for the real clock.
	Clock clock.PassiveClock
	// SeenWriteEvery limits the LastSeenAt writes from OCPP messages to one per station
	// per interval; 0 is DefaultSeenWriteEvery.
	SeenWriteEvery time.Duration
	// StaleAfter is how long a connected station may show no sign of life before the
	// sweep marks it disconnected; 0 is DefaultStaleAfter.
	StaleAfter time.Duration
	// OnConnected, when set, is told about each connection that was recorded, after
	// its transaction.
	OnConnected ConnectionObserver

	mu sync.Mutex
	// lastWrite is when this replica last wrote the LastSeenAt of a station.
	lastWrite map[string]time.Time
	// notImplemented logs, once, that the store does not track connectivity.
	notImplemented sync.Once
}

var _ transport.ConnectionEventHandler = (*Tracker)(nil)

// HandleConnectionEvent records a connection event of a gateway. Whether a connected or
// disconnected event still applies is decided by the store, from the connection id and
// the time of the event, so late and duplicated events do no harm. Only an event that
// applies emits a domain event.
func (t *Tracker) HandleConnectionEvent(ctx context.Context, ev *transport.ConnectionEvent) {
	log := slog.With("station", ev.StationKey, "connection_id", ev.ConnectionId, "gateway_id", ev.GatewayId)

	switch ev.Type {
	case transport.ConnectionEventConnected:
		applied, err := t.changeAndEmit(ctx, func(ctx context.Context) (bool, error) {
			return t.Store.RecordStationConnected(ctx, ev.StationKey, ev.ConnectionId, ev.At)
		}, connectedEvent(ev))
		if err != nil {
			t.storeFailed(log, "recording station connected", err)
			return
		}
		if !applied {
			log.Debug("ignored connected event: a newer connection is recorded", "at", ev.At)
			return
		}
		t.wrote(ev.StationKey)
		log.Info("station connected", "at", ev.At, "remote_addr", ev.RemoteAddr)
		if t.OnConnected != nil {
			t.OnConnected.OnStationConnected(ctx, ev.StationKey)
		}
	case transport.ConnectionEventAlive:
		err := t.Store.TouchStationLastSeen(ctx, ev.StationKey, ev.At)
		if err != nil {
			t.storeFailed(log, "recording station alive", err)
			return
		}
		t.wrote(ev.StationKey)
	case transport.ConnectionEventDisconnected:
		t.forget(ev.StationKey)
		applied, err := t.changeAndEmit(ctx, func(ctx context.Context) (bool, error) {
			return t.Store.RecordStationDisconnected(ctx, ev.StationKey, ev.ConnectionId, ev.At)
		}, disconnectedEvent(ev))
		if err != nil {
			t.storeFailed(log, "recording station disconnected", err)
			return
		}
		if !applied {
			log.Debug("ignored disconnected event: not the recorded connection", "at", ev.At)
			return
		}
		log.Info("station disconnected", "at", ev.At, "reason", ev.Reason, "close_code", ev.CloseCode)
	default:
		log.Warn("ignored connection event of unknown type", "type", ev.Type)
	}
}

// changeAndEmit runs change and, when it applied, emits event, in one transaction.
func (t *Tracker) changeAndEmit(ctx context.Context, change func(ctx context.Context) (bool, error), event events.Event) (applied bool, err error) {
	err = t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		applied, err = change(ctx)
		if err != nil || !applied {
			return err
		}
		return t.Events.Emit(ctx, event)
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

// connectedEvent is the station.connected event of a connected event of a gateway.
func connectedEvent(ev *transport.ConnectionEvent) events.Event {
	return events.Event{
		Type:         events.TypeStationConnected,
		StationKey:   ev.StationKey,
		ResourceType: events.ResourceStation,
		ResourceId:   ev.StationKey,
		Actor:        events.StationActor(ev.StationKey),
		Time:         ev.At,
		Data: events.StationConnectedData{
			StationKey:   ev.StationKey,
			StationId:    events.StationId(ev.StationKey),
			OcppVersion:  ev.OcppVersion,
			ConnectionId: ev.ConnectionId,
			ConnectedAt:  ev.At.UTC(),
		},
	}
}

// disconnectedEvent is the station.disconnected event of a disconnected event of a
// gateway.
func disconnectedEvent(ev *transport.ConnectionEvent) events.Event {
	reason := ev.Reason
	if reason == "" {
		reason = transport.DisconnectReasonClosed
	}
	return events.Event{
		Type:         events.TypeStationDisconnected,
		StationKey:   ev.StationKey,
		ResourceType: events.ResourceStation,
		ResourceId:   ev.StationKey,
		Actor:        events.StationActor(ev.StationKey),
		Time:         ev.At,
		Data: events.StationDisconnectedData{
			StationKey:     ev.StationKey,
			StationId:      events.StationId(ev.StationKey),
			ConnectionId:   ev.ConnectionId,
			DisconnectedAt: ev.At.UTC(),
			Reason:         reason,
			CloseCode:      ev.CloseCode,
		},
	}
}

// WrapHandler returns a handler that hands every message to next and then records that
// the station was seen, at most once per SeenWriteEvery for each station. next is called
// first, so the response to the station is not delayed.
func (t *Tracker) WrapHandler(next transport.MessageHandler) transport.MessageHandler {
	return transport.MessageHandlerFunc(func(ctx context.Context, chargeStationId string, message *transport.Message) {
		next.Handle(ctx, chargeStationId, message)
		t.seen(ctx, chargeStationId)
	})
}

// SweepStale marks the connected stations that have shown no sign of life for
// StaleAfter as disconnected: their gateway vanished without telling. Each of them gets
// a station.disconnected event with the reason "stale", in the same transaction.
func (t *Tracker) SweepStale(ctx context.Context) error {
	now := t.now()
	var ids []string
	err := t.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		ids, err = t.Store.DisconnectStaleStations(ctx, now.Add(-t.staleAfter()), now)
		if err != nil {
			return err
		}
		staleEvents := make([]events.Event, len(ids))
		for i, id := range ids {
			staleEvents[i] = staleEvent(id, now)
		}
		return t.Events.Emit(ctx, staleEvents...)
	})
	if errors.Is(err, store.ErrNotImplemented) {
		t.warnNotImplemented()
		return nil
	}
	if err != nil {
		return fmt.Errorf("disconnecting stale stations: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	t.forget(ids...)
	slog.Info("marked stations without a sign of life disconnected",
		"count", len(ids), "stations", ids, "stale_after", t.staleAfter())
	return nil
}

// staleEvent is the station.disconnected event of a station that the sweep marked
// disconnected at now. The manager made that change, not the station.
func staleEvent(stationKey string, now time.Time) events.Event {
	return events.Event{
		Type:         events.TypeStationDisconnected,
		StationKey:   stationKey,
		ResourceType: events.ResourceStation,
		ResourceId:   stationKey,
		Actor:        events.DefaultActor,
		Time:         now,
		Data: events.StationDisconnectedData{
			StationKey:     stationKey,
			StationId:      events.StationId(stationKey),
			DisconnectedAt: now.UTC(),
			Reason:         events.DisconnectReasonStale,
		},
	}
}

// SweepJob returns the job that runs SweepStale every interval.
func (t *Tracker) SweepJob(interval time.Duration) jobs.Job {
	return jobs.Job{
		Name:     SweepJobName,
		Interval: interval,
		Run:      t.SweepStale,
	}
}

// seen writes the LastSeenAt of a station unless this replica wrote it less than
// SeenWriteEvery ago.
func (t *Tracker) seen(ctx context.Context, chargeStationId string) {
	now := t.now()

	t.mu.Lock()
	last, ok := t.lastWrite[chargeStationId]
	if ok && now.Sub(last) < t.seenWriteEvery() {
		t.mu.Unlock()
		return
	}
	t.setLastWrite(chargeStationId, now)
	t.mu.Unlock()

	err := t.Store.TouchStationLastSeen(ctx, chargeStationId, now)
	if err != nil {
		t.storeFailed(slog.With("station", chargeStationId), "recording station seen", err)
	}
}

// wrote notes that the LastSeenAt of a station was written now.
func (t *Tracker) wrote(chargeStationId string) {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setLastWrite(chargeStationId, now)
}

// setLastWrite must be called with mu held.
func (t *Tracker) setLastWrite(chargeStationId string, at time.Time) {
	if t.lastWrite == nil {
		t.lastWrite = make(map[string]time.Time)
	}
	t.lastWrite[chargeStationId] = at
}

// forget drops what this replica knows about the writes of disconnected stations.
func (t *Tracker) forget(chargeStationIds ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range chargeStationIds {
		delete(t.lastWrite, id)
	}
}

func (t *Tracker) storeFailed(log *slog.Logger, msg string, err error) {
	if errors.Is(err, store.ErrNotImplemented) {
		t.warnNotImplemented()
		return
	}
	log.Error(msg, "err", err)
}

func (t *Tracker) warnNotImplemented() {
	t.notImplemented.Do(func() {
		slog.Warn("station connectivity is not tracked: the storage does not implement it")
	})
}

func (t *Tracker) now() time.Time {
	if t.Clock == nil {
		return time.Now()
	}
	return t.Clock.Now()
}

func (t *Tracker) seenWriteEvery() time.Duration {
	if t.SeenWriteEvery <= 0 {
		return DefaultSeenWriteEvery
	}
	return t.SeenWriteEvery
}

func (t *Tracker) staleAfter() time.Duration {
	if t.StaleAfter <= 0 {
		return DefaultStaleAfter
	}
	return t.StaleAfter
}
