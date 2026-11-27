// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thoughtworks/maeve-csms/gateway/pipe"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"golang.org/x/exp/slog"
	"nhooyr.io/websocket"
)

// The defaults of the keepalive of the station sockets and of the alive events.
const (
	DefaultPingInterval  = 30 * time.Second
	DefaultPongTimeout   = 10 * time.Second
	DefaultAliveInterval = 2 * time.Minute
)

// eventTimeout bounds the publication of one connection event.
const eventTimeout = 5 * time.Second

// WithGatewayId sets the id of the gateway in the connection events (the host name by
// default).
func WithGatewayId(gatewayId string) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.gatewayId = gatewayId
	}
}

// WithPing sets how often the gateway pings every station (DefaultPingInterval by
// default, 0 disables the pings) and how long it waits for the pong
// (DefaultPongTimeout by default). A station that does not answer in time has its
// socket closed.
func WithPing(interval, pongTimeout time.Duration) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.pingInterval = interval
		handler.pongTimeout = pongTimeout
	}
}

// WithAliveInterval sets how often an alive event is published for an open socket
// (DefaultAliveInterval by default, 0 disables the alive events).
func WithAliveInterval(interval time.Duration) WebsocketOpt {
	return func(handler *WebsocketHandler) {
		handler.aliveInterval = interval
	}
}

// DefaultGatewayId is the host name, which is the pod name on Kubernetes; "gateway"
// when the host has no name.
func DefaultGatewayId() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		slog.Warn("no host name for the gateway id", "err", err)
		return "gateway"
	}
	return hostname
}

// stationSocket is an open websocket of a charge station.
type stationSocket struct {
	conn       *websocket.Conn
	opened     time.Time
	id         string
	gatewayId  string
	stationKey string
	protocol   string
	remoteAddr string

	mu sync.Mutex
	// closeReason is why the gateway closed the socket; empty while it has not.
	closeReason string

	// The read loop reads the pongs too, but not while it waits for the pipe to take a
	// message: waitingForPipe is set then, and pipeFreedAt is when it last stopped
	// waiting (time since opened).
	waitingForPipe atomic.Bool
	pipeFreedAt    atomic.Int64
}

// newConnectionId returns a random 128-bit id in hex.
func newConnectionId() string {
	var b [16]byte
	// crypto/rand.Read never returns an error since Go 1.24: it crashes the program
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// remoteAddr is the address of the station: the first entry of X-Forwarded-For when the
// gateway trusts the proxy headers, the address of the connection otherwise.
func remoteAddr(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
		if first = strings.TrimSpace(first); first != "" {
			return first
		}
	}
	return r.RemoteAddr
}

// event returns a connection event of the socket.
func (sock *stationSocket) event(typ string) *transport.ConnectionEvent {
	return &transport.ConnectionEvent{
		Type:         typ,
		StationKey:   sock.stationKey,
		OcppVersion:  sock.protocol,
		ConnectionId: sock.id,
		GatewayId:    sock.gatewayId,
		At:           time.Now().UTC(),
		RemoteAddr:   sock.remoteAddr,
	}
}

// disconnectedEvent returns the disconnected event of a socket whose read loop ended
// with readErr.
func (sock *stationSocket) disconnectedEvent(readErr error) *transport.ConnectionEvent {
	ev := sock.event(transport.ConnectionEventDisconnected)
	ev.CloseCode = int(websocket.CloseStatus(readErr))
	sock.mu.Lock()
	ev.Reason = sock.closeReason
	sock.mu.Unlock()
	if ev.Reason == "" {
		ev.Reason = transport.DisconnectReasonClosed
	}
	return ev
}

// closeFor closes the socket and records why, unless the gateway is closing it already.
// It returns once the close handshake is over, which the websocket library bounds.
func (sock *stationSocket) closeFor(reason string, code websocket.StatusCode, text string) {
	sock.mu.Lock()
	closing := sock.closeReason != ""
	if !closing {
		sock.closeReason = reason
	}
	sock.mu.Unlock()
	if closing {
		return
	}
	_ = sock.conn.Close(code, text)
}

// toPipe hands a message of the station to the pipe and records how long the read loop
// waited for that.
func (sock *stationSocket) toPipe(ch chan<- *pipe.GatewayMessage, msg *pipe.GatewayMessage) {
	select {
	case ch <- msg:
		return
	default:
	}
	sock.waitingForPipe.Store(true)
	ch <- msg
	sock.pipeFreedAt.Store(int64(time.Since(sock.opened)))
	sock.waitingForPipe.Store(false)
}

// waitedForPipeSince reports whether the read loop waits for the pipe now or has waited
// for it since t. A pong may then still be unread.
func (sock *stationSocket) waitedForPipeSince(t time.Time) bool {
	return sock.waitingForPipe.Load() || sock.pipeFreedAt.Load() >= int64(t.Sub(sock.opened))
}

