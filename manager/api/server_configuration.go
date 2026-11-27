// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
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

// GetChargeStationConfiguration answers the configuration of the charge station that the
// manager knows. With refresh, an OCPP 1.6 charge station is asked for its
// configuration instead: a GetConfiguration command is queued and answered (202), and
// the result handler of GetConfiguration keeps the values that the charge station
// answers. Refreshing sends a command, so it needs the permission of the commands as
// well.
func (s *Server) GetChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string, params GetChargeStationConfigurationParams) {
	if params.Refresh != nil && *params.Refresh {
		queue, ok := s.useQueue(w, r, csId)
		if !ok {
			return
		}
		if queue {
			payload := &ocpp16.GetConfigurationJson{}
			if params.Key != nil {
				payload.Key = splitKeys(*params.Key)
			}
			if cmd := s.enqueueFor(w, r, csId, "GetConfiguration", payload); cmd != nil {
				s.renderQueued(w, r, http.StatusAccepted, cmd)
			}
			return
		}
	}

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

// ChangeChargeStationConfiguration changes configuration keys of the charge station. For
// an OCPP 1.6 charge station each key is queued as a ChangeConfiguration command (see
// changeConfigurationThroughQueue). Other charge stations get the keys as pending
// settings in the store, which the settings sync sends.
func (s *Server) ChangeChargeStationConfiguration(w http.ResponseWriter, r *http.Request, csId string) {
	var req ConfigurationChangeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	queue, ok := s.useQueue(w, r, csId)
	if !ok {
		return
	}
	if queue {
		s.changeConfigurationThroughQueue(w, r, csId, req)
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
		resp.Results = append(resp.Results, ConfigurationChangeResult{
			Key:    key,
			Status: ConfigurationChangeResultStatusAccepted, // Will be updated when ChangeConfigurationResult is received
		})
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, resp)
}

// changeConfigurationThroughQueue queues one ChangeConfiguration command per key, in the
// order of the keys, and answers each key as Pending with its command. Nothing is
// written to the settings store, so the settings sync does not send the change a second
// time: the result handler of ChangeConfiguration stores the value with the status that
// the charge station answers. Every key is checked against the OCPP schema before the
// first command is queued, so a request with one key that cannot be sent changes none.
// The caller needs the permission of the operation (cpms.station.write), as for other
// charge stations.
func (s *Server) changeConfigurationThroughQueue(w http.ResponseWriter, r *http.Request, csId string, req ConfigurationChangeRequest) {
	keys := make([]string, 0, len(req))
	for key := range req {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	payloads := make([]*ocpp16.ChangeConfigurationJson, 0, len(keys))
	for _, key := range keys {
		payload := &ocpp16.ChangeConfigurationJson{Key: key, Value: req[key]}
		if err := s.checkCommandPayload(transport.OcppVersion16, "ChangeConfiguration", payload); err != nil {
			renderEnqueueError(w, r, fmt.Errorf("key %q: %w", key, err))
			return
		}
		payloads = append(payloads, payload)
	}

	resp := ConfigurationChangeResponse{Results: make([]ConfigurationChangeResult, 0, len(payloads))}
	for _, payload := range payloads {
		cmd := s.enqueueWithPermission(w, r, csId, tenancy.PermStationWrite, "ChangeConfiguration", payload)
		if cmd == nil {
			return
		}
		id, ok := commandUUID(w, r, cmd)
		if !ok {
			return
		}
		resp.Results = append(resp.Results, ConfigurationChangeResult{
			Key:       payload.Key,
			Status:    ConfigurationChangeResultStatusPending,
			CommandId: &id,
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
