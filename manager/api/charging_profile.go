// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) SetChargingProfile(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetChargingProfileRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Convert API request to store model
	profile := &store.ChargingProfile{
		ChargeStationId:        csId,
		ConnectorId:            int(req.ConnectorId),
		ChargingProfileId:      int(req.CsChargingProfiles.ChargingProfileId),
		StackLevel:             int(req.CsChargingProfiles.StackLevel),
		ChargingProfilePurpose: store.ChargingProfilePurpose(req.CsChargingProfiles.ChargingProfilePurpose),
		ChargingProfileKind:    store.ChargingProfileKind(req.CsChargingProfiles.ChargingProfileKind),
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit: store.ChargingRateUnit(req.CsChargingProfiles.ChargingSchedule.ChargingRateUnit),
		},
	}

	// Optional fields
	if req.CsChargingProfiles.TransactionId != nil {
		txId := int(*req.CsChargingProfiles.TransactionId)
		profile.TransactionId = &txId
	}
	if req.CsChargingProfiles.RecurrencyKind != nil {
		rk := store.RecurrencyKind(*req.CsChargingProfiles.RecurrencyKind)
		profile.RecurrencyKind = &rk
	}
	if req.CsChargingProfiles.ValidFrom != nil {
		profile.ValidFrom = req.CsChargingProfiles.ValidFrom
	}
	if req.CsChargingProfiles.ValidTo != nil {
		profile.ValidTo = req.CsChargingProfiles.ValidTo
	}

	// Charging schedule optional fields
	if req.CsChargingProfiles.ChargingSchedule.Duration != nil {
		duration := int(*req.CsChargingProfiles.ChargingSchedule.Duration)
		profile.ChargingSchedule.Duration = &duration
	}
	if req.CsChargingProfiles.ChargingSchedule.StartSchedule != nil {
		profile.ChargingSchedule.StartSchedule = req.CsChargingProfiles.ChargingSchedule.StartSchedule
	}
	if req.CsChargingProfiles.ChargingSchedule.MinChargingRate != nil {
		profile.ChargingSchedule.MinChargingRate = req.CsChargingProfiles.ChargingSchedule.MinChargingRate
	}

	// Convert charging schedule periods
	for _, period := range req.CsChargingProfiles.ChargingSchedule.ChargingSchedulePeriod {
		storePeriod := store.ChargingSchedulePeriod{
			StartPeriod: int(period.StartPeriod),
			Limit:       period.Limit,
		}
		if period.NumberPhases != nil {
			numPhases := int(*period.NumberPhases)
			storePeriod.NumberPhases = &numPhases
		}
		profile.ChargingSchedule.ChargingSchedulePeriod = append(
			profile.ChargingSchedule.ChargingSchedulePeriod,
			storePeriod,
		)
	}

	// Store the charging profile (this will trigger OCPP operation)
	err := s.store.SetChargingProfile(r.Context(), profile)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetChargingProfiles(w http.ResponseWriter, r *http.Request, csId string, params GetChargingProfilesParams) {
	// Build query filters
	var connectorId *int
	var purpose *store.ChargingProfilePurpose
	var stackLevel *int

	if params.ConnectorId != nil {
		cid := int(*params.ConnectorId)
		connectorId = &cid
	}
	if params.ChargingProfilePurpose != nil {
		p := store.ChargingProfilePurpose(*params.ChargingProfilePurpose)
		purpose = &p
	}
	if params.StackLevel != nil {
		sl := int(*params.StackLevel)
		stackLevel = &sl
	}

	// Retrieve profiles from store
	profiles, err := s.store.GetChargingProfiles(r.Context(), csId, connectorId, purpose, stackLevel)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Convert to API response format
	var apiProfiles []ChargingProfileWithConnector
	for _, profile := range profiles {
		apiProfile := convertStoreProfileToAPI(profile)
		apiProfiles = append(apiProfiles, apiProfile)
	}

	response := ChargingProfileList{
		Profiles: apiProfiles,
	}

	_ = render.Render(w, r, &response)
}

