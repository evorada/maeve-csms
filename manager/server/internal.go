// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"text/template"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thoughtworks/maeve-csms/manager/adminui"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/templates"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// InternalDeps is what the handler of the internal listener needs.
type InternalDeps struct {
	Resolver *tenancy.Resolver
	Engine   store.Engine
	// Verifier authenticates the callers of the control plane. It is required.
	Verifier tenancy.Verifier
	// Settings says which legacy pages are served, and has the addresses and the
	// organization name that the admin pages use.
	Settings config.ApiSettings
	// CSCertProvider issues the client certificates of the admin pages.
	CSCertProvider services.ChargeStationCertificateProvider
	// DefaultOrgId is the org of what is created through the admin pages.
	DefaultOrgId string
}

// NewInternalHandler creates the handler of the internal listener. It serves the
// control plane of the console at /_evorada/v1, the lookups of the gateway at
// /_internal/v1, the health check at /health, the metrics at /metrics and the OpenAPI
// document of the API at /api/openapi.json. The legacy pages /transactions and /adminui
// are only served when d.Settings enables them.
//
// Only the control plane is authenticated: every request to it must be accepted by
// d.Verifier and must come from a service of the console that may manage tenants.
// Everything else on the listener is open, so the listener must only be reachable from
// inside the deployment.
func NewInternalHandler(d InternalDeps) http.Handler {
	if d.Verifier == nil {
		panic("server: the internal handler needs a verifier")
	}

	r := chi.NewRouter()

	logger := middleware.RequestLogger(logFormatter{})

	r.Use(middleware.Recoverer)
	r.With(logger).Mount("/_internal/v1", internalapi.NewHandler(d.Resolver, d.Engine))

	controlPlane := &controlplane.Server{
		Tenants:  d.Engine,
		Resolver: d.Resolver,
	}
	if d.Resolver != nil {
		controlPlane.Mode = d.Resolver.Mode
	}
	// not every storage engine can say whether it works
	if checker, ok := d.Engine.(controlplane.HealthChecker); ok {
		controlPlane.Store = checker
	}
	// a request is authenticated before anything else looks at it; the handler itself
	// refuses every caller that is not a console service with the permission
	r.With(logger, tenancy.Middleware(d.Verifier), logPrincipal).Mount(controlplane.BasePath, controlplane.NewHandler(controlPlane))

	// what a browser may open keeps its security headers; there are no CORS headers, so
	// that the page of another site cannot read anything from this listener
	r.Group(func(r chi.Router) {
		r.Use(securityHeaders())
		r.Get("/health", health)
		r.Handle("/metrics", promhttp.Handler())
		r.Get("/api/openapi.json", getApiSwaggerJson)
		if d.Settings.EnableTransactionsPage {
			slog.Warn("legacy route enabled on internal listener", "route", "/transactions")
			r.Get("/transactions", transactions(d.Engine))
		}
		if d.Settings.EnableAdminUI {
			slog.Warn("legacy route enabled on internal listener", "route", "/adminui")
			r.With(logger).Mount("/adminui", adminui.NewServer(d.Settings.Host, d.Settings.WsPort, d.Settings.WssPort,
				certOrgNameFor(&tenancy.ConfigProvider{Resolver: d.Resolver}, d.Settings.OrgName), d.Engine, d.CSCertProvider, d.DefaultOrgId))
		}
	})
	return r
}

// certOrgNameFor returns what gives the organisation name of the client certificate that
// the admin pages issue for a charge station: the first organisation name that the
// tenant of the charge station accepts in a certificate, and fallback for a charge
// station without such a tenant (always so in single mode).
func certOrgNameFor(tenantConfig *tenancy.ConfigProvider, fallback string) func(ctx context.Context, csId string) string {
	return func(ctx context.Context, csId string) string {
		cfg, err := tenantConfig.ConfigForStation(ctx, csId)
		if err != nil {
			slog.Warn("looking up the tenant config of a charge station, using the configured organisation name",
				"chargeStationId", csId, "err", err)
			return fallback
		}
		if len(cfg.AcceptedCertOrgNames) > 0 {
			return cfg.AcceptedCertOrgNames[0]
		}
		return fallback
	}
}

func getApiSwaggerJson(w http.ResponseWriter, r *http.Request) {
	swagger, err := api.GetSwagger()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	json, err := swagger.MarshalJSON()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(json)
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"OK"}`))
}

func transactions(transactionStore store.Engine) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ts, err := transactionStore.Transactions(r.Context())
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		t := template.Must(template.New("").Parse(templates.Transactions))
		err = t.Execute(w, ts)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
