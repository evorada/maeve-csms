// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) ReconfigureChargeStation(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(ChargeStationSettings)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	chargeStationSettings := make(map[string]*store.ChargeStationSetting)
	for k, v := range *req {
		chargeStationSettings[k] = &store.ChargeStationSetting{
			Value:  v,
			Status: store.ChargeStationSettingStatusPending,
		}
	}

	err := s.store.UpdateChargeStationSettings(r.Context(), csId, &store.ChargeStationSettings{
		Settings: chargeStationSettings,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
}

func (s *Server) GetChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationConfigurationParams) {
	settings, err := s.store.LookupChargeStationSettings(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	if settings == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	// Build response
	resp := &ConfigurationResponse{
		UnknownKey: &[]string{},
	}

	// Filter by keys if specified
	var requestedKeys map[string]bool
	if params.Key != nil && *params.Key != "" {
		requestedKeys = make(map[string]bool)
		for _, key := range splitKeys(*params.Key) {
			requestedKeys[key] = true
		}
	}

	for key, setting := range settings.Settings {
		// Skip if filtering and key not requested
		if requestedKeys != nil && !requestedKeys[key] {
			continue
		}

		valueCopy := setting.Value
		resp.ConfigurationKey = append(resp.ConfigurationKey, struct {
			Key      string  `json:"key"`
			Readonly bool    `json:"readonly"`
			Value    *string `json:"value,omitempty"`
		}{
			Key:      key,
			Readonly: false, // TODO: Track readonly status in store
			Value:    &valueCopy,
		})
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

func (s *Server) ChangeChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string) {
	var req ConfigurationChangeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Convert request map to ChargeStationSettings
	settings := &store.ChargeStationSettings{
		ChargeStationId: csId,
		Settings:        make(map[string]*store.ChargeStationSetting),
	}

	for key, value := range req {
		settings.Settings[key] = &store.ChargeStationSetting{
			Value:     value,
			Status:    store.ChargeStationSettingStatusPending,
			SendAfter: s.clock.Now(),
		}
	}

	err := s.store.UpdateChargeStationSettings(r.Context(), csId, settings)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Build response with pending status for all keys
	var resp ConfigurationChangeResponse

	for key := range req {
		resp.Results = append(resp.Results, struct {
			Key    string                                   `json:"key"`
			Status ConfigurationChangeResponseResultsStatus `json:"status"`
		}{
			Key:    key,
			Status: "Accepted", // Will be updated when ChangeConfigurationResult is received
		})
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, resp)
}

func (s *Server) GetChargeStationVariables(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationVariablesParams) {
	// OCPP 2.0.1 variables support - TODO: Implement full variable caching
	// For now, return empty response
	resp := VariablesResponse{
		Variables: []struct {
			Component struct {
				Evse *struct {
					ConnectorId *int `json:"connectorId,omitempty"`
					Id          *int `json:"id,omitempty"`
				} `json:"evse,omitempty"`
				Instance *string `json:"instance,omitempty"`
				Name     string  `json:"name"`
			} `json:"component"`
			Variable struct {
				Instance *string `json:"instance,omitempty"`
				Name     string  `json:"name"`
			} `json:"variable"`
			VariableAttribute []struct {
				Constant   *bool                                                  `json:"constant,omitempty"`
				Mutability *VariablesResponseVariablesVariableAttributeMutability `json:"mutability,omitempty"`
				Persistent *bool                                                  `json:"persistent,omitempty"`
				Value      *string                                                `json:"value,omitempty"`
			} `json:"variableAttribute"`
		}{},
	}

	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
}

func (s *Server) SetChargeStationVariables(w http.ResponseWriter, r *http.Request, csId string) {
	var req VariablesChangeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// OCPP 2.0.1 variables support - TODO: Implement full variable store
	// For now, return accepted for all variables
	var resp VariablesChangeResponse

	for _, v := range req.Variables {
		resp.Results = append(resp.Results, struct {
			AttributeStatus VariablesChangeResponseResultsAttributeStatus `json:"attributeStatus"`
			Component       struct {
				Name string `json:"name"`
			} `json:"component"`
			Variable struct {
				Name string `json:"name"`
			} `json:"variable"`
		}{
			AttributeStatus: "Accepted",
			Component: struct {
				Name string `json:"name"`
			}{Name: v.Component.Name},
			Variable: struct {
				Name string `json:"name"`
			}{Name: v.Variable.Name},
		})
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, resp)
}

// Render implementations for configuration-related types

func (c ChargeStationSettings) Bind(r *http.Request) error {
	return nil
}

// Helper functions

// splitKeys splits a comma-separated list of keys
func splitKeys(keys string) []string {
	var result []string
	for _, k := range splitBy(keys, ',') {
		if trimmed := trimSpace(k); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitBy(s string, sep rune) []string {
	var result []string
	var current string
	for _, c := range s {
		if c == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" || len(result) > 0 {
		result = append(result, current)
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
