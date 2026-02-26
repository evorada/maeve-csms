// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
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
	if req.SecurityProfile < 0 || req.SecurityProfile > 127 {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("security profile value %d is out of valid range", req.SecurityProfile)))
		return
	}

	err := s.store.SetChargeStationAuth(r.Context(), csId, &store.ChargeStationAuth{
		SecurityProfile:        store.SecurityProfile(req.SecurityProfile),
		Base64SHA256Password:   pwd,
		InvalidUsernameAllowed: invalidUsernameAllowed,
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

	resp := &ChargeStationAuth{
		SecurityProfile: int(auth.SecurityProfile),
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
