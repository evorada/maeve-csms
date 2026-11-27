// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

func (s *Server) RegisterChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationAuth)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	var pwd string
	if req.Base64SHA256Password != nil {
		pwd = *req.Base64SHA256Password
	}
	invalidUsernameAllowed := false
	if req.InvalidUsernameAllowed != nil {
		invalidUsernameAllowed = *req.InvalidUsernameAllowed
	}

	// Validate SecurityProfile is within valid range for int8
	if req.SecurityProfile != nil && (*req.SecurityProfile < 0 || *req.SecurityProfile > 127) {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("security profile value %d is out of valid range", *req.SecurityProfile)))
		return
	}

	// the org that owns the charge station was resolved, and checked against the scope
	// of the caller, before the request got here: the org of the charge station when it
	// exists (registering again never moves it), otherwise the org of its station key
	orgId, ok := tenancy.StationOrgFromContext(r.Context())
	if !ok {
		slog.Error("registering a charge station without a resolved org", "chargeStationId", csId)
		_ = render.Render(w, r, errInternal)
		return
	}

	var securityProfile store.SecurityProfile
	if req.SecurityProfile != nil {
		securityProfile = store.SecurityProfile(*req.SecurityProfile)
	} else {
		// A charge station that is registered without a security profile gets the default
		// of its tenant. When the config of the tenant cannot be looked up nothing is
		// registered: a guess could be a weaker profile than the tenant asked for.
		cfg, err := s.tenantConfig.ConfigForOrg(r.Context(), orgId)
		if err != nil {
			slog.Error("looking up the default security profile of a charge station", "chargeStationId", csId, "org", orgId, "err", err)
			_ = render.Render(w, r, errInternal)
			return
		}
		securityProfile = cfg.SecurityProfile()
	}

	err := s.store.SetChargeStationAuth(r.Context(), csId, &store.ChargeStationAuth{
		SecurityProfile:        securityProfile,
		Base64SHA256Password:   pwd,
		InvalidUsernameAllowed: invalidUsernameAllowed,
		OrgId:                  orgId,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) LookupChargeStationAuth(w http.ResponseWriter, r *http.Request, csId string) {
	auth, err := s.store.LookupChargeStationAuth(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if auth == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	securityProfile := int(auth.SecurityProfile)
	resp := &ChargeStationAuth{
		SecurityProfile: &securityProfile,
	}
	if auth.Base64SHA256Password != "" {
		resp.Base64SHA256Password = &auth.Base64SHA256Password
	}
	resp.InvalidUsernameAllowed = &auth.InvalidUsernameAllowed

	_ = render.Render(w, r, resp)
}

// Render implementations for auth-related types

func (c ChargeStationAuth) Bind(r *http.Request) error {
	return nil
}

func (c ChargeStationAuth) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
