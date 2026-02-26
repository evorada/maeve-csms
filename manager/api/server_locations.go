// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpi"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) RegisterLocation(w http.ResponseWriter, r *http.Request, locationId string) {
	if s.ocpi == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	req := new(Location)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	now := s.clock.Now()

	var numEvses int
	if req.Evses != nil {
		numEvses = len(*req.Evses)
	}
	storeEvses := make([]store.Evse, numEvses)
	if numEvses != 0 {
		for i, reqEvse := range *req.Evses {
			storeConnectors := make([]store.Connector, len(reqEvse.Connectors))
			for j, reqConnector := range reqEvse.Connectors {
				storeConnectors[j] = store.Connector{
					Id:          reqConnector.Id,
					Format:      string(reqConnector.Format),
					PowerType:   string(reqConnector.PowerType),
					Standard:    string(reqConnector.Standard),
					MaxVoltage:  reqConnector.MaxVoltage,
					MaxAmperage: reqConnector.MaxAmperage,
					LastUpdated: now.Format(time.RFC3339),
				}
				storeEvses[i] = store.Evse{
					Connectors:  storeConnectors,
					EvseId:      reqEvse.EvseId,
					Status:      string(ocpi.EvseStatusUNKNOWN),
					Uid:         reqEvse.Uid,
					LastUpdated: now.Format(time.RFC3339),
				}
			}
		}
	}
	err := s.store.SetLocation(r.Context(), &store.Location{
		Address: req.Address,
		City:    req.City,
		Coordinates: store.GeoLocation{
			Latitude:  req.Coordinates.Latitude,
			Longitude: req.Coordinates.Longitude,
		},
		Country:     req.Country,
		Evses:       &storeEvses,
		Id:          locationId,
		Name:        *req.Name,
		ParkingType: string(*req.ParkingType),
		PostalCode:  *req.PostalCode,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	ocpiEvses := make([]ocpi.Evse, numEvses)
	if numEvses != 0 {
		for i, reqEvse := range *req.Evses {
			ocpiConnectors := make([]ocpi.Connector, len(reqEvse.Connectors))
			for j, reqConnector := range reqEvse.Connectors {
				ocpiConnectors[j] = ocpi.Connector{
					Id:          reqConnector.Id,
					Format:      ocpi.ConnectorFormat(reqConnector.Format),
					PowerType:   ocpi.ConnectorPowerType(reqConnector.PowerType),
					Standard:    ocpi.ConnectorStandard(reqConnector.Standard),
					MaxVoltage:  reqConnector.MaxVoltage,
					MaxAmperage: reqConnector.MaxAmperage,
					LastUpdated: now.Format(time.RFC3339),
				}
				ocpiEvses[i] = ocpi.Evse{
					Connectors:  ocpiConnectors,
					EvseId:      reqEvse.EvseId,
					Status:      ocpi.EvseStatusUNKNOWN,
					Uid:         reqEvse.Uid,
					LastUpdated: now.Format(time.RFC3339),
				}
			}
		}
	}
	err = s.ocpi.PushLocation(r.Context(), ocpi.Location{
		Address: req.Address,
		City:    req.City,
		Coordinates: ocpi.GeoLocation{
			Latitude:  req.Coordinates.Latitude,
			Longitude: req.Coordinates.Longitude,
		},
		Country:     req.Country,
		CountryCode: req.CountryCode,
		Evses:       &ocpiEvses,
		Id:          locationId,
		LastUpdated: now.Format(time.RFC3339),
		Name:        req.Name,
		ParkingType: (*ocpi.LocationParkingType)(req.ParkingType),
		PartyId:     req.PartyId,
		PostalCode:  req.PostalCode,
		Publish:     true,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Render implementations

func (r Location) Bind(req *http.Request) error {
	return nil
}
