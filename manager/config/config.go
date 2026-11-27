// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/subnova/slog-exporter/slogtrace"
	"github.com/thoughtworks/maeve-csms/manager/connectivity"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	mqtt2 "github.com/thoughtworks/maeve-csms/manager/transport/mqtt"
	natstransport "github.com/thoughtworks/maeve-csms/manager/transport/nats"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.19.0"
	oteltrace "go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/utils/clock"
)

type ApiSettings struct {
	Addr    string
	Host    string
	WsPort  int
	WssPort int
	OrgName string
	// EnableTransactionsPage and EnableAdminUI switch on the legacy pages, which are
	// served on the internal listener without authentication.
	EnableTransactionsPage bool
	EnableAdminUI          bool
}

type Config struct {
	Api                              ApiSettings
	Tracer                           oteltrace.Tracer
	TracerProvider                   *trace.TracerProvider
	Storage                          store.Engine
	MsgEmitter                       transport.Emitter
	MsgListener                      transport.Listener
	Ocpp16Handler                    transport.MessageHandler
	Ocpp201Handler                   transport.MessageHandler
	ContractCertValidationService    services.CertificateValidationService
	ContractCertProviderService      services.ContractCertificateProvider
	ChargeStationCertProviderService services.ChargeStationCertificateProvider
	TariffService                    services.TariffService
	OcpiApi                          ocpi.Api
	JobRunner                        jobs.Runner
	ReservationExpiryInterval        time.Duration
	// DefaultOrgId is the org that owns the rows from before orgs and everything that
	// is created without a tenant.
	DefaultOrgId string
	// Tenancy says how charge stations are mapped to orgs.
	Tenancy tenancy.Mode
	// TenantResolver gives the identity of a charge station inside the manager and the
	// org that owns it.
	TenantResolver *tenancy.Resolver
	// TenantScoper says which orgs a caller of the API may reach.
	TenantScoper *tenancy.Scoper
	// TenantConfig gives the config of a tenant to the code that applies it.
	TenantConfig *tenancy.ConfigProvider
	// Entitlements enforces the hard limits of the entitlements of a tenant when
	// something is created.
	Entitlements *entitlements.Enforcer
	// AuthVerifier says who the caller of an API request is.
	AuthVerifier tenancy.Verifier
	// Events stores the domain events in the outbox of the storage, in the transaction of
	// the change they describe.
	Events events.Emitter
	// NatsConn is the one connection to NATS, nil when nothing uses NATS. Whoever stops
	// the manager drains it after the listeners have disconnected.
	NatsConn *nats.Conn
	// Connectivity records whether the websockets of the charge stations are open, from
	// the connection events of the gateways and the OCPP messages. It is nil when the
	// storage does not track connectivity (Firestore).
	Connectivity *connectivity.Tracker
	// ConnectivitySweepInterval is the pause between two runs of the job that marks the
	// stations without a sign of life disconnected.
	ConnectivitySweepInterval time.Duration
	// EventRelay publishes the events of the outbox to JetStream. It is nil when the
	// events or the relay are disabled and with Firestore storage.
	EventRelay *events.Relay
	// EventRelayInterval is the pause between two runs of the relay job.
	EventRelayInterval time.Duration
	// OutboxPurge says when the published events are deleted from the outbox. It is nil
	// when the storage has no outbox (Firestore).
	OutboxPurge *OutboxPurgeSettings
}

// devSubject is the caller of every API request in dev mode.
const devSubject = "user:dev"

