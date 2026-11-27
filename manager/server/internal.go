// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/thoughtworks/maeve-csms/manager/internalapi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// InternalDeps is what the handler of the internal listener needs.
type InternalDeps struct {
	Resolver *tenancy.Resolver
	Engine   store.Engine
}

// NewInternalHandler creates the handler of the internal listener, which serves the
// lookups of the gateway at /_internal/v1. Nothing on it is authenticated: the
// listener must only be reachable by the gateway.
func NewInternalHandler(d InternalDeps) http.Handler {
	r := chi.NewRouter()

	logger := middleware.RequestLogger(logFormatter{})

	r.Use(middleware.Recoverer)
	r.With(logger).Mount("/_internal/v1", internalapi.NewHandler(d.Resolver, d.Engine))
	return r
}
