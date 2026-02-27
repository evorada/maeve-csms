// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Variable Monitoring

func (s *Store) SetVariableMonitoring(ctx context.Context, chargeStationId string, config *store.VariableMonitoringConfig) error {
	var componentInstance pgtype.Text
	if config.ComponentInstance != nil {
		componentInstance = pgtype.Text{String: *config.ComponentInstance, Valid: true}
	}

	var variableInstance pgtype.Text
	if config.VariableInstance != nil {
		variableInstance = pgtype.Text{String: *config.VariableInstance, Valid: true}
	}

	if config.Id != 0 {
		err := s.writeQueries().UpsertVariableMonitoringWithId(ctx, UpsertVariableMonitoringWithIdParams{
			ID:                int32(config.Id),
			ChargeStationID:   chargeStationId,
			ComponentName:     config.ComponentName,
			ComponentInstance: componentInstance,
			VariableName:      config.VariableName,
			VariableInstance:  variableInstance,
			MonitorType:       string(config.MonitorType),
			Value:             config.Value,
			Severity:          int32(config.Severity),
			Transaction:       config.Transaction,
		})
		if err != nil {
			return fmt.Errorf("failed to set variable monitoring: %w", err)
		}
		return nil
	}

	id, err := s.writeQueries().UpsertVariableMonitoring(ctx, UpsertVariableMonitoringParams{
		ChargeStationID:   chargeStationId,
		ComponentName:     config.ComponentName,
		ComponentInstance: componentInstance,
		VariableName:      config.VariableName,
		VariableInstance:  variableInstance,
		MonitorType:       string(config.MonitorType),
		Value:             config.Value,
		Severity:          int32(config.Severity),
		Transaction:       config.Transaction,
	})
	if err != nil {
		return fmt.Errorf("failed to set variable monitoring: %w", err)
	}
	config.Id = int(id)
	return nil
}

func (s *Store) GetVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) (*store.VariableMonitoringConfig, error) {
	row, err := s.readQueries().GetVariableMonitoring(ctx, GetVariableMonitoringParams{
		ChargeStationID: chargeStationId,
		ID:              int32(monitorId),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get variable monitoring: %w", err)
	}

	result := &store.VariableMonitoringConfig{
		Id:              int(row.ID),
		ChargeStationId: row.ChargeStationID,
		ComponentName:   row.ComponentName,
		VariableName:    row.VariableName,
		MonitorType:     store.MonitoringType(row.MonitorType),
		Value:           row.Value,
		Severity:        int(row.Severity),
		Transaction:     row.Transaction,
	}

	if row.ComponentInstance.Valid {
		result.ComponentInstance = &row.ComponentInstance.String
	}
	if row.VariableInstance.Valid {
		result.VariableInstance = &row.VariableInstance.String
	}
	if row.CreatedAt.Valid {
		result.CreatedAt = row.CreatedAt.Time
	}

	return result, nil
}

func (s *Store) DeleteVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) error {
	err := s.writeQueries().DeleteVariableMonitoring(ctx, DeleteVariableMonitoringParams{
		ChargeStationID: chargeStationId,
		ID:              int32(monitorId),
	})
	if err != nil {
		return fmt.Errorf("failed to delete variable monitoring: %w", err)
	}
	return nil
}

