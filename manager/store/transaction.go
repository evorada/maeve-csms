// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

type Transaction struct {
	ChargeStationId   string       `firestore:"chargeStationId"`
	TransactionId     string       `firestore:"transactionId"`
	IdToken           string       `firestore:"idToken"`
	TokenType         string       `firestore:"tokenType"`
	MeterValues       []MeterValue `firestore:"meterValues"`
	StartSeqNo        int          `firestore:"startSeqNo"`
	EndedSeqNo        int          `firestore:"endedSeqNo"`
	UpdatedSeqNoCount int          `firestore:"updatedSeqNoCount"`
	Offline           bool         `firestore:"offline"`
	// LastCost is the most recently communicated running cost for this transaction (from CostUpdated).
	LastCost *float64 `firestore:"lastCost,omitempty"`
}

type MeterValue struct {
	SampledValues []SampledValue `firestore:"sampledValue"`
	Timestamp     string         `firestore:"timestamp"`
}

type SampledValue struct {
	Context       *string        `firestore:"context"`
	Location      *string        `firestore:"location"`
	Measurand     *string        `firestore:"measurand"`
	Phase         *string        `firestore:"phase"`
	UnitOfMeasure *UnitOfMeasure `firestore:"unitOfMeasure"`
	Value         float64        `firestore:"value"`
}

type UnitOfMeasure struct {
	Unit      string `firestore:"unit"`
	Multipler int    `firestore:"multipler"`
}

type TransactionStore interface {
	Transactions(ctx context.Context) ([]*Transaction, error)
	FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*Transaction, error)
	CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []MeterValue, seqNo int, offline bool) error
	UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValue []MeterValue) error
	EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []MeterValue, seqNo int) error
	// UpdateTransactionCost stores the most recent running cost for a transaction as
	// communicated by the CSMS via the CostUpdated message.
	UpdateTransactionCost(ctx context.Context, chargeStationId, transactionId string, totalCost float64) error
	// ListTransactionsForChargeStation retrieves transactions for a specific charge station with filtering and pagination
	ListTransactionsForChargeStation(ctx context.Context, chargeStationId, status string, startDate, endDate *time.Time, limit, offset int) ([]*Transaction, int64, error)
}

type RemoteTransactionRequestStatus string

var (
	RemoteTransactionRequestStatusPending  RemoteTransactionRequestStatus = "Pending"
	RemoteTransactionRequestStatusAccepted RemoteTransactionRequestStatus = "Accepted"
	RemoteTransactionRequestStatusRejected RemoteTransactionRequestStatus = "Rejected"
)

type RemoteTransactionRequestType string

var (
	RemoteTransactionRequestTypeStart RemoteTransactionRequestType = "Start"
	RemoteTransactionRequestTypeStop  RemoteTransactionRequestType = "Stop"
)

type RemoteStartTransactionRequest struct {
	ChargeStationId string
	IdTag           string
	ConnectorId     *int
	ChargingProfile *string
	Status          RemoteTransactionRequestStatus
	SendAfter       time.Time
	RequestType     RemoteTransactionRequestType
}

type RemoteStopTransactionRequest struct {
	ChargeStationId string
	TransactionId   string
	Status          RemoteTransactionRequestStatus
	SendAfter       time.Time
	RequestType     RemoteTransactionRequestType
}

type RemoteTransactionRequestStore interface {
	SetRemoteStartTransactionRequest(ctx context.Context, chargeStationId string, request *RemoteStartTransactionRequest) error
	GetRemoteStartTransactionRequest(ctx context.Context, chargeStationId string) (*RemoteStartTransactionRequest, error)
	DeleteRemoteStartTransactionRequest(ctx context.Context, chargeStationId string) error
	ListRemoteStartTransactionRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*RemoteStartTransactionRequest, error)
	SetRemoteStopTransactionRequest(ctx context.Context, chargeStationId string, request *RemoteStopTransactionRequest) error
	GetRemoteStopTransactionRequest(ctx context.Context, chargeStationId string) (*RemoteStopTransactionRequest, error)
	DeleteRemoteStopTransactionRequest(ctx context.Context, chargeStationId string) error
	ListRemoteStopTransactionRequests(ctx context.Context, pageSize int, previousChargeStationId string) ([]*RemoteStopTransactionRequest, error)
}