// getAuthVerifier creates what authenticates the callers of the API: the verifier of
// the tokens of the platform, or in dev mode one that accepts every request.
func getAuthVerifier(ctx context.Context, cfg *AuthConfig, defaultOrgId string) (tenancy.Verifier, error) {
	switch cfg.Mode {
	case "jwt":
		minRefreshInterval, err := time.ParseDuration(cfg.MinRefreshInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to parse api auth min refresh interval: %s", err)
		}
		if minRefreshInterval <= 0 {
			return nil, fmt.Errorf("api auth min refresh interval must be positive: %s", cfg.MinRefreshInterval)
		}
		clockSkew, err := time.ParseDuration(cfg.ClockSkew)
		if err != nil {
			return nil, fmt.Errorf("failed to parse api auth clock skew: %s", err)
		}
		if clockSkew < 0 {
			return nil, fmt.Errorf("api auth clock skew must not be negative: %s", cfg.ClockSkew)
		}
		maxTokenLifetime, err := time.ParseDuration(cfg.MaxTokenLifetime)
		if err != nil {
			return nil, fmt.Errorf("failed to parse api auth max token lifetime: %s", err)
		}
		if maxTokenLifetime <= 0 {
			return nil, fmt.Errorf("api auth max token lifetime must be positive: %s", cfg.MaxTokenLifetime)
		}

		verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
			JWKSURL:            cfg.JWKSURL,
			Issuer:             cfg.Issuer,
			Audience:           cfg.Audience,
			MinRefreshInterval: minRefreshInterval,
			ClockSkew:          clockSkew,
			MaxLifetime:        maxTokenLifetime,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create the api token verifier: %w", err)
		}
		return verifier, nil
	case "dev":
		if os.Getenv("ENVIRONMENT") != "dev" {
			return nil, errors.New(`api auth mode "dev" needs the ENVIRONMENT environment variable to be "dev"`)
		}

		principal := tenancy.Principal{
			Subject:  devSubject,
			OrgId:    cfg.Dev.OrgId,
			OrgScope: tenancy.OrgScope(cfg.Dev.OrgScope),
			Perms:    cfg.Dev.Perms,
		}
		if principal.OrgId == "" {
			principal.OrgId = defaultOrgId
		}
		if principal.Perms == nil {
			principal.Perms = slices.Clone(tenancy.AllPermissions)
		}

		slog.Warn("API authentication disabled (dev mode)",
			"sub", principal.Subject, "org", principal.OrgId, "org_scope", string(principal.OrgScope))
		return tenancy.DevVerifier{Principal: principal}, nil
	default:
		return nil, fmt.Errorf("unknown api auth mode: %s", cfg.Mode)
	}
}

// legacyOrgAssigner is implemented by the storage engines that store the org of a row.
type legacyOrgAssigner interface {
	AssignLegacyOrg(ctx context.Context, orgId string) (int64, error)
}

// assignLegacyOrg gives the rows of the legacy org to the default org, when another org
// than the legacy one is configured and the storage engine stores the org of a row.
func assignLegacyOrg(ctx context.Context, engine store.Engine, defaultOrgId string) error {
	if defaultOrgId == store.LegacyOrgId {
		return nil
	}
	assigner, ok := engine.(legacyOrgAssigner)
	if !ok {
		return nil
	}
	rows, err := assigner.AssignLegacyOrg(ctx, defaultOrgId)
	if err != nil {
		return fmt.Errorf("failed to assign the legacy org rows to the default org: %w", err)
	}
	slog.Info("assigned the rows of the legacy org to the default org", "org_id", defaultOrgId, "rows", rows)
	return nil
}

// configureTenants checks that the storage engine can serve the tenancy mode and, in
// multi mode, puts the configured bootstrap tenants in the tenant store.
func configureTenants(ctx context.Context, cfg *TenancyConfig, storageType string, engine store.Engine) error {
	switch tenancy.Mode(cfg.Mode) {
	case tenancy.ModeSingle:
		// the host of a station is ignored in single mode: tenants that are configured
		// here would not be isolated from each other
		if len(cfg.BootstrapTenants) > 0 {
			return fmt.Errorf("tenancy bootstrap tenants need tenancy mode %q", tenancy.ModeMulti)
		}
		return nil
	case tenancy.ModeMulti:
		_, err := engine.GetTenant(ctx, store.LegacyOrgId)
		if errors.Is(err, store.ErrNotImplemented) {
			return fmt.Errorf("tenancy mode %q is not supported by the %s storage", tenancy.ModeMulti, storageType)
		}
		if err != nil {
			return fmt.Errorf("failed to read the tenant store: %w", err)
		}

		bootstrapTenants := make([]tenancy.BootstrapTenant, 0, len(cfg.BootstrapTenants))
		for _, t := range cfg.BootstrapTenants {
			bootstrapTenants = append(bootstrapTenants, tenancy.BootstrapTenant{
				OrgId:     t.OrgId,
				Slug:      t.Slug,
				OrgKey:    t.OrgKey,
				Hostnames: t.Hostnames,
			})
		}
		err = tenancy.Bootstrap(ctx, engine, bootstrapTenants)
		if err != nil {
			return fmt.Errorf("failed to bootstrap tenants: %w", err)
		}
		if len(bootstrapTenants) > 0 {
			slog.Info("bootstrapped tenants", "count", len(bootstrapTenants))
		}
		return nil
	default:
		return fmt.Errorf("unknown tenancy mode: %s", cfg.Mode)
	}
}