func (s *Store) ListVariableMonitoring(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.VariableMonitoringConfig, error) {
	rows, err := s.readQueries().ListVariableMonitoring(ctx, ListVariableMonitoringParams{
		ChargeStationID: chargeStationId,
		Limit:           int32(limit),
		Offset:          int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list variable monitoring: %w", err)
	}

	var results []*store.VariableMonitoringConfig
	for _, row := range rows {
		result := &store.VariableMonitoringConfig{
			Id:              int(row.ID),
			ChargeStationId: row.ChargeStationID,
			ComponentName:   row.ComponentName,
			VariableName:    row.VariableName,
			MonitorType:     store.MonitoringType(row.MonitorType),
			Value:           row.Value,
			Severity:        int(row.Severity),
			Transaction:     row.Transaction,
		}
		if row.ComponentInstance.Valid {
			result.ComponentInstance = &row.ComponentInstance.String
		}
		if row.VariableInstance.Valid {
			result.VariableInstance = &row.VariableInstance.String
		}
		if row.CreatedAt.Valid {
			result.CreatedAt = row.CreatedAt.Time
		}
		results = append(results, result)
	}

	if results == nil {
		return []*store.VariableMonitoringConfig{}, nil
	}
	return results, nil
}

// Charge Station Events

func (s *Store) AddChargeStationEvent(ctx context.Context, chargeStationId string, event *store.ChargeStationEvent) error {
	var techCode, techInfo, eventData, componentId, variableId pgtype.Text
	if event.TechCode != nil {
		techCode = pgtype.Text{String: *event.TechCode, Valid: true}
	}
	if event.TechInfo != nil {
		techInfo = pgtype.Text{String: *event.TechInfo, Valid: true}
	}
	if event.EventData != nil {
		eventData = pgtype.Text{String: *event.EventData, Valid: true}
	}
	if event.ComponentId != nil {
		componentId = pgtype.Text{String: *event.ComponentId, Valid: true}
	}
	if event.VariableId != nil {
		variableId = pgtype.Text{String: *event.VariableId, Valid: true}
	}

	id, err := s.writeQueries().InsertChargeStationEvent(ctx, InsertChargeStationEventParams{
		ChargeStationID: chargeStationId,
		Timestamp:       pgtype.Timestamptz{Time: event.Timestamp, Valid: true},
		EventType:       event.EventType,
		TechCode:        techCode,
		TechInfo:        techInfo,
		EventData:       eventData,
		ComponentID:     componentId,
		VariableID:      variableId,
		Cleared:         event.Cleared,
	})
	if err != nil {
		return fmt.Errorf("failed to insert charge station event: %w", err)
	}
	event.Id = int(id)
	return nil
}

func (s *Store) ListChargeStationEvents(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.ChargeStationEvent, int, error) {
	count, err := s.readQueries().CountChargeStationEvents(ctx, chargeStationId)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count events: %w", err)
	}

	rows, err := s.readQueries().ListChargeStationEvents(ctx, ListChargeStationEventsParams{
		ChargeStationID: chargeStationId,
		Limit:           int32(limit),
		Offset:          int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list events: %w", err)
	}

	var results []*store.ChargeStationEvent
	for _, row := range rows {
		result := &store.ChargeStationEvent{
			Id:              int(row.ID),
			ChargeStationId: row.ChargeStationID,
			EventType:       row.EventType,
			Cleared:         row.Cleared,
		}
		if row.Timestamp.Valid {
			result.Timestamp = row.Timestamp.Time
		}
		if row.TechCode.Valid {
			result.TechCode = &row.TechCode.String
		}
		if row.TechInfo.Valid {
			result.TechInfo = &row.TechInfo.String
		}
		if row.EventData.Valid {
			result.EventData = &row.EventData.String
		}
		if row.ComponentID.Valid {
			result.ComponentId = &row.ComponentID.String
		}
		if row.VariableID.Valid {
			result.VariableId = &row.VariableID.String
		}
		if row.CreatedAt.Valid {
			result.CreatedAt = row.CreatedAt.Time
		}
		results = append(results, result)
	}

	if results == nil {
		return []*store.ChargeStationEvent{}, int(count), nil
	}
	return results, int(count), nil
}

// Device Reports

func (s *Store) AddDeviceReport(ctx context.Context, chargeStationId string, report *store.DeviceReport) error {
	var reportType pgtype.Text
	if report.ReportType != nil {
		reportType = pgtype.Text{String: *report.ReportType, Valid: true}
	}

	var reportData []byte
	if report.ReportData != nil {
		reportData = []byte(*report.ReportData)
	}

	id, err := s.writeQueries().InsertDeviceReport(ctx, InsertDeviceReportParams{
		ChargeStationID: chargeStationId,
		RequestID:       int32(report.RequestId),
		GeneratedAt:     pgtype.Timestamptz{Time: report.GeneratedAt, Valid: true},
		ReportType:      reportType,
		ReportData:      reportData,
	})
	if err != nil {
		return fmt.Errorf("failed to insert device report: %w", err)
	}
	report.Id = int(id)
	return nil
}

func (s *Store) ListDeviceReports(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.DeviceReport, int, error) {
	count, err := s.readQueries().CountDeviceReports(ctx, chargeStationId)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count device reports: %w", err)
	}

	rows, err := s.readQueries().ListDeviceReports(ctx, ListDeviceReportsParams{
		ChargeStationID: chargeStationId,
		Limit:           int32(limit),
		Offset:          int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list device reports: %w", err)
	}

	var results []*store.DeviceReport
	for _, row := range rows {
		result := &store.DeviceReport{
			Id:              int(row.ID),
			ChargeStationId: row.ChargeStationID,
			RequestId:       int(row.RequestID),
		}
		if row.GeneratedAt.Valid {
			result.GeneratedAt = row.GeneratedAt.Time
		}
		if row.ReportType.Valid {
			result.ReportType = &row.ReportType.String
		}
		if row.ReportData != nil {
			reportData := string(row.ReportData)
			result.ReportData = &reportData
		}
		if row.CreatedAt.Valid {
			result.CreatedAt = row.CreatedAt.Time
		}
		results = append(results, result)
	}

	if results == nil {
		return []*store.DeviceReport{}, int(count), nil
	}
	return results, int(count), nil
}
