// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
)

// Variable Monitoring

type firestoreVariableMonitoringConfig struct {
	ChargeStationId   string  `firestore:"chargeStationId"`
	ComponentName     string  `firestore:"componentName"`
	ComponentInstance *string `firestore:"componentInstance,omitempty"`
	VariableName      string  `firestore:"variableName"`
	VariableInstance  *string `firestore:"variableInstance,omitempty"`
	MonitorType       string  `firestore:"monitorType"`
	Value             float64 `firestore:"value"`
	Severity          int     `firestore:"severity"`
	Transaction       bool    `firestore:"transaction"`
	CreatedAt         string  `firestore:"createdAt"`
}

func (s *Store) SetVariableMonitoring(ctx context.Context, chargeStationId string, config *store.VariableMonitoringConfig) error {
	collection := s.client.Collection("ChargeStations").Doc(chargeStationId).Collection("VariableMonitoring")

	var docRef *firestore.DocumentRef
	if config.Id != 0 {
		docRef = collection.Doc(fmt.Sprintf("%d", config.Id))
	} else {
		docRef = collection.NewDoc()
	}

	doc := &firestoreVariableMonitoringConfig{
		ChargeStationId:   chargeStationId,
		ComponentName:     config.ComponentName,
		ComponentInstance: config.ComponentInstance,
		VariableName:      config.VariableName,
		VariableInstance:  config.VariableInstance,
		MonitorType:       string(config.MonitorType),
		Value:             config.Value,
		Severity:          config.Severity,
		Transaction:       config.Transaction,
		CreatedAt:         time.Now().Format(time.RFC3339),
	}

	_, err := docRef.Set(ctx, doc)
	if err != nil {
		return fmt.Errorf("setting variable monitoring for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) GetVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) (*store.VariableMonitoringConfig, error) {
	snap, err := s.client.Collection("ChargeStations").Doc(chargeStationId).
		Collection("VariableMonitoring").Doc(fmt.Sprintf("%d", monitorId)).Get(ctx)
	if err != nil {
		return nil, nil
	}

	var doc firestoreVariableMonitoringConfig
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("decoding variable monitoring: %w", err)
	}

	result := &store.VariableMonitoringConfig{
		Id:                monitorId,
		ChargeStationId:   chargeStationId,
		ComponentName:     doc.ComponentName,
		ComponentInstance: doc.ComponentInstance,
		VariableName:      doc.VariableName,
		VariableInstance:  doc.VariableInstance,
		MonitorType:       store.MonitoringType(doc.MonitorType),
		Value:             doc.Value,
		Severity:          doc.Severity,
		Transaction:       doc.Transaction,
	}

	if createdAt, err := time.Parse(time.RFC3339, doc.CreatedAt); err == nil {
		result.CreatedAt = createdAt
	}

	return result, nil
}

func (s *Store) DeleteVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) error {
	_, err := s.client.Collection("ChargeStations").Doc(chargeStationId).
		Collection("VariableMonitoring").Doc(fmt.Sprintf("%d", monitorId)).Delete(ctx)
	if err != nil {
		return fmt.Errorf("deleting variable monitoring %d for %s: %w", monitorId, chargeStationId, err)
	}
	return nil
}

func (s *Store) ListVariableMonitoring(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.VariableMonitoringConfig, error) {
	iter := s.client.Collection("ChargeStations").Doc(chargeStationId).
		Collection("VariableMonitoring").
		OrderBy(firestore.DocumentID, firestore.Asc).
		Offset(offset).
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()

	var results []*store.VariableMonitoringConfig
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("listing variable monitoring for %s: %w", chargeStationId, err)
		}

		var doc firestoreVariableMonitoringConfig
		if err := snap.DataTo(&doc); err != nil {
			return nil, fmt.Errorf("decoding variable monitoring: %w", err)
		}

		result := &store.VariableMonitoringConfig{
			ChargeStationId:   chargeStationId,
			ComponentName:     doc.ComponentName,
			ComponentInstance: doc.ComponentInstance,
			VariableName:      doc.VariableName,
			VariableInstance:  doc.VariableInstance,
			MonitorType:       store.MonitoringType(doc.MonitorType),
			Value:             doc.Value,
			Severity:          doc.Severity,
			Transaction:       doc.Transaction,
		}

		if createdAt, err := time.Parse(time.RFC3339, doc.CreatedAt); err == nil {
			result.CreatedAt = createdAt
		}

		results = append(results, result)
	}

	if results == nil {
		return []*store.VariableMonitoringConfig{}, nil
	}
	return results, nil
}

// Charge Station Events

type firestoreChargeStationEvent struct {
	ChargeStationId string  `firestore:"chargeStationId"`
	Timestamp       string  `firestore:"timestamp"`
	EventType       string  `firestore:"eventType"`
	TechCode        *string `firestore:"techCode,omitempty"`
	TechInfo        *string `firestore:"techInfo,omitempty"`
	EventData       *string `firestore:"eventData,omitempty"`
	ComponentId     *string `firestore:"componentId,omitempty"`
	VariableId      *string `firestore:"variableId,omitempty"`
	Cleared         bool    `firestore:"cleared"`
	CreatedAt       string  `firestore:"createdAt"`
}

