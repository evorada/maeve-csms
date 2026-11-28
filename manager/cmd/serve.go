// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/sync"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

var (
	configFile string
)

// natsDrainTimeout bounds the wait for the NATS connection to send what is pending
// when the manager stops.
const natsDrainTimeout = 10 * time.Second

// jobsStopTimeout bounds the wait for the background jobs to return when the manager
// stops.
const jobsStopTimeout = 10 * time.Second

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the server",
	Long: `Starts the server which will subscribe to messages from
the gateway and send appropriate responses.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// cancelled when the manager stops (and when the command returns): stops the background jobs
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cfg := config.DefaultConfig
		if configFile != "" {
			err := cfg.LoadFromFile(configFile)
			if err != nil {
				return err
			}
		}

		settings, err := config.Configure(context.Background(), &cfg)
		if err != nil {
			return err
		}
		defer func() {
			err := settings.TracerProvider.Shutdown(context.Background())
			if err != nil {
				slog.Warn("shutting down tracer provider", "error", err)
			}
		}()

		apiServer := server.New("api", cfg.Api.Addr, nil,
			server.NewApiHandler(server.ApiDeps{
				Engine:       settings.Storage,
				Ocpi:         settings.OcpiApi,
				Verifier:     settings.AuthVerifier,
				Scoper:       settings.TenantScoper,
				Entitlements: settings.Entitlements,
				Events:       settings.Events,
			}))
		internalServer := server.New("internal", cfg.Api.InternalAddr, nil,
			server.NewInternalHandler(server.InternalDeps{
				Resolver:       settings.TenantResolver,
				Engine:         settings.Storage,
				Verifier:       settings.AuthVerifier,
				Scoper:         settings.TenantScoper,
				Settings:       settings.Api,
				CSCertProvider: settings.ChargeStationCertProviderService,
				DefaultOrgId:   settings.DefaultOrgId,
			}))

		// every OCPP message is a sign of life of its station (Connectivity is nil when the
		// storage does not track connectivity)
		ocpp16Handler, ocpp201Handler := settings.Ocpp16Handler, settings.Ocpp201Handler
		if settings.Connectivity != nil {
			if ocpp16Handler != nil {
				ocpp16Handler = settings.Connectivity.WrapHandler(ocpp16Handler)
			}
			if ocpp201Handler != nil {
				ocpp201Handler = settings.Connectivity.WrapHandler(ocpp201Handler)
			}
		}

		jobList := backgroundJobs(settings)
		jobsDone := make(chan struct{})
		go func() {
			defer close(jobsDone)
			if err := settings.JobRunner.Run(ctx, jobList); err != nil {
				slog.Error("running background jobs", "err", err)
			}
		}()

		// one slot for each sender below, so that none of them blocks when more than one fails
		errCh := make(chan error, 6)
		apiServer.Start(errCh)
		internalServer.Start(errCh)
		var ocpp16Connection transport.Connection
		if ocpp16Handler != nil {
			ocpp16Connection, err = settings.MsgListener.Connect(context.Background(), transport.OcppVersion16, nil, ocpp16Handler)
			if err != nil {
				errCh <- err
			}
		}

		var ocpp201Connection transport.Connection
		if ocpp201Handler != nil {
			ocpp201Connection, err = settings.MsgListener.Connect(context.Background(), transport.OcppVersion201, nil, ocpp201Handler)
			if err != nil {
				errCh <- err
			}
		}

		// the connection events of the gateways, when the transport carries them
		var connectionEvents transport.Connection
		if listener, ok := settings.MsgListener.(transport.ConnectionEventListener); ok && settings.Connectivity != nil {
			connectionEvents, err = listener.ListenConnectionEvents(context.Background(), settings.Connectivity)
			if err != nil {
				errCh <- err
			}
		}

		if settings.OcpiApi != nil {
			ocpiServer := server.New("ocpi", cfg.Ocpi.Addr, nil, server.NewOcpiHandler(settings.Storage, clock.RealClock{}, settings.OcpiApi, settings.MsgEmitter))
			ocpiServer.Start(errCh)
		}

		// SIGTERM is how Kubernetes stops a pod. Until it arrives, a second signal would be
		// swallowed too, so the handler is removed as soon as the first one is seen: a second
		// signal then ends the process at once.
		signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err = waitForStop(signals, errCh)
		stopSignals()

		if ocpp16Connection != nil {
			err := ocpp16Connection.Disconnect(context.Background())
			if err != nil {
				slog.Warn("disconnecting from broker", "err", err)
			}
		}
		if ocpp201Connection != nil {
			err := ocpp201Connection.Disconnect(context.Background())
			if err != nil {
				slog.Warn("disconnecting from broker", "err", err)
			}
		}
		if connectionEvents != nil {
			err := connectionEvents.Disconnect(context.Background())
			if err != nil {
				slog.Warn("disconnecting the connection events from broker", "err", err)
			}
		}

		// after the listeners: what their handlers published is still sent
		stopJobsAndDrainNats(cancel, jobsDone, settings.NatsConn, jobsStopTimeout, natsDrainTimeout)

		return err
	},
}

// waitForStop waits until a server fails (its error is returned) or until signals is
// done because the manager was asked to stop (nil is returned). Either way the caller
// then disconnects the listeners, stops the background jobs, which releases their leases,
// and drains NATS.
func waitForStop(signals context.Context, errCh <-chan error) error {
	select {
	case err := <-errCh:
		return err
	case <-signals.Done():
		slog.Info("stop signal received, shutting down")
		return nil
	}
}

// backgroundJobs returns the periodic jobs of the manager. The connectivity sweep, the
// relay and the purge of the outbox are left out when the storage or the configuration
// has no use for them.
func backgroundJobs(settings *config.Config) []jobs.Job {
	jobList := sync.Jobs(settings.Storage, clock.RealClock{}, settings.Tracer, settings.MsgEmitter,
		settings.SyncInterval, settings.SyncRetryAfter)
	jobList = append(jobList, jobs.ReservationExpiryJob(settings.Storage, settings.ReservationExpiryInterval))
	if settings.Connectivity != nil {
		jobList = append(jobList, settings.Connectivity.SweepJob(settings.ConnectivitySweepInterval))
	}
	// the domain events go from the outbox to JetStream, and are deleted after the
	// retention period
	if settings.EventRelay != nil {
		jobList = append(jobList, settings.EventRelay.Job(settings.EventRelayInterval))
	}
	if settings.OutboxPurge != nil {
		jobList = append(jobList, events.PurgeJob(settings.Storage, clock.RealClock{},
			settings.OutboxPurge.Retention, settings.OutboxPurge.Interval))
	}
	return jobList
}

// stopJobsAndDrainNats stops the background jobs, waits for them at most jobsTimeout, and
// then drains the NATS connection (when there is one). The jobs stop first: the relay
// publishes on the connection and what it published before it stopped is marked in the
// outbox, and the leased runner releases the leases of its jobs on it, so that another
// replica takes them over at once.
func stopJobsAndDrainNats(stopJobs context.CancelFunc, jobsDone <-chan struct{}, natsConn *nats.Conn, jobsTimeout, drainTimeout time.Duration) {
	stopJobs()
	select {
	case <-jobsDone:
	case <-time.After(jobsTimeout):
		slog.Warn("the background jobs did not stop in time", "timeout", jobsTimeout)
	}

	if natsConn != nil {
		err := natstransport.Drain(natsConn, drainTimeout)
		if err != nil {
			slog.Warn("closing the NATS connection", "err", err)
		}
	}
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVarP(&configFile, "config-file", "c", "/config/config.toml",
		"The config file to use")
}