func Configure(ctx context.Context, cfg *BaseConfig) (c *Config, err error) {
	err = cfg.Validate()
	if err != nil {
		return nil, err
	}

	// the NATS connection of a configuration that fails is closed again
	var natsConn *nats.Conn
	defer func() {
		if err != nil && natsConn != nil {
			natsConn.Close()
		}
	}()

	heartbeatInterval, err := time.ParseDuration(cfg.Ocpp.HeartbeatInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to parse heartbeat interval: %s", err)
	}

	reservationExpiryInterval, err := time.ParseDuration(cfg.Jobs.ReservationExpiryInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to parse reservation expiry interval: %s", err)
	}
	if reservationExpiryInterval <= 0 {
		return nil, fmt.Errorf("reservation expiry interval must be positive: %s", cfg.Jobs.ReservationExpiryInterval)
	}

	tenantCacheTTL, err := time.ParseDuration(cfg.Tenancy.CacheTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tenancy cache ttl: %s", err)
	}
	if tenantCacheTTL <= 0 {
		return nil, fmt.Errorf("tenancy cache ttl must be positive: %s", cfg.Tenancy.CacheTTL)
	}

	c = &Config{
		Api: ApiSettings{
			Addr:    cfg.Api.Addr,
			Host:    cfg.Api.Host,
			WsPort:  cfg.Api.WsPort,
			WssPort: cfg.Api.WssPort,
			OrgName: cfg.Api.OrgName,

			EnableTransactionsPage: cfg.Api.EnableTransactionsPage,
			EnableAdminUI:          cfg.Api.EnableAdminUI,
		},
		ReservationExpiryInterval: reservationExpiryInterval,
		DefaultOrgId:              cfg.Tenancy.DefaultOrgId,
		Tenancy:                   tenancy.Mode(cfg.Tenancy.Mode),
	}

	switch cfg.Observability.LogFormat {
	case "json":
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	case "text":
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	default:
		return nil, fmt.Errorf("unknown log format: %s", cfg.Observability.LogFormat)
	}

	// after the log format: it may warn
	connSettings, err := parseConnectivityConfig(&cfg.Connectivity)
	if err != nil {
		return nil, err
	}
	c.ConnectivitySweepInterval = connSettings.sweepInterval

	evSettings, err := parseEventsConfig(&cfg.Events)
	if err != nil {
		return nil, err
	}
	c.EventRelayInterval = evSettings.relayInterval

	c.AuthVerifier, err = getAuthVerifier(ctx, &cfg.Api.Auth, c.DefaultOrgId)
	if err != nil {
		return nil, err
	}

	httpClient, err := getHttpClient(cfg.Observability.TlsKeylogFile)
	if err != nil {
		return nil, err
	}

	c.TracerProvider, err = getTracerProvider(ctx, cfg.Observability.OtelCollectorAddr)
	if err != nil {
		return nil, err
	}

	c.Tracer = c.TracerProvider.Tracer("manager")

	c.JobRunner = &jobs.LocalRunner{Clock: clock.RealClock{}, Tracer: c.Tracer}

	c.Storage, err = getStorage(ctx, &cfg.Storage)
	if err != nil {
		return nil, err
	}

	err = assignLegacyOrg(ctx, c.Storage, c.DefaultOrgId)
	if err != nil {
		return nil, err
	}

	err = configureTenants(ctx, &cfg.Tenancy, cfg.Storage.Type, c.Storage)
	if err != nil {
		return nil, err
	}

	c.TenantResolver = &tenancy.Resolver{
		Mode:         c.Tenancy,
		Tenants:      c.Storage,
		Stations:     c.Storage,
		DefaultOrgId: c.DefaultOrgId,
		Clock:        clock.RealClock{},
		CacheTTL:     tenantCacheTTL,
	}
	c.Events = getEventEmitter(&cfg.Events, cfg.Storage.Type, c.Storage, c.TenantResolver)
	c.OutboxPurge = getOutboxPurge(evSettings, cfg.Storage.Type)
	c.Connectivity = getConnectivityTracker(connSettings, cfg.Storage.Type, c.Storage, c.Events)
	c.TenantConfig = &tenancy.ConfigProvider{Resolver: c.TenantResolver}
	c.TenantScoper = &tenancy.Scoper{
		Mode:         c.Tenancy,
		Tenants:      c.Storage,
		Resolver:     c.TenantResolver,
		DefaultOrgId: c.DefaultOrgId,
	}
	c.Entitlements = &entitlements.Enforcer{
		Tenants: c.Storage,
		Counter: c.Storage,
		Mode:    c.Tenancy,
	}

	c.ContractCertValidationService, err = getContractCertValidator(&cfg.ContractCertValidator, httpClient)
	if err != nil {
		return nil, err
	}

	c.ContractCertProviderService, err = getContractCertProvider(&cfg.ContractCertProvider, httpClient)
	if err != nil {
		return nil, err
	}

	c.ChargeStationCertProviderService, err = getChargeStationCertProvider(ctx, &cfg.ChargeStationCertProvider, c.Storage, httpClient)
	if err != nil {
		return nil, err
	}

	c.TariffService, err = getTariffService(&cfg.TariffService)
	if err != nil {
		return nil, err
	}

	if cfg.usesNats() {
		natsConn, err = getNatsConn(cfg.Nats)
		if err != nil {
			return nil, err
		}
		c.NatsConn = natsConn
	}

	if cfg.relaysEvents() {
		c.EventRelay, err = getEventRelay(ctx, &cfg.Events, evSettings, c.Storage, c.NatsConn, c.Tracer)
		if err != nil {
			return nil, err
		}
	} else if cfg.Events.Enabled && cfg.Storage.Type != "firestore" {
		slog.Warn("the relay of the domain events is disabled: the events stay in the outbox")
	}

	c.MsgEmitter, err = getMsgEmitter(&cfg.Transport, c.Tracer, c.NatsConn)
	if err != nil {
		return nil, err
	}

	c.MsgListener, err = getMsgListener(&cfg.Transport, c.Tracer, c.NatsConn)
	if err != nil {
		return nil, err
	}

	if cfg.Ocpp.Ocpp16Enabled {
		c.Ocpp16Handler = ocpp16.NewRouter(c.MsgEmitter,
			clock.RealClock{},
			c.Storage,
			c.Events,
			c.TenantResolver,
			c.TenantConfig,
			c.ContractCertValidationService,
			c.ChargeStationCertProviderService,
			c.ContractCertProviderService,
			heartbeatInterval,
			schemas.OcppSchemas)
	}
	if cfg.Ocpp.Ocpp201Enabled {
		c.Ocpp201Handler = ocpp201.NewRouter(c.MsgEmitter,
			clock.RealClock{},
			c.Storage,
			c.TenantResolver,
			c.TenantConfig,
			c.TariffService,
			c.ContractCertValidationService,
			c.ChargeStationCertProviderService,
			c.ContractCertProviderService,
			heartbeatInterval,
			schemas.OcppSchemas)
	}

	if cfg.Ocpi != nil {
		c.OcpiApi, err = getOcpiApi(cfg.Ocpi, c.Storage, httpClient, c.DefaultOrgId)
		if err != nil {
			return nil, err
		}
	}

	return
}