// startHandling counts a connection that the handler serves; it returns false when the
// gateway shuts down and the connection must be refused.
func (s *WebsocketHandler) startHandling() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return false
	}
	s.handlers.Add(1)
	return true
}

// track registers an open socket, so that a shutdown closes it.
func (s *WebsocketHandler) track(sock *stationSocket) {
	s.sockets.Store(sock.id, sock)
	s.mu.Lock()
	shuttingDown := s.shuttingDown
	s.mu.Unlock()
	if shuttingDown {
		// the shutdown may have looked at the sockets before this one was registered
		go sock.closeFor(transport.DisconnectReasonGatewayShutdown, websocket.StatusGoingAway, "gateway shutdown")
	}
}

func (s *WebsocketHandler) untrack(sock *stationSocket) {
	s.sockets.Delete(sock.id)
}

// shutdown refuses new connections, closes every station socket with 1001 "gateway
// shutdown" and waits until their handlers have published the disconnected events and
// returned, at most until ctx is done.
func (s *WebsocketHandler) shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shuttingDown = true
	s.mu.Unlock()

	s.sockets.Range(func(_, value any) bool {
		go value.(*stationSocket).closeFor(transport.DisconnectReasonGatewayShutdown, websocket.StatusGoingAway, "gateway shutdown")
		return true
	})

	done := make(chan struct{})
	go func() {
		s.handlers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for the station sockets to close: %w", ctx.Err())
	}
}

// publish sends a connection event and logs a failure: the events are best effort, and
// neither a failure nor a slow transport holds up the caller for longer than
// eventTimeout.
func publish(session transport.Session, ev *transport.ConnectionEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), eventTimeout)
	defer cancel()

	sent := make(chan error, 1)
	go func() {
		sent <- session.SendConnectionEvent(ctx, ev)
	}()
	var err error
	select {
	case err = <-sent:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		slog.Warn("publishing connection event", "type", ev.Type, "stationKey", ev.StationKey,
			"connectionId", ev.ConnectionId, "err", err)
	}
}

// watch publishes the connected event of the socket, then pings the station and
// publishes alive events until ctx is done or the station does not answer a ping. The
// events of the socket are published one after the other, so they stay in order.
func (s *WebsocketHandler) watch(ctx context.Context, sock *stationSocket, session transport.Session) {
	publish(session, sock.event(transport.ConnectionEventConnected))

	var pings, alives <-chan time.Time
	if s.pingInterval > 0 {
		ticker := time.NewTicker(s.pingInterval)
		defer ticker.Stop()
		pings = ticker.C
	}
	if s.aliveInterval > 0 {
		ticker := time.NewTicker(s.aliveInterval)
		defer ticker.Stop()
		alives = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-pings:
			if !s.ping(ctx, sock) {
				return
			}
		case <-alives:
			// an alive tick during a ping waits here for its outcome: no alive event for a
			// station that does not answer
			publish(session, sock.event(transport.ConnectionEventAlive))
		}
	}
}

// ping pings the station and waits for the pong. It returns false when ctx is done or
// when the station did not answer within the pong timeout; the socket is closed then.
func (s *WebsocketHandler) ping(ctx context.Context, sock *stationSocket) bool {
	// The library closes the socket when the context of a waiting ping ends, so the
	// context only ends here once the outcome is known.
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	answered := make(chan error, 1)
	go func() {
		answered <- sock.conn.Ping(pingCtx)
	}()

	waitFrom := time.Now()
	timer := time.NewTimer(s.pongTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case err := <-answered:
			pingAnswered(sock, err)
			return true
		case <-timer.C:
			select {
			case err := <-answered:
				pingAnswered(sock, err)
				return true
			default:
			}
			if sock.waitedForPipeSince(waitFrom) {
				// the pong may wait behind a message of the station that the pipe had not
				// taken yet: a slow CSMS must not close the socket, so wait another period
				waitFrom = time.Now()
				timer.Reset(s.pongTimeout)
				continue
			}
			slog.Warn("charge station did not answer a ping: closing its websocket",
				"stationKey", sock.stationKey, "connectionId", sock.id, "pongTimeout", s.pongTimeout)
			sock.closeFor(transport.DisconnectReasonPingTimeout, websocket.StatusPolicyViolation, "ping timeout")
			return false
		}
	}
}

// pingAnswered logs a ping that ended with an error before its timeout: the ping could
// not be written (another write took too long), or the socket closed meanwhile. Neither
// shows that the station has gone, so the socket stays as it is.
func pingAnswered(sock *stationSocket, err error) {
	if err != nil {
		slog.Debug("ping ended without a pong", "stationKey", sock.stationKey, "connectionId", sock.id, "err", err)
	}
}
