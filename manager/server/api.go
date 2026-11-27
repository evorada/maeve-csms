// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"os"
	"text/template"

	"github.com/rs/cors"
	"github.com/thoughtworks/maeve-csms/manager/adminui"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/config"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/unrolled/secure"
	"k8s.io/utils/clock"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thoughtworks/maeve-csms/manager/templates"
)

// ApiDeps is what the handler of the API listener needs.
type ApiDeps struct {
	Settings       config.ApiSettings
	Engine         store.Engine
	Ocpi           ocpi.Api
	CSCertProvider services.ChargeStationCertificateProvider
	// Verifier authenticates the callers of /api/v0. It is required.
	Verifier tenancy.Verifier
	// DefaultOrgId is the org of what is created through the API and the admin pages
	// (see api.NewServer).
	DefaultOrgId string
}

// NewApiHandler creates the handler of the manager API. Every request to /api/v0 must
// be accepted by d.Verifier; the other routes are served without authentication.
func NewApiHandler(d ApiDeps) http.Handler {
	if d.Verifier == nil {
		panic("server: the api handler needs a verifier")
	}

	apiServer, err := api.NewServer(d.Engine, clock.RealClock{}, d.Ocpi, d.DefaultOrgId)
	if err != nil {
		panic(err)
	}

	var isDevelopment bool
	if os.Getenv("ENVIRONMENT") == "dev" {
		isDevelopment = true
	}
	secureMiddleware := secure.New(secure.Options{
		IsDevelopment:         isDevelopment,
		BrowserXssFilter:      true,
		ContentTypeNosniff:    true,
		FrameDeny:             true,
		ContentSecurityPolicy: "frame-ancestors: 'none'",
	})

	r := chi.NewRouter()

	logger := middleware.RequestLogger(logFormatter{})

	r.Use(middleware.Recoverer, secureMiddleware.Handler, cors.Default().Handler)
	r.Get("/health", health)
	r.Get("/transactions", transactions(d.Engine))
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/api/openapi.json", getApiSwaggerJson)
	// a request is authenticated before anything else looks at it
	r.With(logger, tenancy.Middleware(d.Verifier), logPrincipal, api.ValidationMiddleware).Mount("/api/v0", api.Handler(apiServer))
	r.With(logger).Mount("/adminui", adminui.NewServer(d.Settings.Host, d.Settings.WsPort, d.Settings.WssPort, d.Settings.OrgName, d.Engine, d.CSCertProvider, d.DefaultOrgId))
	return r
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