// getEventEmitter creates what business code gives its domain events to: the outbox of
// the storage, with the org of an event's charge station given by the tenant resolver.
// Events are off when they are disabled and with Firestore storage, which has no outbox.
func getEventEmitter(cfg *EventsConfig, storageType string, engine store.Engine, orgs events.OrgResolver) events.Emitter {
	if !cfg.Enabled {
		slog.Info("domain events disabled")
		return events.NopEmitter{}
	}
	if storageType == "firestore" {
		slog.Warn("domain events disabled: the firestore storage has no outbox")
		return events.NopEmitter{}
	}
	return &events.OutboxEmitter{
		Store:   engine,
		Orgs:    orgs,
		Clock:   clock.RealClock{},
		Product: events.Product,
	}
}

func getOcpiApi(o *OcpiConfig, engine store.Engine, httpClient *http.Client, defaultOrgId string) (ocpi.Api, error) {
	api := ocpi.NewOCPI(engine, httpClient, o.CountryCode, o.PartyId, defaultOrgId)
	api.SetExternalUrl(o.ExternalURL)
	return api, nil
}

func getHttpClient(keylogFile string) (*http.Client, error) {
	var httpTransport http.RoundTripper

	if keylogFile != "" {
		slog.Warn("***** TLS key logging enabled *****")

		//#nosec G304 - only files specified by the person running the application will be used
		keyLog, err := os.OpenFile(keylogFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return nil, fmt.Errorf("opening key log file: %v", err)
		}

		baseTransport := http.DefaultTransport.(*http.Transport).Clone()
		baseTransport.TLSClientConfig = &tls.Config{
			KeyLogWriter: keyLog,
			MinVersion:   tls.VersionTLS12,
		}

		httpTransport = otelhttp.NewTransport(baseTransport)
	} else {
		httpTransport = otelhttp.NewTransport(http.DefaultTransport)
	}

	return &http.Client{Transport: httpTransport}, nil
}

