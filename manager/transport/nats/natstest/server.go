// SPDX-License-Identifier: Apache-2.0

// Package natstest runs an embedded NATS server for tests. Only tests import it, so
// the manager binary does not contain the server.
package natstest

import (
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
)

// RunServer starts an embedded NATS server on a random port (JetStream optional,
// store dir t.TempDir()) and shuts it down with t.Cleanup.
func RunServer(t testing.TB, jetStream bool) (*server.Server, string) {
	t.Helper()

	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	if jetStream {
		opts.JetStream = true
		opts.StoreDir = t.TempDir()
	}
	srv := natsserver.RunServer(&opts)
	t.Cleanup(func() {
		srv.Shutdown()
		srv.WaitForShutdown()
	})
	return srv, srv.ClientURL()
}
