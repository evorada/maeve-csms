// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Server) GetMeterValues(w http.ResponseWriter, r *http.Request, csId string, params GetMeterValuesParams) {
	// Build filter from query parameters
	filter := store.MeterValuesFilter{
		ChargeStationId: csId,
		ConnectorId:     params.ConnectorId,
		TransactionId:   params.TransactionId,
		Limit:           100, // default
		Offset:          0,
	}

	if params.StartTime != nil {
		startTimeStr := params.StartTime.Format(time.RFC3339)
		filter.StartTime = &startTimeStr
	}
	if params.EndTime != nil {
		endTimeStr := params.EndTime.Format(time.RFC3339)
		filter.EndTime = &endTimeStr
	}
	if params.Limit != nil {
		if *params.Limit > 0 && *params.Limit <= 1000 {
			filter.Limit = *params.Limit
		}
	}
	if params.Offset != nil && *params.Offset >= 0 {
		filter.Offset = *params.Offset
	}

	result, err := s.store.QueryMeterValues(r.Context(), filter)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Convert store types to API types
	apiMeterValues := make([]MeterValue, len(result.MeterValues))
	for i, mv := range result.MeterValues {
		apiSampledValues := make([]MeterValuesSampledValue, len(mv.MeterValue.SampledValues))
		for j, sv := range mv.MeterValue.SampledValues {
			var unit *string
			if sv.UnitOfMeasure != nil {
				unit = &sv.UnitOfMeasure.Unit
			}
			apiSampledValues[j] = MeterValuesSampledValue{
				Value:     fmt.Sprintf("%f", sv.Value),
				Context:   sv.Context,
				Format:    nil, // not stored in current schema
				Measurand: sv.Measurand,
				Phase:     sv.Phase,
				Location:  sv.Location,
				Unit:      unit,
			}
		}

		connectorId := &mv.EvseId
		var transactionId *string
		if mv.TransactionId != "" {
			transactionId = &mv.TransactionId
		}

		// Parse timestamp from string
		timestamp, err := time.Parse(time.RFC3339, mv.MeterValue.Timestamp)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(fmt.Errorf("invalid timestamp: %w", err)))
			return
		}

		apiMeterValues[i] = MeterValue{
			Timestamp:     timestamp,
			ConnectorId:   connectorId,
			EvseId:        &mv.EvseId,
			TransactionId: transactionId,
			SampledValue:  apiSampledValues,
		}
	}

	resp := &MeterValuesResponse{
		MeterValues: apiMeterValues,
		Total:       result.Total,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	}

	_ = render.Render(w, r, resp)
}

// Render implementations

func (m MeterValuesResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