func getStorage(ctx context.Context, cfg *StorageConfig) (engine store.Engine, err error) {
	switch cfg.Type {
	case "firestore":
		engine, err = firestore.NewStore(ctx, cfg.FirestoreStorage.ProjectId, clock.RealClock{})
		if err != nil {
			return nil, fmt.Errorf("create firestore storage: %w", err)
		}
	case "postgres":
		// Import added at the top of the file
		connStr := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			cfg.PostgresStorage.Host,
			cfg.PostgresStorage.Port,
			cfg.PostgresStorage.User,
			cfg.PostgresStorage.Password,
			cfg.PostgresStorage.Database,
			cfg.PostgresStorage.SSLMode,
		)

		// Run migrations if configured
		if cfg.PostgresStorage.RunMigrations {
			// Build postgres:// URL for migrations (golang-migrate format)
			migrateConnStr := fmt.Sprintf(
				"postgres://%s:%s@%s:%d/%s?sslmode=%s",
				cfg.PostgresStorage.User,
				cfg.PostgresStorage.Password,
				cfg.PostgresStorage.Host,
				cfg.PostgresStorage.Port,
				cfg.PostgresStorage.Database,
				cfg.PostgresStorage.SSLMode,
			)
			slog.Info("running database migrations", "migrations_path", cfg.PostgresStorage.MigrationsPath)
			if err := postgres.RunMigrations(migrateConnStr, cfg.PostgresStorage.MigrationsPath); err != nil {
				return nil, fmt.Errorf("failed to run migrations: %w", err)
			}
			slog.Info("database migrations completed successfully")
		}

		// Build read-only connection string if configured
		if cfg.PostgresReadOnlyStorage != nil && cfg.PostgresReadOnlyStorage.Host != "" {
			readHost := cfg.PostgresReadOnlyStorage.Host
			readPort := cfg.PostgresReadOnlyStorage.Port
			if readPort == 0 {
				readPort = cfg.PostgresStorage.Port // Default to primary port
			}

			readConnStr := fmt.Sprintf(
				"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
				readHost,
				readPort,
				cfg.PostgresStorage.User,
				cfg.PostgresStorage.Password,
				cfg.PostgresStorage.Database,
				cfg.PostgresStorage.SSLMode,
			)

			slog.Info("read replica configured",
				"read_host", readHost,
				"read_port", readPort)

			engine, err = postgres.NewStore(ctx, connStr, readConnStr)
		} else {
			engine, err = postgres.NewStore(ctx, connStr)
		}

		if err != nil {
			return nil, fmt.Errorf("create postgres storage: %w", err)
		}
	case "in_memory":
		engine = inmemory.NewStore(clock.RealClock{})
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.Type)
	}

	return
}

func getContractCertValidator(cfg *ContractCertValidatorConfig, httpClient *http.Client) (contractCertValidator services.CertificateValidationService, err error) {
	switch cfg.Type {
	case "ocsp":
		var rootCertificateProvider services.RootCertificateProviderService
		rootCertificateProvider, err = getRootCertProvider(&cfg.Ocsp.RootCertProvider, httpClient)
		if err != nil {
			return nil, fmt.Errorf("create root certificate provider: %w", err)
		}

		contractCertValidator, err = &services.OnlineCertificateValidationService{
			RootCertificateProvider: rootCertificateProvider,
			MaxOCSPAttempts:         cfg.Ocsp.MaxAttempts,
			HttpClient:              httpClient,
		}, nil
	default:
		return nil, fmt.Errorf("unknown contract certificate validator type: %s", cfg.Type)
	}

	return
}

func getRootCertProvider(cfg *RootCertProviderConfig, httpClient *http.Client) (rootCertificateProvider services.RootCertificateProviderService, err error) {
	switch cfg.Type {
	case "file":
		rootCertificateProvider = services.FileRootCertificateProviderService{
			FilePaths: cfg.File.FileNames,
		}
	case "opcp":
		ttl := 24 * time.Hour

		if cfg.Opcp.Ttl != "" {
			ttl, err = time.ParseDuration(cfg.Opcp.Ttl)
			if err != nil {
				return nil, fmt.Errorf("failed to parse root certificate provider TTL: %w", err)
			}
		}

		httpTokenService, err := getHttpTokenService(&cfg.Opcp.HttpAuth, httpClient)
		if err != nil {
			return nil, fmt.Errorf("create http auth service: %w", err)
		}

		rootCertificateProvider = services.NewCachingRootCertificateProviderService(
			services.OpcpRootCertificateProviderService{
				BaseURL:      cfg.Opcp.Url,
				TokenService: httpTokenService,
				HttpClient:   httpClient,
			}, ttl, clock.RealClock{})
	case "composite":
		providers := make([]services.RootCertificateProviderService, len(cfg.Composite.Providers))
		for index, providerCfg := range cfg.Composite.Providers {
			providerCfg := providerCfg
			providers[index], err = getRootCertProvider(&providerCfg, httpClient)
			if err != nil {
				return nil, fmt.Errorf("creating composite root certificate provider %d: %v", index, err)
			}
		}

		rootCertificateProvider = services.CompositeRootCertificateProviderService{
			Providers: providers,
		}

	default:
		return nil, fmt.Errorf("unknown root certificate provider type: %s", cfg.Type)
	}

	return
}