func (s *Server) ClearChargingProfile(w http.ResponseWriter, r *http.Request, csId string, profileId int, params ClearChargingProfileParams) {
	// Build filters for clearing
	var connectorId *int
	var purpose *store.ChargingProfilePurpose
	var stackLevel *int

	pid := profileId
	if params.ConnectorId != nil {
		cid := int(*params.ConnectorId)
		connectorId = &cid
	}
	if params.ChargingProfilePurpose != nil {
		p := store.ChargingProfilePurpose(*params.ChargingProfilePurpose)
		purpose = &p
	}
	if params.StackLevel != nil {
		sl := int(*params.StackLevel)
		stackLevel = &sl
	}

	// Clear the charging profile(s)
	_, err := s.store.ClearChargingProfile(r.Context(), csId, &pid, connectorId, purpose, stackLevel)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) GetCompositeSchedule(w http.ResponseWriter, r *http.Request, csId string, params GetCompositeScheduleParams) {
	// Set defaults
	duration := 3600
	if params.Duration != nil {
		duration = int(*params.Duration)
	}

	var chargingRateUnit *store.ChargingRateUnit
	if params.ChargingRateUnit != nil {
		unit := store.ChargingRateUnit(*params.ChargingRateUnit)
		chargingRateUnit = &unit
	} else {
		unit := store.ChargingRateUnitA
		chargingRateUnit = &unit
	}

	// Get composite schedule from store
	schedule, err := s.store.GetCompositeSchedule(r.Context(), csId, params.ConnectorId, duration, chargingRateUnit)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Convert to API response format
	response := CompositeSchedule{
		ConnectorId:      int32(params.ConnectorId),
		ScheduleStart:    s.clock.Now(),
		ChargingSchedule: convertStoreScheduleToAPI(schedule),
	}

	_ = render.Render(w, r, &response)
}

// Helper functions

func convertStoreProfileToAPI(profile *store.ChargingProfile) ChargingProfileWithConnector {
	apiProfile := ChargingProfileWithConnector{
		ChargingProfileId:      int32(profile.ChargingProfileId),
		StackLevel:             int32(profile.StackLevel),
		ChargingProfilePurpose: ChargingProfileWithConnectorChargingProfilePurpose(profile.ChargingProfilePurpose),
		ChargingProfileKind:    ChargingProfileWithConnectorChargingProfileKind(profile.ChargingProfileKind),
		ChargingSchedule:       convertStoreScheduleToAPI(&profile.ChargingSchedule),
		ConnectorId:            int32(profile.ConnectorId),
	}

	if profile.TransactionId != nil {
		txId := int32(*profile.TransactionId)
		apiProfile.TransactionId = &txId
	}
	if profile.RecurrencyKind != nil {
		rk := ChargingProfileWithConnectorRecurrencyKind(*profile.RecurrencyKind)
		apiProfile.RecurrencyKind = &rk
	}
	if profile.ValidFrom != nil {
		apiProfile.ValidFrom = profile.ValidFrom
	}
	if profile.ValidTo != nil {
		apiProfile.ValidTo = profile.ValidTo
	}

	return apiProfile
}

func convertStoreScheduleToAPI(schedule *store.ChargingSchedule) ChargingSchedule {
	apiSchedule := ChargingSchedule{
		ChargingRateUnit:       ChargingScheduleChargingRateUnit(schedule.ChargingRateUnit),
		ChargingSchedulePeriod: make([]ChargingSchedulePeriod, 0),
	}

	if schedule.Duration != nil {
		duration := int32(*schedule.Duration)
		apiSchedule.Duration = &duration
	}
	if schedule.StartSchedule != nil {
		apiSchedule.StartSchedule = schedule.StartSchedule
	}
	if schedule.MinChargingRate != nil {
		apiSchedule.MinChargingRate = schedule.MinChargingRate
	}

	for _, period := range schedule.ChargingSchedulePeriod {
		apiPeriod := ChargingSchedulePeriod{
			StartPeriod: int32(period.StartPeriod),
			Limit:       period.Limit,
		}
		if period.NumberPhases != nil {
			numPhases := int32(*period.NumberPhases)
			apiPeriod.NumberPhases = &numPhases
		}
		apiSchedule.ChargingSchedulePeriod = append(apiSchedule.ChargingSchedulePeriod, apiPeriod)
	}

	return apiSchedule
}

// Implement Renderer interface for responses

func (c *ChargingProfileList) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func (c *CompositeSchedule) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

// Implement Binder interface for requests

func (s *SetChargingProfileRequest) Bind(r *http.Request) error {
	// Validate required fields
	if len(s.CsChargingProfiles.ChargingSchedule.ChargingSchedulePeriod) == 0 {
		return fmt.Errorf("at least one charging schedule period is required")
	}
	return nil
}
