// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// connectivityT0 is the time of the first event of the connectivity tests. Every time
// is a whole second, so that PostgreSQL stores it exactly.
var connectivityT0 = time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)

// atSecond is connectivityT0 plus the given number of seconds.
func atSecond(seconds int) time.Time {
	return connectivityT0.Add(time.Duration(seconds) * time.Second)
}

func getStatus(t *testing.T, s store.Engine, chargeStationId string) *store.ChargeStationStatus {
	t.Helper()
	status, err := s.GetChargeStationStatus(context.Background(), chargeStationId)
	require.NoError(t, err)
	require.NotNil(t, status)
	return status
}

func assertTime(t *testing.T, want time.Time, got *time.Time, field string) {
	t.Helper()
	if assert.NotNil(t, got, field) {
		assert.True(t, want.Equal(*got), "%s: want %s, got %s", field, want, *got)
	}
}

func assertConnection(t *testing.T, want string, status *store.ChargeStationStatus) {
	t.Helper()
	if assert.NotNil(t, status.ConnectionId, "ConnectionId") {
		assert.Equal(t, want, *status.ConnectionId)
	}
}

func connect(t *testing.T, s store.Engine, chargeStationId, connectionId string, when time.Time) bool {
	t.Helper()
	applied, err := s.RecordStationConnected(context.Background(), chargeStationId, connectionId, when)
	require.NoError(t, err)
	return applied
}

func disconnect(t *testing.T, s store.Engine, chargeStationId, connectionId string, when time.Time) bool {
	t.Helper()
	applied, err := s.RecordStationDisconnected(context.Background(), chargeStationId, connectionId, when)
	require.NoError(t, err)
	return applied
}