func (s *Store) AddChargeStationEvent(ctx context.Context, chargeStationId string, event *store.ChargeStationEvent) error {
	collection := s.client.Collection("ChargeStations").Doc(chargeStationId).Collection("Events")

	doc := &firestoreChargeStationEvent{
		ChargeStationId: chargeStationId,
		Timestamp:       event.Timestamp.Format(time.RFC3339),
		EventType:       event.EventType,
		TechCode:        event.TechCode,
		TechInfo:        event.TechInfo,
		EventData:       event.EventData,
		ComponentId:     event.ComponentId,
		VariableId:      event.VariableId,
		Cleared:         event.Cleared,
		CreatedAt:       time.Now().Format(time.RFC3339),
	}

	_, _, err := collection.Add(ctx, doc)
	if err != nil {
		return fmt.Errorf("adding event for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) ListChargeStationEvents(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.ChargeStationEvent, int, error) {
	collection := s.client.Collection("ChargeStations").Doc(chargeStationId).Collection("Events")

	// Get total count
	allDocs := collection.Documents(ctx)
	total := 0
	for {
		_, err := allDocs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			allDocs.Stop()
			return nil, 0, fmt.Errorf("counting events for %s: %w", chargeStationId, err)
		}
		total++
	}
	allDocs.Stop()

	// Get paginated results
	iter := collection.
		OrderBy("timestamp", firestore.Desc).
		Offset(offset).
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()

	var results []*store.ChargeStationEvent
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("listing events for %s: %w", chargeStationId, err)
		}

		var doc firestoreChargeStationEvent
		if err := snap.DataTo(&doc); err != nil {
			return nil, 0, fmt.Errorf("decoding event: %w", err)
		}

		result := &store.ChargeStationEvent{
			ChargeStationId: chargeStationId,
			EventType:       doc.EventType,
			TechCode:        doc.TechCode,
			TechInfo:        doc.TechInfo,
			EventData:       doc.EventData,
			ComponentId:     doc.ComponentId,
			VariableId:      doc.VariableId,
			Cleared:         doc.Cleared,
		}

		if ts, err := time.Parse(time.RFC3339, doc.Timestamp); err == nil {
			result.Timestamp = ts
		}
		if createdAt, err := time.Parse(time.RFC3339, doc.CreatedAt); err == nil {
			result.CreatedAt = createdAt
		}

		results = append(results, result)
	}

	if results == nil {
		return []*store.ChargeStationEvent{}, total, nil
	}
	return results, total, nil
}

// Device Reports

type firestoreDeviceReport struct {
	ChargeStationId string  `firestore:"chargeStationId"`
	RequestId       int     `firestore:"requestId"`
	GeneratedAt     string  `firestore:"generatedAt"`
	ReportType      *string `firestore:"reportType,omitempty"`
	ReportData      *string `firestore:"reportData,omitempty"`
	CreatedAt       string  `firestore:"createdAt"`
}

func (s *Store) AddDeviceReport(ctx context.Context, chargeStationId string, report *store.DeviceReport) error {
	collection := s.client.Collection("ChargeStations").Doc(chargeStationId).Collection("DeviceReports")

	doc := &firestoreDeviceReport{
		ChargeStationId: chargeStationId,
		RequestId:       report.RequestId,
		GeneratedAt:     report.GeneratedAt.Format(time.RFC3339),
		ReportType:      report.ReportType,
		ReportData:      report.ReportData,
		CreatedAt:       time.Now().Format(time.RFC3339),
	}

	_, _, err := collection.Add(ctx, doc)
	if err != nil {
		return fmt.Errorf("adding device report for %s: %w", chargeStationId, err)
	}
	return nil
}

func (s *Store) ListDeviceReports(ctx context.Context, chargeStationId string, offset int, limit int) ([]*store.DeviceReport, int, error) {
	collection := s.client.Collection("ChargeStations").Doc(chargeStationId).Collection("DeviceReports")

	// Get total count
	allDocs := collection.Documents(ctx)
	total := 0
	for {
		_, err := allDocs.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			allDocs.Stop()
			return nil, 0, fmt.Errorf("counting device reports for %s: %w", chargeStationId, err)
		}
		total++
	}
	allDocs.Stop()

	// Get paginated results
	iter := collection.
		OrderBy("generatedAt", firestore.Desc).
		Offset(offset).
		Limit(limit).
		Documents(ctx)
	defer iter.Stop()

	var results []*store.DeviceReport
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("listing device reports for %s: %w", chargeStationId, err)
		}

		var doc firestoreDeviceReport
		if err := snap.DataTo(&doc); err != nil {
			return nil, 0, fmt.Errorf("decoding device report: %w", err)
		}

		result := &store.DeviceReport{
			ChargeStationId: chargeStationId,
			RequestId:       doc.RequestId,
			ReportType:      doc.ReportType,
			ReportData:      doc.ReportData,
		}

		if generatedAt, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
			result.GeneratedAt = generatedAt
		}
		if createdAt, err := time.Parse(time.RFC3339, doc.CreatedAt); err == nil {
			result.CreatedAt = createdAt
		}

		results = append(results, result)
	}

	if results == nil {
		return []*store.DeviceReport{}, total, nil
	}
	return results, total, nil
}
