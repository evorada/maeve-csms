// SPDX-License-Identifier: Apache-2.0

package commands

import "time"

// ProgressState is the state of what follows a command that its station accepted.
type ProgressState string

const (
	ProgressAwaitingTransaction ProgressState = "awaiting_transaction" // RemoteStart accepted
	ProgressTransactionStarted  ProgressState = "transaction_started"
	ProgressNoTransaction       ProgressState = "no_transaction" // window passed
	ProgressAwaitingStop        ProgressState = "awaiting_stop"  // RemoteStop accepted
	ProgressTransactionStopped  ProgressState = "transaction_stopped"
	ProgressStopNotConfirmed    ProgressState = "stop_not_confirmed"
	ProgressScheduled           ProgressState = "scheduled"       // ChangeAvailability Scheduled
	ProgressAwaitingStatus      ProgressState = "awaiting_status" // ChangeAvailability Accepted
	ProgressApplied             ProgressState = "applied"
	ProgressNotApplied          ProgressState = "not_applied"
)

// Progress is what the station did after it accepted a command, as stored in the
// progress of the command. Each change replaces it whole.
type Progress struct {
	State ProgressState `json:"state"`
	// Deadline is when a state that waits for the station ends without the effect.
	Deadline          *time.Time `json:"deadline,omitempty"`
	TransactionId     string     `json:"transactionId,omitempty"` // stored (UUID) form
	OcppTransactionId *int       `json:"ocppTransactionId,omitempty"`
	ConnectorId       *int       `json:"connectorId,omitempty"`
	ConnectorStatus   string     `json:"connectorStatus,omitempty"`
	StopReason        string     `json:"stopReason,omitempty"`
}