func getContractCertProvider(cfg *ContractCertProviderConfig, httpClient *http.Client) (evCertificateProvider services.ContractCertificateProvider, err error) {
	switch cfg.Type {
	case "opcp":
		httpTokenService, err := getHttpTokenService(&cfg.Opcp.HttpAuth, httpClient)
		if err != nil {
			return nil, fmt.Errorf("create http auth service: %w", err)
		}

		evCertificateProvider = &services.OpcpContractCertificateProvider{
			BaseURL:          cfg.Opcp.Url,
			HttpTokenService: httpTokenService,
			HttpClient:       httpClient,
		}
	case "default":
		evCertificateProvider = &services.DefaultContractCertificateProvider{}
	default:
		return nil, fmt.Errorf("unknown contract certificate provider type: %s", cfg.Type)
	}

	return
}

func getChargeStationCertProvider(ctx context.Context, cfg *ChargeStationCertProviderConfig, engine store.Engine, httpClient *http.Client) (chargeStationCertProvider services.ChargeStationCertificateProvider, err error) {
	switch cfg.Type {
	case "opcp":
		httpTokenService, err := getHttpTokenService(&cfg.Opcp.HttpAuth, httpClient)
		if err != nil {
			return nil, fmt.Errorf("create http auth service: %w", err)
		}

		chargeStationCertProvider = &services.OpcpChargeStationCertificateProvider{
			BaseURL:          cfg.Opcp.Url,
			ISOVersion:       services.ISO15118V2,
			HttpTokenService: httpTokenService,
			HttpClient:       httpClient,
		}
	case "local":
		certificateSource, err := getLocalSource(cfg.Local.CertificateSource)
		if err != nil {
			return nil, fmt.Errorf("create local source: %w", err)
		}
		privateKeySource, err := getLocalSource(cfg.Local.PrivateKeySource)
		if err != nil {
			return nil, fmt.Errorf("create private key source: %w", err)
		}

		chargeStationCertProvider = &services.LocalChargeStationCertificateProvider{
			Store:             engine,
			CertificateReader: certificateSource,
			PrivateKeyReader:  privateKeySource,
		}
	case "delegating":
		var v2gChargeStationCertProvider services.ChargeStationCertificateProvider
		v2gChargeStationCertProvider, err = getChargeStationCertProvider(ctx, cfg.Delegating.V2G, engine, httpClient)
		if err != nil {
			return
		}
		var csoChargeStationCertProvider services.ChargeStationCertificateProvider
		csoChargeStationCertProvider, err = getChargeStationCertProvider(ctx, cfg.Delegating.CSO, engine, httpClient)
		if err != nil {
			return
		}

		chargeStationCertProvider = &services.DelegatingChargeStationCertificateProvider{
			V2GChargeStationCertificateProvider: v2gChargeStationCertProvider,
			CSOChargeStationCertificateProvider: csoChargeStationCertProvider,
		}
	case "default":
		chargeStationCertProvider = &services.DefaultChargeStationCertificateProvider{}
	default:
		return nil, fmt.Errorf("unknown charge station certificate provider type: %s", cfg.Type)
	}

	return
}

