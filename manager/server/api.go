// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"os"

	"github.com/rs/cors"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/unrolled/secure"
	"k8s.io/utils/clock"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ApiDeps is what the handler of the API listener needs.
type ApiDeps struct {
	Engine store.Engine
	Ocpi   ocpi.Api
	// Verifier authenticates the callers of /api/v0. It is required.
	Verifier tenancy.Verifier
	// Scoper says which orgs a caller of /api/v0 may reach. It is required.
	Scoper *tenancy.Scoper
	// Entitlements enforces the hard limits of the entitlements of a tenant on what is
	// created through /api/v0. Nothing is limited without it.
	Entitlements *entitlements.Enforcer
	// Events stores the domain events of the changes made through /api/v0. It is
	// required.
	Events events.Emitter
	// Commands queues the commands that callers send to the charge stations. Without it
	// (a storage engine that does not keep commands) the command operations answer 501.
	Commands *commands.Service
}

// apiBasePath is where the API is mounted.
const apiBasePath = api.BasePath

// NewApiHandler creates the handler of the API listener. It serves /api/v0 and nothing
// else: every request to it must be accepted by d.Verifier, and its caller needs the
// permission of the operation and only reaches the orgs that d.Scoper gives it. The
// health check, the metrics, the OpenAPI document and the legacy pages are on the
// internal listener (NewInternalHandler).
func NewApiHandler(d ApiDeps) http.Handler {
	if d.Verifier == nil {
		panic("server: the api handler needs a verifier")
	}
	if d.Scoper == nil {
		panic("server: the api handler needs a scoper")
	}
	if d.Events == nil {
		panic("server: the api handler needs an emitter of domain events")
	}

	apiServer, err := api.NewServer(d.Engine, clock.RealClock{}, d.Ocpi, d.Events, api.WithScoper(d.Scoper),
		api.WithEntitlements(d.Entitlements), api.WithCommands(d.Commands))
	if err != nil {
		panic(err)
	}
	swagger, err := api.GetSwagger()
	if err != nil {
		panic(err)
	}
	// an operation without a permission in the spec stops the manager from starting
	permissions, err := api.BuildPermissionIndex(swagger)
	if err != nil {
		panic(err)
	}
	apiHandler := api.HandlerWithOptions(apiServer, api.ChiServerOptions{
		Middlewares: []api.MiddlewareFunc{api.AuthzMiddleware(d.Scoper, permissions, apiBasePath)},
	})

	r := chi.NewRouter()

	logger := middleware.RequestLogger(logFormatter{})

	r.Use(middleware.Recoverer, securityHeaders(), cors.Default().Handler)
	// a request is authenticated before anything else looks at it; its caller is the
	// actor of the domain events that it causes
	r.With(logger, tenancy.Middleware(d.Verifier), logPrincipal, api.ActorMiddleware, api.ValidationMiddleware).Mount(apiBasePath, apiHandler)
	return r
}

// securityHeaders returns the middleware that sets the security headers of a response.
func securityHeaders() func(http.Handler) http.Handler {
	return secure.New(secure.Options{
		IsDevelopment:         os.Getenv("ENVIRONMENT") == "dev",
		BrowserXssFilter:      true,
		ContentTypeNosniff:    true,
		FrameDeny:             true,
		ContentSecurityPolicy: "frame-ancestors: 'none'",
	}).Handler
}
