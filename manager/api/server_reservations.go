// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) CreateReservation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ReservationRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Build reservation
	reservation := &store.Reservation{
		ReservationId:   int(req.ReservationId),
		ChargeStationId: csId,
		ConnectorId:     int(req.ConnectorId),
		IdTag:           req.IdTag,
		ExpiryDate:      req.ExpiryDate,
		Status:          store.ReservationStatusAccepted, // Initial status
		CreatedAt:       s.clock.Now(),
	}

	if req.ParentIdTag != nil {
		reservation.ParentIdTag = req.ParentIdTag
	}

	// Store the reservation
	err := s.store.CreateReservation(r.Context(), reservation)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - actual OCPP command will be sent asynchronously
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) CancelReservation(w http.ResponseWriter, r *http.Request, csId string, reservationId int32) {
	// Check if the reservation exists
	reservation, err := s.store.GetReservation(r.Context(), int(reservationId))
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if reservation == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Verify the reservation belongs to the specified charge station
	if reservation.ChargeStationId != csId {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Cancel the reservation
	err = s.store.CancelReservation(r.Context(), int(reservationId))
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - actual OCPP command will be sent asynchronously
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) ListReservations(w http.ResponseWriter, r *http.Request, csId string, params ListReservationsParams) {
	// Default status filter is "active"
	statusFilter := "active"
	if params.Status != nil {
		statusFilter = string(*params.Status)
	}

	var reservations []*store.Reservation
	var err error

	switch statusFilter {
	case "active":
		// Get active reservations (accepted status, not expired)
		reservations, err = s.store.GetActiveReservations(r.Context(), csId)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	case "all":
		// For "all", we still use GetActiveReservations as the primary method
		// In a real implementation, you might want a separate store method
		reservations, err = s.store.GetActiveReservations(r.Context(), csId)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
		// Note: This is a simplified implementation
		// A full implementation might need a GetAllReservations method
	case "expired":
		// For expired, we'd need a separate query
		// For now, return empty list as a placeholder
		reservations = []*store.Reservation{}
	default:
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid status filter: %s", statusFilter)))
		return
	}

	// Sort by reservation ID for deterministic ordering
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].ReservationId < reservations[j].ReservationId
	})

	// Convert to API response format
	response := &ReservationList{
		Reservations: make([]ReservationResponse, 0, len(reservations)),
	}

	for _, res := range reservations {
		createdAt := res.CreatedAt
		apiRes := ReservationResponse{
			ReservationId: int32(res.ReservationId),
			ConnectorId:   int32(res.ConnectorId),
			ExpiryDate:    res.ExpiryDate,
			IdTag:         res.IdTag,
			Status:        ReservationResponseStatus(res.Status),
			CreatedAt:     &createdAt,
		}
		if res.ParentIdTag != nil {
			apiRes.ParentIdTag = res.ParentIdTag
		}
		response.Reservations = append(response.Reservations, apiRes)
	}

	_ = render.Render(w, r, response)
}

// Render implementations

func (r ReservationRequest) Bind(req *http.Request) error {
	return nil
}

// Render implementations

func (r ReservationList) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}