func getHttpTokenService(cfg *HttpAuthConfig, httpClient *http.Client) (httpTokenService services.HttpTokenService, err error) {
	switch cfg.Type {
	case "env_token":
		httpTokenService, err = services.NewEnvHttpTokenService(cfg.EnvToken.EnvVar)
	case "fixed_token":
		httpTokenService = services.NewFixedHttpTokenService(cfg.FixedToken.Token)
	case "oauth2_token":
		var clientSecret string
		if cfg.OAuth2Token.ClientSecret != nil {
			clientSecret = *cfg.OAuth2Token.ClientSecret
		} else if cfg.OAuth2Token.ClientSecretEnvVar != nil {
			clientSecret = os.Getenv(*cfg.OAuth2Token.ClientSecretEnvVar)
		} else {
			return nil, fmt.Errorf("client_secret or client_secret_env_var must be provided")
		}
		httpTokenService = services.NewOAuth2HttpTokenService(cfg.OAuth2Token.Url, cfg.OAuth2Token.ClientId, clientSecret, httpClient, clock.RealClock{})
	case "hubject_test_token":
		ttl := 24 * time.Hour
		if cfg.HubjectTestToken.Ttl != "" {
			ttl, err = time.ParseDuration(cfg.HubjectTestToken.Ttl)
			if err != nil {
				return nil, fmt.Errorf("parse hubject test token ttl: %w", err)
			}
		}
		httpTokenService = services.NewCachingHttpTokenService(
			services.NewHubjectTestHttpTokenService(cfg.HubjectTestToken.Url, httpClient), ttl, clock.RealClock{})
	default:
		return nil, fmt.Errorf("unknown http auth service type: %s", cfg.Type)
	}

	return
}

func getLocalSource(cfg *LocalSourceConfig) (source services.LocalSource, err error) {
	switch cfg.Type {
	case "file":
		source = services.FileSource{
			FileName: cfg.File,
		}
	case "google_cloud_secret":
		source = services.GoogleSecretSource{
			SecretName: cfg.GoogleCloudSecret,
		}
	default:
		return nil, fmt.Errorf("unknown local source type: %s", cfg.Type)
	}

	return
}

func getTariffService(cfg *TariffServiceConfig) (tariffService services.TariffService, err error) {
	switch cfg.Type {
	case "kwh":
		tariffService = services.BasicKwhTariffService{}
	default:
		return nil, fmt.Errorf("unknown tariff service type: %s", cfg.Type)
	}

	return
}

// getNatsConn opens the connection to NATS that [nats] describes.
func getNatsConn(cfg *NatsConfig) (*nats.Conn, error) {
	connCfg := natstransport.ConnConfig{
		Urls:           cfg.Urls,
		Name:           cfg.Name,
		CredsFile:      cfg.CredsFile,
		NkeySeedFile:   cfg.NkeySeedFile,
		User:           cfg.User,
		Password:       cfg.Password,
		Token:          cfg.Token,
		TlsCaFile:      cfg.TlsCaFile,
		TlsCertFile:    cfg.TlsCertFile,
		TlsKeyFile:     cfg.TlsKeyFile,
		ConnectTimeout: natstransport.DefaultConnectTimeout,
		ReconnectWait:  natstransport.DefaultReconnectWait,
		MaxReconnects:  natstransport.DefaultMaxReconnects,
	}
	if cfg.ConnectTimeout != "" {
		connectTimeout, err := time.ParseDuration(cfg.ConnectTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse nats connect timeout: %w", err)
		}
		if connectTimeout <= 0 {
			return nil, fmt.Errorf("nats connect timeout must be positive: %s", cfg.ConnectTimeout)
		}
		connCfg.ConnectTimeout = connectTimeout
	}
	if cfg.ReconnectWait != "" {
		reconnectWait, err := time.ParseDuration(cfg.ReconnectWait)
		if err != nil {
			return nil, fmt.Errorf("failed to parse nats reconnect wait: %w", err)
		}
		if reconnectWait <= 0 {
			return nil, fmt.Errorf("nats reconnect wait must be positive: %s", cfg.ReconnectWait)
		}
		connCfg.ReconnectWait = reconnectWait
	}
	if cfg.MaxReconnects != nil {
		connCfg.MaxReconnects = *cfg.MaxReconnects
	}

	conn, err := natstransport.Connect(connCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nats: %w", err)
	}
	return conn, nil
}

// natsTransportSettings returns [transport.nats], which may be left out.
func natsTransportSettings(cfg *TransportConfig) (*NatsTransportConfig, error) {
	settings := cfg.Nats
	if settings == nil {
		settings = &NatsTransportConfig{}
	}
	if settings.SubjectPrefix != "" {
		if err := natstransport.ValidatePrefix(settings.SubjectPrefix); err != nil {
			return nil, fmt.Errorf("transport nats subject_prefix: %w", err)
		}
	}
	return settings, nil
}

