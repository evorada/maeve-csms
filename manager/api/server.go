// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/getkin/kin-openapi/openapi3"
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

// NewServer creates the API server. Its operations expect a verified caller in the
// context of a request (tenancy.FromContext) and the AuthzMiddleware in front of them.
//
// What is created through the API belongs to an org that the caller may reach.
// Without WithScoper the server is single-tenant: the only org is the legacy org
// (store.LegacyOrgId).
func NewServer(engine store.Engine, clock clock.PassiveClock, ocpi ocpi.Api, opts ...ServerOption) (*Server, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, err
	}
	s := &Server{
		store:   engine,
		clock:   clock,
		ocpi:    ocpi,
		swagger: swagger,
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