// TestConnectivityStore checks the rules of a ConnectivityStore: a connection event
// applies only when no newer connection is recorded, and a disconnection only to the
// connection that is recorded.
func TestConnectivityStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("connect records the connection of a station without a status", func(t *testing.T) {
		s := newStore(t)
		// a station key of the widest kind: org key, dot, station id (80 characters)
		csId := "k7f2." + strings.Repeat("x", 75)

		assert.True(t, connect(t, s, csId, "conn-a", atSecond(0)))

		status := getStatus(t, s, csId)
		assert.Equal(t, csId, status.ChargeStationId)
		assert.True(t, status.Connected)
		assertTime(t, atSecond(0), status.LastConnectedAt, "LastConnectedAt")
		assertTime(t, atSecond(0), status.LastSeenAt, "LastSeenAt")
		assertConnection(t, "conn-a", status)
		assert.Nil(t, status.LastDisconnectedAt)
	})

	t.Run("connect keeps what boot notification stored", func(t *testing.T) {
		s := newStore(t)
		heartbeat := atSecond(-60)
		require.NoError(t, s.SetChargeStationStatus(ctx, "cs001", &store.ChargeStationStatus{
			ChargeStationId: "cs001",
			Connected:       false,
			LastHeartbeat:   &heartbeat,
			Model:           ptr("Model X"),
			Vendor:          ptr("Vendor Y"),
			UpdatedAt:       heartbeat,
		}))

		assert.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))

		status := getStatus(t, s, "cs001")
		assert.True(t, status.Connected)
		assertTime(t, heartbeat, status.LastHeartbeat, "LastHeartbeat")
		assert.Equal(t, ptr("Model X"), status.Model)
		assert.Equal(t, ptr("Vendor Y"), status.Vendor)
		assertConnection(t, "conn-a", status)
	})

	t.Run("disconnect of the recorded connection", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))

		assert.False(t, disconnect(t, s, "cs001", "conn-b", atSecond(10)), "another connection")
		status := getStatus(t, s, "cs001")
		assert.True(t, status.Connected)
		assert.Nil(t, status.LastDisconnectedAt)

		assert.True(t, disconnect(t, s, "cs001", "conn-a", atSecond(10)))
		status = getStatus(t, s, "cs001")
		assert.False(t, status.Connected)
		assertTime(t, atSecond(10), status.LastDisconnectedAt, "LastDisconnectedAt")
		assertTime(t, atSecond(0), status.LastConnectedAt, "LastConnectedAt")
		assertConnection(t, "conn-a", status)

		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(20)), "a station that is not connected")
		assertTime(t, atSecond(10), getStatus(t, s, "cs001").LastDisconnectedAt, "LastDisconnectedAt")
	})

	t.Run("disconnect of a station without a status", func(t *testing.T) {
		s := newStore(t)
		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(0)))
		_, err := s.GetChargeStationStatus(ctx, "cs001")
		assert.ErrorIs(t, err, store.ErrNotFound)
		assert.EqualError(t, err, "charge station cs001 not found")
	})

	t.Run("a disconnection from before the connection does not apply", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(10)))

		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(5)))
		assert.True(t, getStatus(t, s, "cs001").Connected)
	})

	t.Run("a late disconnection of an old socket does not disconnect the new one", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))
		// the station reconnects through another socket before the old one is reported closed
		require.True(t, connect(t, s, "cs001", "conn-b", atSecond(20)))

		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(10)))
		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(30)), "however late the old socket closed")

		status := getStatus(t, s, "cs001")
		assert.True(t, status.Connected)
		assertConnection(t, "conn-b", status)
		assertTime(t, atSecond(20), status.LastConnectedAt, "LastConnectedAt")
		assert.Nil(t, status.LastDisconnectedAt)
	})

	t.Run("an older connection after a newer one does not apply", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-b", atSecond(20)))

		assert.False(t, connect(t, s, "cs001", "conn-a", atSecond(10)))

		status := getStatus(t, s, "cs001")
		assertConnection(t, "conn-b", status)
		assertTime(t, atSecond(20), status.LastConnectedAt, "LastConnectedAt")
		assertTime(t, atSecond(20), status.LastSeenAt, "LastSeenAt")

		// and the newer one can still be disconnected
		assert.True(t, disconnect(t, s, "cs001", "conn-b", atSecond(30)))
	})

	t.Run("reconnect after a disconnection", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))
		require.True(t, disconnect(t, s, "cs001", "conn-a", atSecond(10)))

		assert.True(t, connect(t, s, "cs001", "conn-b", atSecond(20)))

		status := getStatus(t, s, "cs001")
		assert.True(t, status.Connected)
		assertConnection(t, "conn-b", status)
		assertTime(t, atSecond(20), status.LastConnectedAt, "LastConnectedAt")
		assertTime(t, atSecond(10), status.LastDisconnectedAt, "LastDisconnectedAt")
	})

	t.Run("a connection does not move the last seen time backwards", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))
		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(50)))

		require.True(t, connect(t, s, "cs001", "conn-b", atSecond(40)))

		status := getStatus(t, s, "cs001")
		assertTime(t, atSecond(40), status.LastConnectedAt, "LastConnectedAt")
		assertTime(t, atSecond(50), status.LastSeenAt, "LastSeenAt")
	})

	t.Run("last seen never moves backwards", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))

		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(30)))
		assertTime(t, atSecond(30), getStatus(t, s, "cs001").LastSeenAt, "LastSeenAt")

		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(20)))
		assertTime(t, atSecond(30), getStatus(t, s, "cs001").LastSeenAt, "LastSeenAt")

		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(30)))
		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(40)))
		status := getStatus(t, s, "cs001")
		assertTime(t, atSecond(40), status.LastSeenAt, "LastSeenAt")
		assert.True(t, status.Connected, "touching changes nothing else")
		assertConnection(t, "conn-a", status)
	})

	t.Run("last seen of a station with a status but no connection", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.UpdateHeartbeat(ctx, "cs001", atSecond(0)))

		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(10)))

		status := getStatus(t, s, "cs001")
		assertTime(t, atSecond(10), status.LastSeenAt, "LastSeenAt")
		assert.Nil(t, status.ConnectionId)
	})

	t.Run("last seen of a station without a status is not recorded", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(0)))
		_, err := s.GetChargeStationStatus(ctx, "cs001")
		assert.Error(t, err)
	})

	t.Run("disconnect stale stations", func(t *testing.T) {
		s := newStore(t)
		now := atSecond(600)
		seenBefore := atSecond(300)

		// connected, last seen recently
		require.True(t, connect(t, s, "fresh", "conn-1", atSecond(0)))
		require.NoError(t, s.TouchStationLastSeen(ctx, "fresh", atSecond(500)))
		// connected, nothing since the connection
		require.True(t, connect(t, s, "stale", "conn-2", atSecond(100)))
		// connected long ago, but a heartbeat came recently
		require.True(t, connect(t, s, "heartbeating", "conn-3", atSecond(0)))
		require.NoError(t, s.UpdateHeartbeat(ctx, "heartbeating", atSecond(400)))
		// connected only through its heartbeats, the last one long ago
		require.NoError(t, s.UpdateHeartbeat(ctx, "heartbeat-only", atSecond(200)))
		// stale, but its socket was already reported closed
		require.True(t, connect(t, s, "closed", "conn-4", atSecond(0)))
		require.True(t, disconnect(t, s, "closed", "conn-4", atSecond(100)))
		// connected, but with no time at all to decide by
		require.NoError(t, s.SetChargeStationStatus(ctx, "no-times", &store.ChargeStationStatus{
			ChargeStationId: "no-times",
			Connected:       true,
			UpdatedAt:       atSecond(0),
		}))
		// last seen exactly at the limit
		require.True(t, connect(t, s, "at-limit", "conn-5", atSecond(300)))

		ids, err := s.DisconnectStaleStations(ctx, seenBefore, now)
		require.NoError(t, err)
		assert.Equal(t, []string{"heartbeat-only", "stale"}, ids)

		for _, csId := range ids {
			status := getStatus(t, s, csId)
			assert.False(t, status.Connected, csId)
			assertTime(t, now, status.LastDisconnectedAt, csId+" LastDisconnectedAt")
		}
		for _, csId := range []string{"fresh", "heartbeating", "no-times", "at-limit"} {
			status := getStatus(t, s, csId)
			assert.True(t, status.Connected, csId)
			assert.Nil(t, status.LastDisconnectedAt, csId)
		}
		closed := getStatus(t, s, "closed")
		assert.False(t, closed.Connected)
		assertTime(t, atSecond(100), closed.LastDisconnectedAt, "closed LastDisconnectedAt")

		ids, err = s.DisconnectStaleStations(ctx, seenBefore, now)
		require.NoError(t, err)
		assert.Empty(t, ids, "a second sweep finds nothing")

		// the socket of a swept station closes later: it is no longer connected, so the
		// time of the sweep stays
		assert.False(t, disconnect(t, s, "stale", "conn-2", atSecond(700)))
		assertTime(t, now, getStatus(t, s, "stale").LastDisconnectedAt, "stale LastDisconnectedAt")
	})

	t.Run("boot notification keeps the connectivity fields", func(t *testing.T) {
		s := newStore(t)
		require.True(t, connect(t, s, "cs001", "conn-a", atSecond(0)))
		require.NoError(t, s.TouchStationLastSeen(ctx, "cs001", atSecond(5)))
		require.True(t, disconnect(t, s, "cs001", "conn-a", atSecond(10)))
		require.True(t, connect(t, s, "cs001", "conn-b", atSecond(20)))

		boot := atSecond(21)
		require.NoError(t, s.SetChargeStationStatus(ctx, "cs001", &store.ChargeStationStatus{
			ChargeStationId: "cs001",
			Connected:       true,
			LastHeartbeat:   &boot,
			FirmwareVersion: ptr("1.2.3"),
			UpdatedAt:       boot,
		}))

		status := getStatus(t, s, "cs001")
		assert.True(t, status.Connected)
		assertTime(t, boot, status.LastHeartbeat, "LastHeartbeat")
		assert.Equal(t, ptr("1.2.3"), status.FirmwareVersion)
		assertTime(t, atSecond(20), status.LastConnectedAt, "LastConnectedAt")
		assertTime(t, atSecond(10), status.LastDisconnectedAt, "LastDisconnectedAt")
		assertTime(t, atSecond(20), status.LastSeenAt, "LastSeenAt")
		assertConnection(t, "conn-b", status)

		// and the old socket still cannot disconnect the station
		assert.False(t, disconnect(t, s, "cs001", "conn-a", atSecond(30)))
	})

	t.Run("concurrent events of one station end in the newest connection", func(t *testing.T) {
		s := newStore(t)
		const stations = 10
		const sockets = 8

		// every station connects through `sockets` sockets at increasing times, and every
		// socket but the last one is reported closed; all events at the same time, in
		// any order
		var wg sync.WaitGroup
		for st := 0; st < stations; st++ {
			csId := fmt.Sprintf("cs%03d", st)
			for sock := 0; sock < sockets; sock++ {
				connId := fmt.Sprintf("conn-%d", sock)
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := s.RecordStationConnected(ctx, csId, connId, atSecond(10*sock))
					assert.NoError(t, err)
				}()
				if sock < sockets-1 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, err := s.RecordStationDisconnected(ctx, csId, connId, atSecond(10*sock+5))
						assert.NoError(t, err)
					}()
				}
			}
		}
		wg.Wait()

		for st := 0; st < stations; st++ {
			csId := fmt.Sprintf("cs%03d", st)
			status := getStatus(t, s, csId)
			assert.True(t, status.Connected, csId)
			assertConnection(t, fmt.Sprintf("conn-%d", sockets-1), status)
			assertTime(t, atSecond(10*(sockets-1)), status.LastConnectedAt, csId+" LastConnectedAt")
			assertTime(t, atSecond(10*(sockets-1)), status.LastSeenAt, csId+" LastSeenAt")
		}
	})
}

func ptr[T any](v T) *T {
	return &v
}
