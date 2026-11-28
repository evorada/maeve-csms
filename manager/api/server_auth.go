// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/stations"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// newStationConnectors is how many connectors a charge station counts for when it is
// registered.
const newStationConnectors = 1

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

	// The lookup of the charge station, its registration and its event are one store
	// transaction: station.registered is stored with the registration of a charge station
	// that did not exist, and never for a registration that was refused or not stored.
	err := s.store.RunInTx(r.Context(), func(ctx context.Context) error {
		existing, err := s.store.LookupChargeStationAuth(ctx, csId)
		if err != nil {
			return fmt.Errorf("look up the charge station: %w", err)
		}
		// a decommissioned charge station is kept for its history: its id is not taken
		// into service again
		if existing != nil {
			decommissioned, err := stations.Decommissioned(ctx, s.store, csId)
			if err != nil {
				return err
			}
			if decommissioned {
				return answerWith(ErrConflict(errorStationDecommissioned + ": the charge station is decommissioned; its id cannot be registered again"))
			}
		}

		// A charge station that is new adds to the connectors of its tenant: it is only
		// registered while a hard limit on them has room for one more (how many connectors
		// it really has is not known before it reports them). Registering a charge station
		// again adds nothing and is never refused.
		if existing == nil {
			if err := s.limits.CheckConnectors(ctx, orgId, newStationConnectors); err != nil {
				// nothing is registered when the limit is used up or cannot be checked
				return fmt.Errorf("check the connector limit of the tenant: %w", err)
			}
		}

		var securityProfile store.SecurityProfile
		if req.SecurityProfile != nil {
			securityProfile = store.SecurityProfile(*req.SecurityProfile)
		} else {
			// A charge station that is registered without a security profile gets the
			// default of its tenant. When the config of the tenant cannot be looked up
			// nothing is registered: a guess could be a weaker profile than the tenant
			// asked for.
			cfg, err := s.tenantConfig.ConfigForOrg(ctx, orgId)
			if err != nil {
				return fmt.Errorf("look up the default security profile: %w", err)
			}
			securityProfile = cfg.SecurityProfile()
		}

		err = s.store.SetChargeStationAuth(ctx, csId, &store.ChargeStationAuth{
			SecurityProfile:        securityProfile,
			Base64SHA256Password:   pwd,
			InvalidUsernameAllowed: invalidUsernameAllowed,
			OrgId:                  orgId,
		})
		if err != nil {
			return fmt.Errorf("store the charge station: %w", err)
		}
		if existing != nil {
			return nil
		}
		// the actor is the caller of the request (ActorMiddleware)
		return s.events.Emit(ctx, events.Event{
			Type:         events.TypeStationRegistered,
			OrgId:        orgId,
			ResourceType: events.ResourceStation,
			ResourceId:   csId,
			Data: events.StationRegisteredData{
				StationKey:      csId,
				StationId:       events.StationId(csId),
				SecurityProfile: int(securityProfile),
			},
		})
	})
	if err != nil {
		var exceeded *entitlements.LimitExceededError
		if errors.As(err, &exceeded) {
			_ = render.Render(w, r, ErrLimitExceeded(exceeded))
			return
		}
		var answer *assetAnswer
		if errors.As(err, &answer) {
			_ = render.Render(w, r, answer.answer)
			return
		}
		slog.Error("registering a charge station", "chargeStationId", csId, "org", orgId, "err", err)
		_ = render.Render(w, r, errInternal)
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
	// a decommissioned charge station may no longer connect
	decommissioned, err := stations.Decommissioned(r.Context(), s.store, csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if decommissioned {
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
