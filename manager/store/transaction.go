// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"
	"math"
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

	// The fields below are written with TransactionStore.UpdateTransactionDetails.
	// A nil pointer means that the value is not known.
	EvseId        *int       `firestore:"evseId,omitempty"`        // 2.0.1 EVSE id; for 1.6 equal to ConnectorId
	ConnectorId   *int       `firestore:"connectorId,omitempty"`   // OCPP connector id
	StartedAt     *time.Time `firestore:"startedAt,omitempty"`     // station timestamp of the start
	StoppedAt     *time.Time `firestore:"stoppedAt,omitempty"`     // station timestamp of the stop
	MeterStartWh  *int64     `firestore:"meterStartWh,omitempty"`  // energy register at start, Wh
	MeterStopWh   *int64     `firestore:"meterStopWh,omitempty"`   // energy register at stop, Wh
	EnergyWh      *int64     `firestore:"energyWh,omitempty"`      // delivered energy, Wh
	StopReason    *string    `firestore:"stopReason,omitempty"`    // OCPP stop reason (1.6 Reason / 2.0.1 StoppedReason)
	IdTokenGroup  *string    `firestore:"idTokenGroup,omitempty"`  // token group id (1.6 parentIdTag) at start
	ReservationId *int       `firestore:"reservationId,omitempty"` // reservation consumed by this transaction

	// OrgId is the org (UUID) that owns the transaction. It is read-only: the store
	// takes it from the charge station when the transaction is created (LegacyOrgId
	// when the charge station is not registered), and it stays the same when the charge
	// station moves to another org. Firestore does not store it: it always reads as "".
	OrgId string `firestore:"-"`

	// The tariff that the transaction is charged with, chosen once, when the transaction
	// starts, and written with TransactionStore.SetTransactionTariff. TariffId and
	// TariffVersion name a version of a tariff (TariffVersion), which stays as it was
	// when the tariff gets a new version later. TariffSource says where the tariff came
	// from (the rule that chose it); a transaction for which no tariff was found has only
	// the source. All three are nil for a transaction whose tariff was not chosen.
	// Firestore does not store them.
	TariffId      *string `firestore:"-"`
	TariffVersion *int    `firestore:"-"`
	TariffSource  *string `firestore:"-"`

	// Analysis is what the manager worked out about the transaction after it ended; nil
	// before that. It is written with TransactionStore.SetTransactionAnalysis. Firestore
	// does not store it.
	Analysis *TransactionAnalysis `firestore:"-"`
}

// MaxAveragePowerW is the limit of TransactionAnalysis.AveragePowerW, which must stay
// below it: PostgreSQL keeps the value with nine digits before the decimal point and one
// after it.
const MaxAveragePowerW = 1e9

// TransactionAnalysis is what the manager worked out about an ended transaction from the
// readings of its energy register: how long it charged and how long it stayed plugged in
// afterwards (idle time).
type TransactionAnalysis struct {
	// IdleStartedAt is when the transaction stopped charging: the end of the last interval
	// in which it drew power. It is the end of the transaction when it charged until
	// then, and nil when there was no reading to tell from.
	IdleStartedAt *time.Time
	// IdleS is the idle time in seconds: from IdleStartedAt to the end of the transaction.
	IdleS int64
	// ChargingS is the rest of the transaction in seconds: its duration less IdleS.
	ChargingS int64
	// AveragePowerW is the average power in W over the intervals between two readings in
	// which the transaction drew more than the minimum charging power: 0 when there was
	// no such interval, nil when there were no two readings. PostgreSQL keeps one decimal.
	AveragePowerW *float64
	// AnalysedAt is when this was worked out. Both times are read back in UTC.
	AnalysedAt time.Time
}

