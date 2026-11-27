// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/subnova/slog-exporter/slogtrace"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"github.com/thoughtworks/maeve-csms/gateway/server"
	"github.com/thoughtworks/maeve-csms/gateway/transport"
	"github.com/thoughtworks/maeve-csms/gateway/transport/mqtt"
	natstransport "github.com/thoughtworks/maeve-csms/gateway/transport/nats"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// transportSettings are the flags that choose and configure the transport to the CSMS.
type transportSettings struct {
	kind              string
	mqttAddr          string
	mqttPrefix        string
	natsUrls          []string
	natsCreds         string
	natsTlsCa         string
	natsName          string
	natsSubjectPrefix string
}

// socketSettings are the flags of the keepalive and the connection events of the
// station sockets, and of the shutdown.
type socketSettings struct {
	gatewayId       string
	pingInterval    time.Duration
	pongTimeout     time.Duration
	aliveInterval   time.Duration
	shutdownTimeout time.Duration
}

// validate rejects negative durations, and a ping without a pong timeout.
func (s socketSettings) validate() error {
	for flag, d := range map[string]time.Duration{
		"--ws-ping-interval":    s.pingInterval,
		"--ws-pong-timeout":     s.pongTimeout,
		"--conn-alive-interval": s.aliveInterval,
		"--shutdown-timeout":    s.shutdownTimeout,
	} {
		if d < 0 {
			return fmt.Errorf("%s must not be negative", flag)
		}
	}
	if s.pingInterval > 0 && s.pongTimeout == 0 {
		return fmt.Errorf("--ws-pong-timeout must be positive while --ws-ping-interval is")
	}
	return nil
}

// options are the options of the websocket handler for the settings.
func (s socketSettings) options() []server.WebsocketOpt {
	return []server.WebsocketOpt{
		server.WithGatewayId(s.gatewayId),
		server.WithPing(s.pingInterval, s.pongTimeout),
		server.WithAliveInterval(s.aliveInterval),
	}
}

var (
	transportFlags           transportSettings
	socketFlags              socketSettings
	wsAddr                   string
	wssAddr                  string
	statusAddr               string
	tlsServerCert            string
	tlsServerKey             string
	tlsTrustCert             []string
	orgNames                 []string
	managerApiAddr           string
	registryCacheTTL         time.Duration
	registryNegativeCacheTTL time.Duration
	trustProxyHeaders        bool
	otelCollectorAddr        string
	logFormat                string
)

// Initializes an OTLP exporter, and configures the corresponding trace and
// metric providers.
func initProvider(collectorAddr string) (func(context.Context) error, error) {
	ctx := context.Background()

	var err error
	var res *resource.Resource
	var traceExporter trace.SpanExporter

	if collectorAddr != "" {
		res, err = resource.New(ctx,
			resource.WithDetectors(gcp.NewDetector()),
			resource.WithTelemetrySDK(),
			resource.WithAttributes(
				// the service name used to display traces in backends
				semconv.ServiceName("maeve-csms-gateway"),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create resource: %w", err)
		}

		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		//nolint:staticcheck // grpc.DialContext is deprecated in newer versions, but we're on v1.56
		conn, err := grpc.DialContext(ctx, collectorAddr,
			// Note the use of insecure transport here. TLS is recommended in production.
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(), //nolint:staticcheck // grpc.WithBlock is deprecated in newer versions
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
		}

		// Set up a trace exporter
		traceExporter, err = otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
	} else {
		res, err = resource.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create resource: %w", err)
		}

		traceExporter, err = slogtrace.New()
		if err != nil {
			return nil, err
		}
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := trace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.AlwaysSample()),
		trace.WithResource(res),
		trace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Shutdown will flush any remaining spans and shut down the exporter.
	return tracerProvider.Shutdown, nil
}

// newTransport builds the transport that the settings choose: "nats" (one shared
// connection) or "mqtt" (one broker connection per station socket).
func newTransport(settings transportSettings, tracer oteltrace.Tracer) (transport.Transport, error) {
	switch settings.kind {
	case "nats":
		if err := natstransport.ValidatePrefix(settings.natsSubjectPrefix); err != nil {
			return nil, err
		}
		conn, err := natstransport.Connect(settings.natsUrls, settings.natsName, settings.natsCreds, settings.natsTlsCa)
		if err != nil {
			return nil, err
		}
		return natstransport.New(conn,
			natstransport.WithSubjectPrefix(settings.natsSubjectPrefix),
			natstransport.WithTracer(tracer)), nil
	case "mqtt":
		brokerUrl, err := url.Parse(settings.mqttAddr)
		if err != nil {
			return nil, fmt.Errorf("parsing mqtt broker url: %v", err)
		}
		return mqtt.New(
			mqtt.WithBrokerURLs(brokerUrl),
			mqtt.WithTopicPrefix(settings.mqttPrefix),
			mqtt.WithTracer(tracer)), nil
	default:
		return nil, fmt.Errorf("unknown transport %q: must be nats or mqtt", settings.kind)
	}
}