func getMsgEmitter(cfg *TransportConfig, tracer oteltrace.Tracer, natsConn *nats.Conn) (transport.Emitter, error) {
	switch cfg.Type {
	case "nats":
		settings, err := natsTransportSettings(cfg)
		if err != nil {
			return nil, err
		}
		return natstransport.NewEmitter(natsConn,
			natstransport.WithSubjectPrefix[natstransport.Emitter](settings.SubjectPrefix),
			natstransport.WithOtelTracer[natstransport.Emitter](tracer)), nil
	case "mqtt":
		var mqttUrls []*url.URL
		for _, urlStr := range cfg.Mqtt.Urls {
			u, err := url.Parse(urlStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse mqtt url: %w", err)
			}
			mqttUrls = append(mqttUrls, u)
		}

		mqttConnectTimeout, err := time.ParseDuration(cfg.Mqtt.ConnectTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt connect timeout: %w", err)
		}

		mqttConnectRetryDelay, err := time.ParseDuration(cfg.Mqtt.ConnectRetryDelay)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt connect retry delay: %w", err)
		}

		mqttKeepAliveInterval, err := time.ParseDuration(cfg.Mqtt.KeepAliveInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt keep alive interval: %w", err)
		}

		mqttEmitter := mqtt2.NewEmitter(
			mqtt2.WithMqttBrokerUrls[mqtt2.Emitter](mqttUrls),
			mqtt2.WithMqttPrefix[mqtt2.Emitter](cfg.Mqtt.Prefix),
			mqtt2.WithMqttConnectSettings[mqtt2.Emitter](mqttConnectTimeout, mqttConnectRetryDelay, mqttKeepAliveInterval),
			mqtt2.WithOtelTracer[mqtt2.Emitter](tracer))

		return mqttEmitter, nil
	default:
		return nil, fmt.Errorf("unknown transport type: %s", cfg.Type)
	}
}

func getMsgListener(cfg *TransportConfig, tracer oteltrace.Tracer, natsConn *nats.Conn) (transport.Listener, error) {
	switch cfg.Type {
	case "nats":
		settings, err := natsTransportSettings(cfg)
		if err != nil {
			return nil, err
		}
		return natstransport.NewListener(natsConn,
			natstransport.WithSubjectPrefix[natstransport.Listener](settings.SubjectPrefix),
			natstransport.WithQueueGroup(settings.QueueGroup),
			natstransport.WithWorkers(settings.Workers),
			natstransport.WithOtelTracer[natstransport.Listener](tracer)), nil
	case "mqtt":
		var mqttUrls []*url.URL
		for _, urlStr := range cfg.Mqtt.Urls {
			u, err := url.Parse(urlStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse mqtt url: %w", err)
			}
			mqttUrls = append(mqttUrls, u)
		}

		mqttConnectTimeout, err := time.ParseDuration(cfg.Mqtt.ConnectTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt connect timeout: %w", err)
		}

		mqttConnectRetryDelay, err := time.ParseDuration(cfg.Mqtt.ConnectRetryDelay)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt connect retry delay: %w", err)
		}

		mqttKeepAliveInterval, err := time.ParseDuration(cfg.Mqtt.KeepAliveInterval)
		if err != nil {
			return nil, fmt.Errorf("failed to parse mqtt keep alive interval: %w", err)
		}

		opts := []mqtt2.Opt[mqtt2.Listener]{
			mqtt2.WithMqttBrokerUrls[mqtt2.Listener](mqttUrls),
			mqtt2.WithMqttPrefix[mqtt2.Listener](cfg.Mqtt.Prefix),
			mqtt2.WithMqttConnectSettings[mqtt2.Listener](mqttConnectTimeout, mqttConnectRetryDelay, mqttKeepAliveInterval),
			mqtt2.WithMqttGroup[mqtt2.Listener](cfg.Mqtt.Group),
			mqtt2.WithOtelTracer[mqtt2.Listener](tracer),
		}

		return mqtt2.NewListener(opts...), nil
	default:
		return nil, fmt.Errorf("unknown transport type: %s", cfg.Type)
	}
}

func getTracerProvider(ctx context.Context, collectorAddr string) (*trace.TracerProvider, error) {
	var err error
	var res *resource.Resource
	var traceExporter trace.SpanExporter

	if collectorAddr != "" {
		res, err = resource.New(ctx,
			resource.WithDetectors(gcp.NewDetector()),
			resource.WithTelemetrySDK(),
			resource.WithAttributes(
				// the service name used to display traces in backends
				semconv.ServiceName("maeve-csms-manager"),
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create resource: %w", err)
		}

		// Note the use of insecure transport here. TLS is recommended in production.
		conn, err := grpc.NewClient(collectorAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
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

	return tracerProvider, nil
}
