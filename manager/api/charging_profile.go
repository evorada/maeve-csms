// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// SetChargingProfile sets a charging profile on a connector of the charge station. For
// an OCPP 1.6 charge station it queues a SetChargingProfile command with the profile and
// answers it: the result handler of SetChargingProfile stores the profile when the
// charge station accepts it. Other charge stations get the profile in the store at once.
func (s *Server) SetChargingProfile(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(SetChargingProfileRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	profile := chargingProfileOf(csId, req)
	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "SetChargingProfile", setChargingProfileOf(profile)) {
		return
	}

	err := s.store.SetChargingProfile(r.Context(), profile)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// chargingProfileOf is the charging profile of the request as the manager stores it.
func chargingProfileOf(csId string, req *SetChargingProfileRequest) *store.ChargingProfile {
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

	return profile
}

// setChargingProfileOf is the SetChargingProfile request of OCPP 1.6 that sets the
// charging profile: what the result handler of SetChargingProfile stores when the
// charge station accepts it is the profile again.
func setChargingProfileOf(profile *store.ChargingProfile) *ocpp16.SetChargingProfileJson {
	schedule := ocpp16.SetChargingProfileJsonChargingSchedule{
		ChargingRateUnit:       ocpp16.SetChargingProfileJsonChargingRateUnit(profile.ChargingSchedule.ChargingRateUnit),
		Duration:               profile.ChargingSchedule.Duration,
		StartSchedule:          optionalOcppDateTime(profile.ChargingSchedule.StartSchedule),
		MinChargingRate:        profile.ChargingSchedule.MinChargingRate,
		ChargingSchedulePeriod: make([]ocpp16.SetChargingProfileJsonChargingSchedulePeriod, 0, len(profile.ChargingSchedule.ChargingSchedulePeriod)),
	}
	for _, period := range profile.ChargingSchedule.ChargingSchedulePeriod {
		schedule.ChargingSchedulePeriod = append(schedule.ChargingSchedulePeriod, ocpp16.SetChargingProfileJsonChargingSchedulePeriod{
			StartPeriod:  period.StartPeriod,
			Limit:        period.Limit,
			NumberPhases: period.NumberPhases,
		})
	}

	chargingProfile := ocpp16.SetChargingProfileJsonCsChargingProfiles{
		ChargingProfileId:      profile.ChargingProfileId,
		TransactionId:          profile.TransactionId,
		StackLevel:             profile.StackLevel,
		ChargingProfilePurpose: ocpp16.SetChargingProfileJsonChargingProfilePurpose(profile.ChargingProfilePurpose),
		ChargingProfileKind:    ocpp16.SetChargingProfileJsonChargingProfileKind(profile.ChargingProfileKind),
		ValidFrom:              optionalOcppDateTime(profile.ValidFrom),
		ValidTo:                optionalOcppDateTime(profile.ValidTo),
		ChargingSchedule:       schedule,
	}
	if profile.RecurrencyKind != nil {
		recurrencyKind := ocpp16.SetChargingProfileJsonRecurrencyKind(*profile.RecurrencyKind)
		chargingProfile.RecurrencyKind = &recurrencyKind
	}

	return &ocpp16.SetChargingProfileJson{
		ConnectorId:        profile.ConnectorId,
		CsChargingProfiles: chargingProfile,
	}
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

// ClearChargingProfile clears a charging profile of the charge station. For an OCPP 1.6
// charge station it queues a ClearChargingProfile command for the profile id and the
// other criteria of the request and answers it: the result handler of
// ClearChargingProfile clears the matching profiles from the store when the charge
// station accepts. Other charge stations get them cleared from the store at once.
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

	payload := &ocpp16.ClearChargingProfileJson{Id: &pid, ConnectorId: connectorId, StackLevel: stackLevel}
	if params.ChargingProfilePurpose != nil {
		p := ocpp16.ClearChargingProfileJsonChargingProfilePurpose(*params.ChargingProfilePurpose)
		payload.ChargingProfilePurpose = &p
	}
	if s.answerThroughQueue(w, r, csId, http.StatusAccepted, "ClearChargingProfile", payload) {
		return
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