// serveUntilDone waits until a server fails or ctx is done (SIGINT or SIGTERM). Then it
// stops accepting connections, closes the station sockets and waits for their
// disconnected events, all within shutdownTimeout. The caller closes the transport
// afterwards.
func serveUntilDone(ctx context.Context, errCh <-chan error, shutdownSockets func(context.Context) error, shutdownTimeout time.Duration, wsServers ...*server.Server) error {
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down: closing the station sockets", "timeout", shutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for _, srv := range wsServers {
		if err := srv.Stop(shutdownCtx); err != nil {
			slog.Warn("stopping the websocket server", "error", err)
		}
	}
	if err := shutdownSockets(shutdownCtx); err != nil {
		slog.Warn("closing the station sockets", "error", err)
	}
	return nil
}

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the gateway server",
	RunE: func(cmd *cobra.Command, args []string) error {
		if logFormat == "json" {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
		}

		shutdown, err := initProvider(otelCollectorAddr)
		if err != nil {
			return err
		}
		defer func() {
			err := shutdown(context.Background())
			if err != nil {
				slog.Error("shutting down OTLP exporter", "error", err)
			}
		}()

		tracer := otel.Tracer("gateway")

		if registryCacheTTL < 0 || registryNegativeCacheTTL < 0 {
			return fmt.Errorf("the registry cache TTLs must not be negative")
		}
		if err := socketFlags.validate(); err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		csmsTransport, err := newTransport(transportFlags, tracer)
		if err != nil {
			return err
		}
		defer func() {
			err := csmsTransport.Close()
			if err != nil {
				slog.Error("closing the transport", "error", err)
			}
		}()

		deviceRegistry := &registry.CachingRegistry{
			Inner: registry.RemoteRegistry{
				ManagerApiAddr: managerApiAddr,
			},
			TTL:         registryCacheTTL,
			NegativeTTL: registryNegativeCacheTTL,
		}
		statusServer := server.New("status", statusAddr, nil, server.NewStatusHandler())
		websocketHandler, shutdownSockets := server.NewWebsocketHandlerWithShutdown(append(socketFlags.options(),
			server.WithTransport(csmsTransport),
			server.WithDeviceRegistry(deviceRegistry),
			server.WithOrgNames(orgNames),
			server.WithTrustProxyHeaders(trustProxyHeaders),
			server.WithOtelTracer(tracer))...)
		wsServer := server.New("ws", wsAddr, nil, websocketHandler)
		wsServers := []*server.Server{wsServer}

		if wssAddr != "" {
			if tlsServerCert == "" {
				return fmt.Errorf("no tls server cert specified for wss connection")
			}
			if tlsServerKey == "" {
				return fmt.Errorf("no tls server key specified for wss connection")
			}

			//#nosec G304 - only files specified by the person running the application will be loaded
			cb, err := os.ReadFile(tlsServerCert)
			if err != nil {
				return fmt.Errorf("reading tls cert from %s: %v", tlsServerCert, err)
			}
			//#nosec G304 - only files specified by the person running the application will be loaded
			kb, err := os.ReadFile(tlsServerKey)
			if err != nil {
				return fmt.Errorf("reading tls key from %s: %v", tlsServerKey, err)
			}
			tlsCert, err := tls.X509KeyPair(cb, kb)
			if err != nil {
				return fmt.Errorf("processing tls key pair: %v", err)
			}
			trustedCerts := x509.NewCertPool()
			for _, tc := range tlsTrustCert {
				//#nosec G304 - only files specified by the person running the application will be loaded
				tcb, err := os.ReadFile(tc)
				if err != nil {
					return fmt.Errorf("reading trusted certs from %s: %v", tc, err)
				}
				if ok := trustedCerts.AppendCertsFromPEM(tcb); !ok {
					return fmt.Errorf("processing trusted certs from %s: no certificate found", tc)
				}
			}
			tlsConfig := &tls.Config{
				Certificates: []tls.Certificate{tlsCert},
				ClientCAs:    trustedCerts,
				ClientAuth:   tls.VerifyClientCertIfGiven,
				MinVersion:   tls.VersionTLS12,
			}

			wsServers = append(wsServers, server.New("wss", wssAddr, tlsConfig, websocketHandler))
		}

		// room for an error of every server: none of them waits to report one
		errCh := make(chan error, len(wsServers)+1)
		for _, srv := range wsServers {
			srv.Start(errCh)
		}
		statusServer.Start(errCh)

		return serveUntilDone(ctx, errCh, shutdownSockets, socketFlags.shutdownTimeout, wsServers...)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)

	serveCmd.Flags().StringVar(&transportFlags.kind, "transport", "nats",
		"How the gateway exchanges messages with the CSMS manager, one of [nats, mqtt]; the manager must use the same (its [transport] type)")
	serveCmd.Flags().StringSliceVar(&transportFlags.natsUrls, "nats-url", []string{natstransport.DefaultURL},
		"A comma-separated list of NATS server URLs, e.g. nats://127.0.0.1:4222 (with --transport nats)")
	serveCmd.Flags().StringVar(&transportFlags.natsCreds, "nats-creds", "",
		"A file with the user JWT and nkey seed to authenticate to NATS (with --transport nats)")
	serveCmd.Flags().StringVar(&transportFlags.natsTlsCa, "nats-tls-ca", "",
		"A file with the PEM encoded CA certificates that sign the certificate of the NATS server (with --transport nats)")
	serveCmd.Flags().StringVar(&transportFlags.natsName, "nats-name", natstransport.DefaultConnectionName,
		"The client name of the gateway that NATS monitoring shows (with --transport nats)")
	serveCmd.Flags().StringVar(&transportFlags.natsSubjectPrefix, "nats-subject-prefix", natstransport.DefaultSubjectPrefix,
		"The first token of the NATS subjects; the manager must use the same (its transport.nats.subject_prefix) (with --transport nats)")
	serveCmd.Flags().StringVarP(&transportFlags.mqttAddr, "mqtt-addr", "m", "mqtt://127.0.0.1:1883",
		"The address of the MQTT broker, e.g. mqtt://127.0.0.1:1883 (with --transport mqtt)")
	serveCmd.Flags().StringVar(&transportFlags.mqttPrefix, "mqtt-prefix", "cs",
		"The first level of the MQTT topics; the manager must use the same (its transport.mqtt.prefix) (with --transport mqtt)")
	serveCmd.Flags().StringVarP(&wsAddr, "ws-addr", "a", "127.0.0.1:9310",
		"The address that the insecure websocket server will listen on for connections, e.g. 127.0.0.1:9310")
	serveCmd.Flags().StringVarP(&wssAddr, "wss-addr", "w", "",
		"The address that the secure websocket server will listen on for connections, e.g. 127.0.0.1:9311")
	serveCmd.Flags().StringVarP(&statusAddr, "status-addr", "s", "127.0.0.1:9312",
		"The address that the status server will listen on for connections, e.g. 127.0.0.1:9312")
	serveCmd.Flags().StringVarP(&tlsServerCert, "tls-server-cert", "c", "",
		"A file that contains a PEM encoded certificate to use as the TLS server cert")
	serveCmd.Flags().StringVarP(&tlsServerKey, "tls-server-key", "k", "",
		"A file that contains a PEM encoded private key to use as the TLS server key")
	serveCmd.Flags().StringArrayVarP(&tlsTrustCert, "tls-trust-cert", "t", []string{},
		"A file that contains a PEM encoded certificate to add to the TLS trust store")
	serveCmd.Flags().StringSliceVarP(&orgNames, "org-name", "o", []string{"Thoughtworks"},
		"A comma-separated list of organisation names that are valid in client certificates. "+
			"Deprecated: single-tenant mode only. It is only used for a charge station whose tenant has no list of its own (accepted_cert_org_names of the tenant config)")
	serveCmd.Flags().StringVarP(&managerApiAddr, "manager-api-addr", "r", "http://127.0.0.1:9412",
		"The internal address of the CSMS manager (its [api] internal_addr, not the public API), e.g. http://127.0.0.1:9412")
	serveCmd.Flags().DurationVar(&registryCacheTTL, "registry-cache-ttl", 10*time.Second,
		"How long the auth details of a charge station that the manager returned are kept, 0 to always ask the manager")
	serveCmd.Flags().DurationVar(&registryNegativeCacheTTL, "registry-negative-cache-ttl", 2*time.Second,
		"How long it is kept that the manager does not know a host or a charge station, 0 to always ask the manager")
	serveCmd.Flags().BoolVar(&trustProxyHeaders, "trust-proxy", false,
		"Trust proxy headers when determining the client's TLS status")
	serveCmd.Flags().StringVar(&otelCollectorAddr, "otel-collector-addr", "",
		"The address of the open telemetry collector that will receive traces, e.g. localhost:4317")
	serveCmd.Flags().StringVar(&logFormat, "log-format", "text",
		"The format of the logs, one of [text, json]")
	serveCmd.Flags().StringVar(&socketFlags.gatewayId, "gateway-id", server.DefaultGatewayId(),
		"The id of this gateway in the connection events of the station sockets (default: the host name, the pod name on Kubernetes)")
	serveCmd.Flags().DurationVar(&socketFlags.pingInterval, "ws-ping-interval", server.DefaultPingInterval,
		"How often the gateway pings each charge station over its websocket, 0 to send no pings")
	serveCmd.Flags().DurationVar(&socketFlags.pongTimeout, "ws-pong-timeout", server.DefaultPongTimeout,
		"How long the gateway waits for the pong of a charge station before it closes the websocket")
	serveCmd.Flags().DurationVar(&socketFlags.aliveInterval, "conn-alive-interval", server.DefaultAliveInterval,
		"How often the gateway publishes an alive event for each open station websocket, 0 to publish none; must be well below the manager's connectivity.stale_after")
	serveCmd.Flags().DurationVar(&socketFlags.shutdownTimeout, "shutdown-timeout", 10*time.Second,
		"How long the gateway waits on SIGINT or SIGTERM for the station websockets to close and their disconnected events to be published")
}
