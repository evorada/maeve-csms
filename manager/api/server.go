// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"k8s.io/utils/clock"
)

type Server struct {
	store   store.Engine
	clock   clock.PassiveClock
	swagger *openapi3.T
	ocpi    ocpi.Api
	// defaultOrgId is the org that owns what is created through this API
	defaultOrgId string
}

// NewServer creates the API server. The charge stations, tokens and locations that are
// created through the API belong to defaultOrgId; an empty defaultOrgId is the legacy
// org (store.LegacyOrgId).
func NewServer(engine store.Engine, clock clock.PassiveClock, ocpi ocpi.Api, defaultOrgId string) (*Server, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, err
	}
	if defaultOrgId == "" {
		defaultOrgId = store.LegacyOrgId
	}
	return &Server{
		store:        engine,
		clock:        clock,
		ocpi:         ocpi,
		swagger:      swagger,
		defaultOrgId: defaultOrgId,
	}, nil
}
