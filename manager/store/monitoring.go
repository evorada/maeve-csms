// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// MonitoringType represents the type of variable monitoring
type MonitoringType string

var (
	MonitoringTypeUpperThreshold       MonitoringType = "UpperThreshold"
	MonitoringTypeLowerThreshold       MonitoringType = "LowerThreshold"
	MonitoringTypeDelta                MonitoringType = "Delta"
	MonitoringTypePeriodic             MonitoringType = "Periodic"
	MonitoringTypePeriodicClockAligned MonitoringType = "PeriodicClockAligned"
)

// VariableMonitoringConfig represents a monitoring configuration for a variable
type VariableMonitoringConfig struct {
	Id                int
	ChargeStationId   string
	ComponentName     string
	ComponentInstance *string
	VariableName      string
	VariableInstance  *string
	MonitorType       MonitoringType
	Value             float64
	Severity          int
	Transaction       bool
	CreatedAt         time.Time
}

// ChargeStationEvent represents an event reported by a charge station
type ChargeStationEvent struct {
	Id              int
	ChargeStationId string
	Timestamp       time.Time
	EventType       string
	TechCode        *string
	TechInfo        *string
	EventData       *string
	ComponentId     *string
	VariableId      *string
	Cleared         bool
	CreatedAt       time.Time
}

// DeviceReport represents a device model report from a charge station
type DeviceReport struct {
	Id              int
	ChargeStationId string
	RequestId       int
	GeneratedAt     time.Time
	ReportType      *string
	ReportData      *string // JSON-encoded report data
	CreatedAt       time.Time
}

// VariableMonitoringStore defines the interface for managing variable monitoring configurations
type VariableMonitoringStore interface {
	SetVariableMonitoring(ctx context.Context, chargeStationId string, config *VariableMonitoringConfig) error
	GetVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) (*VariableMonitoringConfig, error)
	DeleteVariableMonitoring(ctx context.Context, chargeStationId string, monitorId int) error
	ListVariableMonitoring(ctx context.Context, chargeStationId string, offset int, limit int) ([]*VariableMonitoringConfig, error)
}

// ChargeStationEventStore defines the interface for managing charge station events
type ChargeStationEventStore interface {
	AddChargeStationEvent(ctx context.Context, chargeStationId string, event *ChargeStationEvent) error
	ListChargeStationEvents(ctx context.Context, chargeStationId string, offset int, limit int) ([]*ChargeStationEvent, int, error)
}

// DeviceReportStore defines the interface for managing device reports
type DeviceReportStore interface {
	AddDeviceReport(ctx context.Context, chargeStationId string, report *DeviceReport) error
	ListDeviceReports(ctx context.Context, chargeStationId string, offset int, limit int) ([]*DeviceReport, int, error)
}
