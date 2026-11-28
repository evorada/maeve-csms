// SPDX-License-Identifier: Apache-2.0

package nats

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/exp/slog"
)

const (
	// DefaultConnectionName is the client name that NATS monitoring shows.
	DefaultConnectionName = "maeve-gateway"
	// DefaultURL is the NATS server of a local installation.
	DefaultURL = "nats://127.0.0.1:4222"

	reconnectWait = 2 * time.Second
)

// Connect opens the connection to NATS that all station sockets share. name is the
// client name that NATS monitoring shows (DefaultConnectionName when empty); credsFile
// holds the user JWT and nkey seed, and tlsCaFile the CAs that sign the certificate of
// the server, both optional.
//
// It does not fail when no server can be reached: the connection keeps trying in the
// background, forever. Disconnects, reconnects, failed attempts and asynchronous errors
// are logged; server URLs only without their user information.
func Connect(urls []string, name, credsFile, tlsCaFile string) (*nats.Conn, error) {
	if len(urls) == 0 {
		return nil, errors.New("no NATS server URL")
	}
	if name == "" {
		name = DefaultConnectionName
	}

	opts := []nats.Option{
		nats.Name(name),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(reconnectWait),
		nats.ConnectHandler(func(c *nats.Conn) {
			slog.Info("connected to NATS", "url", c.ConnectedUrlRedacted(), "name", c.Opts.Name)
		}),
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			if err != nil {
				slog.Warn("lost the NATS connection", "name", c.Opts.Name, "err", err)
			}
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			slog.Info("reconnected to NATS", "url", c.ConnectedUrlRedacted(), "name", c.Opts.Name)
		}),
		nats.ReconnectErrHandler(func(c *nats.Conn, err error) {
			slog.Warn("NATS connection attempt failed", "name", c.Opts.Name, "err", err)
		}),
		nats.ClosedHandler(func(c *nats.Conn) {
			slog.Info("NATS connection closed", "name", c.Opts.Name)
		}),
		nats.ErrorHandler(func(c *nats.Conn, sub *nats.Subscription, err error) {
			if sub == nil {
				slog.Error("NATS error", "name", c.Opts.Name, "err", err)
				return
			}
			slog.Error("NATS subscription error", "name", c.Opts.Name, "subject", sub.Subject, "err", err)
		}),
	}
	if credsFile != "" {
		// the creds file is read on every (re)connect: check it now for a clear error
		if _, err := os.Stat(credsFile); err != nil {
			return nil, fmt.Errorf("NATS creds file: %w", err)
		}
		opts = append(opts, nats.UserCredentials(credsFile))
	}
	if tlsCaFile != "" {
		opts = append(opts, nats.RootCAs(tlsCaFile))
	}

	conn, err := nats.Connect(strings.Join(urls, ","), opts...)
	if err != nil {
		return nil, fmt.Errorf("connecting to NATS: %w", err)
	}
	if conn.IsConnected() {
		slog.Info("connected to NATS", "url", conn.ConnectedUrlRedacted(), "name", conn.Opts.Name)
	} else {
		slog.Warn("no NATS server reachable yet: connecting in the background", "name", conn.Opts.Name)
	}
	return conn, nil
}
