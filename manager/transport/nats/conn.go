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
	DefaultConnectionName = "maeve-manager"
	// DefaultConnectTimeout bounds one attempt to connect to a server.
	DefaultConnectTimeout = 5 * time.Second
	// DefaultReconnectWait is the pause between two attempts to reconnect to a server.
	DefaultReconnectWait = 2 * time.Second
	// DefaultMaxReconnects makes the connection retry forever.
	DefaultMaxReconnects = -1
)

// ConnConfig says how to connect to NATS. The credentials and keys are read from the
// files that it names.
type ConnConfig struct {
	Urls           []string
	Name           string        // client name shown in NATS monitoring (default DefaultConnectionName)
	CredsFile      string        // JWT + nkey creds (production)
	NkeySeedFile   string        // nkey seed, for a server that authenticates nkeys without JWTs
	User, Password string        // local development only
	Token          string        // local development only
	TlsCaFile      string        // CAs that sign the certificate of the server
	TlsCertFile    string        // client certificate, with TlsKeyFile
	TlsKeyFile     string        // key of the client certificate
	ConnectTimeout time.Duration // default DefaultConnectTimeout
	ReconnectWait  time.Duration // default DefaultReconnectWait
	// MaxReconnects is how many times the connection tries to reconnect after it was
	// lost, or to connect when the first attempt failed; negative tries forever. The
	// configuration of the manager sets DefaultMaxReconnects unless it says otherwise.
	MaxReconnects int
}

// Connect opens one shared connection (transport, event relay and job leases use it).
//
// It does not fail when no server can be reached: the connection keeps trying in the
// background (nats.RetryOnFailedConnect). Disconnects, reconnects, failed attempts,
// asynchronous errors and slow consumers are logged. Secrets are never logged, and the
// server URL only without its user information.
func Connect(cfg ConnConfig) (*nats.Conn, error) {
	if len(cfg.Urls) == 0 {
		return nil, errors.New("no NATS server URL")
	}
	opts, err := connectOptions(cfg)
	if err != nil {
		return nil, err
	}

	conn, err := nats.Connect(strings.Join(cfg.Urls, ","), opts...)
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

func connectOptions(cfg ConnConfig) ([]nats.Option, error) {
	name := cfg.Name
	if name == "" {
		name = DefaultConnectionName
	}
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = DefaultConnectTimeout
	}
	reconnectWait := cfg.ReconnectWait
	if reconnectWait <= 0 {
		reconnectWait = DefaultReconnectWait
	}

	opts := []nats.Option{
		nats.Name(name),
		nats.Timeout(connectTimeout),
		nats.ReconnectWait(reconnectWait),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.RetryOnFailedConnect(true),
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
		nats.ErrorHandler(logAsyncError),
	}

	if cfg.CredsFile != "" {
		// the creds file is read on every (re)connect: check it now for a clear error
		if _, err := os.Stat(cfg.CredsFile); err != nil {
			return nil, fmt.Errorf("NATS creds file: %w", err)
		}
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}
	if cfg.NkeySeedFile != "" {
		opt, err := nats.NkeyOptionFromSeed(cfg.NkeySeedFile)
		if err != nil {
			return nil, fmt.Errorf("NATS nkey seed file: %w", err)
		}
		opts = append(opts, opt)
	}
	if cfg.User != "" || cfg.Password != "" {
		opts = append(opts, nats.UserInfo(cfg.User, cfg.Password))
	}
	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}
	if cfg.TlsCaFile != "" {
		opts = append(opts, nats.RootCAs(cfg.TlsCaFile))
	}
	if (cfg.TlsCertFile == "") != (cfg.TlsKeyFile == "") {
		return nil, errors.New("NATS TLS client certificate and key must be given together")
	}
	if cfg.TlsCertFile != "" {
		opts = append(opts, nats.ClientCert(cfg.TlsCertFile, cfg.TlsKeyFile))
	}
	return opts, nil
}

func logAsyncError(c *nats.Conn, sub *nats.Subscription, err error) {
	if sub == nil {
		slog.Error("NATS error", "name", c.Opts.Name, "err", err)
		return
	}
	if errors.Is(err, nats.ErrSlowConsumer) {
		pendingMsgs, pendingBytes, _ := sub.Pending()
		dropped, _ := sub.Dropped()
		slog.Warn("NATS slow consumer: the handlers cannot keep up, messages are dropped",
			"name", c.Opts.Name, "subject", sub.Subject, "queue", sub.Queue,
			"pending_msgs", pendingMsgs, "pending_bytes", pendingBytes, "dropped_msgs", dropped)
		return
	}
	slog.Error("NATS subscription error", "name", c.Opts.Name, "subject", sub.Subject, "err", err)
}

// Drain stops the subscriptions of the connection after their pending messages, flushes
// what was published and closes it. It waits for that at most for timeout.
func Drain(conn *nats.Conn, timeout time.Duration) error {
	if conn.IsClosed() {
		return nil
	}
	closed := conn.StatusChanged(nats.CLOSED)
	if err := conn.Drain(); err != nil {
		if errors.Is(err, nats.ErrConnectionClosed) {
			return nil
		}
		return fmt.Errorf("draining the NATS connection: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
		return nil
	case <-timer.C:
		conn.Close()
		return fmt.Errorf("draining the NATS connection: not done after %s, closed it", timeout)
	}
}
