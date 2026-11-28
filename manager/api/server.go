// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

type Server struct {
	store   store.Engine
	clock   clock.PassiveClock
	swagger *openapi3.T
	ocpi    ocpi.Api
	// scoper says which orgs the caller of a request may reach
	scoper *tenancy.Scoper
	// tenantConfig gives the config of the tenant that something is created for
	tenantConfig *tenancy.ConfigProvider
	// limits refuses what goes beyond a hard limit of the entitlements of a tenant; nil
	// limits nothing
	limits *entitlements.Enforcer
	// events stores the domain events of the changes that the API makes, in the store
	// transaction of each change
	events events.Emitter
}

// ServerOption changes how NewServer sets up the API server.
type ServerOption func(*Server)

// WithScoper sets what decides which orgs a caller may reach. It should be the scoper
// of the AuthzMiddleware in front of the server.
func WithScoper(scoper *tenancy.Scoper) ServerOption {
	return func(s *Server) {
		if scoper != nil {
			s.scoper = scoper
		}
	}
}

// WithEntitlements sets what enforces the hard limits of the entitlements of a tenant
// when something is created through the API. Without it nothing is limited.
func WithEntitlements(enforcer *entitlements.Enforcer) ServerOption {
	return func(s *Server) {
		s.limits = enforcer
	}
}

// NewServer creates the API server. Its operations expect a verified caller in the
// context of a request (tenancy.FromContext) and the AuthzMiddleware in front of them.
// The domain events of the changes go to domainEvents; ActorMiddleware in front of the
// server names the caller as their actor.
//
// What is created through the API belongs to an org that the caller may reach.
// Without WithScoper the server is single-tenant: the only org is the legacy org
// (store.LegacyOrgId).
func NewServer(engine store.Engine, clock clock.PassiveClock, ocpi ocpi.Api, domainEvents events.Emitter, opts ...ServerOption) (*Server, error) {
	if domainEvents == nil {
		return nil, errors.New("the api server needs an emitter of domain events")
	}
	swagger, err := GetSwagger()
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:   engine,
		clock:   clock,
		ocpi:    ocpi,
		swagger: swagger,
		events:  domainEvents,
		scoper: &tenancy.Scoper{
			Mode:         tenancy.ModeSingle,
			Resolver:     &tenancy.Resolver{Mode: tenancy.ModeSingle, Stations: engine, DefaultOrgId: store.LegacyOrgId},
			DefaultOrgId: store.LegacyOrgId,
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	// the tenants of the scoper, so that both agree on which org is a tenant
	s.tenantConfig = &tenancy.ConfigProvider{Resolver: s.scoper.Resolver}
	return s, nil
}
