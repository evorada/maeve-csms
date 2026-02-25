// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// MessagePriority represents the priority of a display message
type MessagePriority string

const (
	MessagePriorityNormalCycle MessagePriority = "NormalCycle"
	MessagePriorityInFront     MessagePriority = "InFront"
	MessagePriorityAlwaysFront MessagePriority = "AlwaysFront"
)

// MessageState represents the charging state when a message should be displayed
type MessageState string

const (
	MessageStateCharging    MessageState = "Charging"
	MessageStateFaulted     MessageState = "Faulted"
	MessageStateIdle        MessageState = "Idle"
	MessageStateUnavailable MessageState = "Unavailable"
)

// MessageFormat represents the content format of a message
type MessageFormat string

const (
	MessageFormatASCII MessageFormat = "ASCII"
	MessageFormatHTML  MessageFormat = "HTML"
	MessageFormatURI   MessageFormat = "URI"
)

// MessageContent represents the content of a display message
type MessageContent struct {
	Content  string        `json:"content"`
	Language *string       `json:"language,omitempty"`
	Format   MessageFormat `json:"format"`
}

// DisplayMessage represents a message to be displayed on a charge station
type DisplayMessage struct {
	ChargeStationId string          `json:"chargeStationId"`
	Id              int             `json:"id"`
	Priority        MessagePriority `json:"priority"`
	State           *MessageState   `json:"state,omitempty"`
	StartDateTime   *time.Time      `json:"startDateTime,omitempty"`
	EndDateTime     *time.Time      `json:"endDateTime,omitempty"`
	TransactionId   *string         `json:"transactionId,omitempty"`
	Message         MessageContent  `json:"message"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// DisplayMessageStore defines the interface for managing display messages
type DisplayMessageStore interface {
	// SetDisplayMessage stores or updates a display message
	SetDisplayMessage(ctx context.Context, message *DisplayMessage) error

	// GetDisplayMessage retrieves a specific display message
	GetDisplayMessage(ctx context.Context, chargeStationId string, messageId int) (*DisplayMessage, error)

	// ListDisplayMessages retrieves all display messages for a charge station
	// Can optionally filter by state and/or priority
	ListDisplayMessages(ctx context.Context, chargeStationId string, state *MessageState, priority *MessagePriority) ([]*DisplayMessage, error)

	// DeleteDisplayMessage removes a display message
	DeleteDisplayMessage(ctx context.Context, chargeStationId string, messageId int) error

	// DeleteAllDisplayMessages removes all display messages for a charge station
	DeleteAllDisplayMessages(ctx context.Context, chargeStationId string) error
}
