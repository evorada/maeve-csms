// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// caller returns the verified caller of the request. Without one the request is
// answered (401) and false is reported.
func (s *Server) caller(w http.ResponseWriter, r *http.Request) (*tenancy.Principal, bool) {
	p, ok := tenancy.FromContext(r.Context())
	if !ok {
		_ = render.Render(w, r, ErrUnauthorized)
		return nil, false
	}
	return p, true
}

// targetOrg returns the org of something that the caller creates or changes: the
// requested org, or the org of the caller when none is requested. When the caller may
// not write there the request is answered and false is reported: an org outside the
// scope of the caller is answered like something that does not exist (404), an org in
// its scope whose tenant is suspended with 403 and the error tenant_suspended.
//
// It is only for operations that write: reading never depends on the status of a
// tenant.
func (s *Server) targetOrg(w http.ResponseWriter, r *http.Request, requested *openapi_types.UUID) (string, bool) {
	p, ok := s.caller(w, r)
	if !ok {
		return "", false
	}
	var requestedOrgId string
	if requested != nil {
		requestedOrgId = requested.String()
	}
	orgId, err := s.scoper.TargetOrg(r.Context(), p, requestedOrgId)
	if err != nil {
		renderScopeError(w, r, err)
		return "", false
	}
	if err := s.scoper.CheckWritable(r.Context(), orgId); err != nil {
		renderNotWritable(w, r, err)
		return "", false
	}
	return orgId, true
}

// renderScopeError answers a request with what the scoper refused or failed with.
func renderScopeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, tenancy.ErrOutOfScope):
		_ = render.Render(w, r, ErrNotFound)
	case errors.Is(err, tenancy.ErrOrgRequired):
		_ = render.Render(w, r, ErrInvalidRequest(tenancy.ErrOrgRequired))
	default:
		slog.Error("resolving the scope of the caller", "method", r.Method, "path", r.URL.Path, "err", err)
		_ = render.Render(w, r, errInternal)
	}
}

// defaultOrgId is the org of what does not belong to a tenant.
func (s *Server) defaultOrgId() string {
	if s.scoper.DefaultOrgId == "" {
		return store.LegacyOrgId
	}
	return s.scoper.DefaultOrgId
}
