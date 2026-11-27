// SPDX-License-Identifier: Apache-2.0

package nats_test

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"github.com/thoughtworks/maeve-csms/manager/transport/nats/natstest"
)

func TestConnectRefusesBadSettings(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for name, cfg := range map[string]natstransport.ConnConfig{
		"no url":                {},
		"creds file missing":    {Urls: []string{"nats://127.0.0.1:4222"}, CredsFile: missing},
		"nkey seed missing":     {Urls: []string{"nats://127.0.0.1:4222"}, NkeySeedFile: missing},
		"tls ca missing":        {Urls: []string{"nats://127.0.0.1:4222"}, TlsCaFile: missing},
		"tls cert without key":  {Urls: []string{"nats://127.0.0.1:4222"}, TlsCertFile: missing},
		"tls key without cert":  {Urls: []string{"nats://127.0.0.1:4222"}, TlsKeyFile: missing},
		"tls client cert files": {Urls: []string{"nats://127.0.0.1:4222"}, TlsCertFile: missing, TlsKeyFile: missing},
	} {
		t.Run(name, func(t *testing.T) {
			conn, err := natstransport.Connect(cfg)
			assert.Error(t, err)
			assert.Nil(t, conn)
		})
	}
}

func TestConnectKeepsTryingWhenNoServerIsReachable(t *testing.T) {
	// a port that nothing listens on
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "nats://" + l.Addr().String()
	require.NoError(t, l.Close())

	conn, err := natstransport.Connect(natstransport.ConnConfig{Urls: []string{url}, MaxReconnects: -1, ReconnectWait: 10 * time.Millisecond})
	require.NoError(t, err)
	defer conn.Close()
	assert.False(t, conn.IsConnected())
	assert.False(t, conn.IsClosed())
}

func TestConnectWithToken(t *testing.T) {
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.Authorization = "s3cret-token"
	srv := natsserver.RunServer(&opts)
	defer srv.Shutdown()

	conn, err := natstransport.Connect(natstransport.ConnConfig{Urls: []string{srv.ClientURL()}, Token: "s3cret-token"})
	require.NoError(t, err)
	defer conn.Close()
	assert.True(t, conn.IsConnected())
	assert.Equal(t, natstransport.DefaultConnectionName, conn.Opts.Name)
}

func TestConnectWithUserAndPassword(t *testing.T) {
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.Username = "manager"
	opts.Password = "s3cret-password"
	srv := natsserver.RunServer(&opts)
	defer srv.Shutdown()

	conn, err := natstransport.Connect(natstransport.ConnConfig{Urls: []string{srv.ClientURL()}, User: "manager", Password: "s3cret-password", Name: "replica-1"})
	require.NoError(t, err)
	defer conn.Close()
	assert.True(t, conn.IsConnected())
	assert.Equal(t, "replica-1", conn.Opts.Name)
}

func TestConnectDoesNotLogSecrets(t *testing.T) {
	logs := captureLogs(t)
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.Authorization = "the-right-token"
	srv := natsserver.RunServer(&opts)
	defer srv.Shutdown()

	// the server URL carries a password too
	url := "nats://someone:url-password@" + srv.Addr().String()
	conn, err := natstransport.Connect(natstransport.ConnConfig{
		Urls:          []string{url},
		Token:         "a-wrong-token",
		MaxReconnects: 2,
		ReconnectWait: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	defer conn.Close()
	require.Eventually(t, conn.IsClosed, waitTimeout, 10*time.Millisecond, "the connection gives up after the attempts")

	assert.Contains(t, logs.String(), "NATS connection attempt failed")
	assert.NotContains(t, logs.String(), "a-wrong-token")
	assert.NotContains(t, logs.String(), "url-password")
}

func TestDrainSendsWhatIsPendingAndClosesTheConnection(t *testing.T) {
	_, url := natstest.RunServer(t, false)
	conn := connect(t, url)
	subscriber := connect(t, url)
	sub, err := subscriber.SubscribeSync("ocpp.out.ocpp16.k7f2.CP001")
	require.NoError(t, err)
	require.NoError(t, subscriber.Flush())

	require.NoError(t, conn.Publish("ocpp.out.ocpp16.k7f2.CP001", []byte("last")))
	require.NoError(t, natstransport.Drain(conn, waitTimeout))
	assert.True(t, conn.IsClosed())

	got, err := sub.NextMsg(waitTimeout)
	require.NoError(t, err)
	assert.Equal(t, "last", string(got.Data))

	// a closed connection needs nothing more
	assert.NoError(t, natstransport.Drain(conn, waitTimeout))
	assert.ErrorIs(t, conn.Publish("x", nil), nats.ErrConnectionClosed)
}
