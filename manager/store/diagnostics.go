// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// DiagnosticsRequestStatus represents whether a diagnostics request is pending or processed
type DiagnosticsRequestStatus string

var (
	DiagnosticsRequestStatusPending  DiagnosticsRequestStatus = "Pending"
	DiagnosticsRequestStatusAccepted DiagnosticsRequestStatus = "Accepted"
	DiagnosticsRequestStatusRejected DiagnosticsRequestStatus = "Rejected"
)

// DiagnosticsRequest represents a diagnostics upload request for OCPP 1.6
type DiagnosticsRequest struct {
	ChargeStationId string
	Location        string
	StartTime       *time.Time
	StopTime        *time.Time
	Retries         *int
	RetryInterval   *int
	Status          DiagnosticsRequestStatus
	SendAfter       time.Time
}

// LogRequestStatus represents whether a log request is pending or processed
type LogRequestStatus string

var (
	LogRequestStatusPending  LogRequestStatus = "Pending"
	LogRequestStatusAccepted LogRequestStatus = "Accepted"
	LogRequestStatusRejected LogRequestStatus = "Rejected"
)

// LogRequest represents a log upload request for OCPP 2.0.1
type LogRequest struct {
	ChargeStationId string
	LogType         string // "DiagnosticsLog" or "SecurityLog"
	RequestId       int
	RemoteLocation  string
	OldestTimestamp *time.Time
	LatestTimestamp *time.Time
	Retries         *int
	RetryInterval   *int
	Status          LogRequestStatus
	SendAfter       time.Time
}

// DiagnosticsRequestStore defines the interface for managing diagnostics requests
type DiagnosticsRequestStore interface {
	SetDiagnosticsRequest(ctx context.Context, chargeStationId string, request *DiagnosticsRequest) error
	GetDiagnosticsRequest(ctx context.Context, chargeStationId string) (*DiagnosticsRequest, error)
	DeleteDiagnosticsRequest(ctx context.Context, chargeStationId string) error
	ListDiagnosticsRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*DiagnosticsRequest, error)
}

// LogRequestStore defines the interface for managing log requests
type LogRequestStore interface {
	SetLogRequest(ctx context.Context, chargeStationId string, request *LogRequest) error
	GetLogRequest(ctx context.Context, chargeStationId string) (*LogRequest, error)
	DeleteLogRequest(ctx context.Context, chargeStationId string) error
	ListLogRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*LogRequest, error)
}