// TransactionDetails is a partial update: only non-nil fields are written.
type TransactionDetails struct {
	EvseId        *int
	ConnectorId   *int
	StartedAt     *time.Time
	StoppedAt     *time.Time
	MeterStartWh  *int64
	MeterStopWh   *int64
	EnergyWh      *int64
	StopReason    *string
	IdTokenGroup  *string
	ReservationId *int
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

// TransactionStore stores the charging transactions.
//
// A transaction is identified by (chargeStationId, transactionId) in every backend:
// transaction ids are only unique per charge station. OCPP 2.0.1 ids are generated by
// the station, so two stations may use the same id.
type TransactionStore interface {
	Transactions(ctx context.Context) ([]*Transaction, error)
	// FindTransaction returns the transaction with its meter values; nil, nil when the
	// charge station has no transaction with the id.
	FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*Transaction, error)
	// FindTransactionSummary is FindTransaction without the meter values of the
	// transaction (MeterValues is nil): for a caller that does not need them, which is
	// spared reading every meter value of a transaction that may run for hours.
	FindTransactionSummary(ctx context.Context, chargeStationId, transactionId string) (*Transaction, error)
	CreateTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []MeterValue, seqNo int, offline bool) error
	UpdateTransaction(ctx context.Context, chargeStationId, transactionId string, meterValue []MeterValue) error
	EndTransaction(ctx context.Context, chargeStationId, transactionId, idToken, tokenType string, meterValue []MeterValue, seqNo int) error
	// UpdateTransactionCost stores the most recent running cost for a transaction as
	// communicated by the CSMS via the CostUpdated message.
	UpdateTransactionCost(ctx context.Context, chargeStationId, transactionId string, totalCost float64) error
	// ListTransactionsForChargeStation retrieves transactions for a specific charge station with filtering and pagination.
	//
	// orgIds narrows the list, and its total, to the transactions that belong to one of
	// these orgs (Transaction.OrgId); without orgs the transactions of every org are
	// listed. A transaction keeps its org when its charge station moves to another org,
	// so a charge station can have transactions of more than one org. A store that does
	// not keep the org of a transaction (Firestore) lists every transaction.
	ListTransactionsForChargeStation(ctx context.Context, chargeStationId, status string, startDate, endDate *time.Time, limit, offset int, orgIds []string) ([]*Transaction, int64, error)
	// UpdateTransactionDetails merges the non-nil fields of details into the stored
	// transaction. Returns an error wrapping ErrNotFound if the transaction does not exist.
	UpdateTransactionDetails(ctx context.Context, chargeStationId, transactionId string, details *TransactionDetails) error
	// NextOcpp16TransactionId returns the next CSMS-assigned OCPP 1.6 transaction id
	// (1..2147483647, wrapping). Firestore returns ErrNotImplemented.
	NextOcpp16TransactionId(ctx context.Context) (int, error)
	// SetTransactionTariff stores the tariff version that the transaction is charged with
	// and its source. tariffId and version are both set, or both nil for a transaction
	// without a tariff, which then has only the source; the source is never empty. The
	// tariff of a transaction is set once: when the transaction has a source already,
	// nothing is written and ErrAlreadyExists is returned. A transaction that does not
	// exist is ErrNotFound. Firestore returns ErrNotImplemented.
	SetTransactionTariff(ctx context.Context, chargeStationId, transactionId string, tariffId *string, version *int, source string) error
	// SetTransactionAnalysis stores what was worked out about an ended transaction
	// (Transaction.Analysis). An analysis that the transaction has already is replaced.
	// A transaction that does not exist is ErrNotFound; an analysis that
	// CheckTransactionAnalysis refuses is not stored. Firestore returns ErrNotImplemented.
	SetTransactionAnalysis(ctx context.Context, chargeStationId, transactionId string, analysis TransactionAnalysis) error
}

// CheckTransactionTariff checks what SetTransactionTariff is given: a source, and a
// tariff id together with a version number (1 to 2147483647), or neither.
func CheckTransactionTariff(tariffId *string, version *int, source string) error {
	switch {
	case source == "":
		return errors.New("a tariff source is required")
	case (tariffId == nil) != (version == nil):
		return errors.New("a tariff id and a tariff version go together")
	case version != nil && (*version < 1 || *version > math.MaxInt32):
		return fmt.Errorf("tariff version %d is not a version number", *version)
	}
	return nil
}

// CheckTransactionAnalysis checks what SetTransactionAnalysis is given: the time of the
// analysis, an idle and a charging time that are not negative, and an average power that
// is a number from 0 and, rounded to one decimal, below MaxAveragePowerW.
func CheckTransactionAnalysis(analysis TransactionAnalysis) error {
	switch {
	case analysis.IdleS < 0:
		return fmt.Errorf("idle time %d s is negative", analysis.IdleS)
	case analysis.ChargingS < 0:
		return fmt.Errorf("charging time %d s is negative", analysis.ChargingS)
	case analysis.AnalysedAt.IsZero():
		return errors.New("the time of the analysis is required")
	}
	if power := analysis.AveragePowerW; power != nil && (math.IsNaN(*power) || *power < 0 || math.Round(*power*10)/10 >= MaxAveragePowerW) {
		return fmt.Errorf("average power %v W is not a power from 0 to below %v W", *power, float64(MaxAveragePowerW))
	}
	return nil
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
