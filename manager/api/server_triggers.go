// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) TriggerChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationTrigger)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	err := s.store.SetChargeStationTriggerMessage(r.Context(), csId, &store.ChargeStationTriggerMessage{
		TriggerMessage: store.TriggerMessage(req.Trigger),
		ConnectorId:    req.ConnectorId,
		TriggerStatus:  store.TriggerStatusPending,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Render implementations

func (c ChargeStationTrigger) Bind(r *http.Request) error {
	return nil
}
