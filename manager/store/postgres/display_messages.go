// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetDisplayMessage(ctx context.Context, message *store.DisplayMessage) error {
	params := CreateOrUpdateDisplayMessageParams{
		ChargeStationID: message.ChargeStationId,
		MessageID:       int32(message.Id),
		Priority:        string(message.Priority),
		Content:         message.Message.Content,
		Format:          string(message.Message.Format),
		CreatedAt:       pgtype.Timestamptz{Time: message.CreatedAt, Valid: true},
		UpdatedAt:       pgtype.Timestamptz{Time: message.UpdatedAt, Valid: true},
	}

	if message.State != nil {
		params.State = pgtype.Text{String: string(*message.State), Valid: true}
	}
	if message.StartDateTime != nil {
		params.StartDateTime = pgtype.Timestamptz{Time: *message.StartDateTime, Valid: true}
	}
	if message.EndDateTime != nil {
		params.EndDateTime = pgtype.Timestamptz{Time: *message.EndDateTime, Valid: true}
	}
	if message.TransactionId != nil {
		params.TransactionID = pgtype.Text{String: *message.TransactionId, Valid: true}
	}
	if message.Message.Language != nil {
		params.Language = pgtype.Text{String: *message.Message.Language, Valid: true}
	}

	err := s.writeQueries().CreateOrUpdateDisplayMessage(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set display message: %w", err)
	}
	return nil
}

func (s *Store) GetDisplayMessage(ctx context.Context, chargeStationId string, messageId int) (*store.DisplayMessage, error) {
	m, err := s.readQueries().GetDisplayMessage(ctx, GetDisplayMessageParams{
		ChargeStationID: chargeStationId,
		MessageID:       int32(messageId),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get display message: %w", err)
	}
	return toStoreDisplayMessage(&m), nil
}

func (s *Store) ListDisplayMessages(ctx context.Context, chargeStationId string, state *store.MessageState, priority *store.MessagePriority) ([]*store.DisplayMessage, error) {
	var rows []DisplayMessage
	var err error

	// Choose the appropriate query based on filters
	if state != nil && priority != nil {
		rows, err = s.readQueries().ListDisplayMessagesByStateAndPriority(ctx, ListDisplayMessagesByStateAndPriorityParams{
			ChargeStationID: chargeStationId,
			State:           pgtype.Text{String: string(*state), Valid: true},
			Priority:        string(*priority),
		})
	} else if state != nil {
		rows, err = s.readQueries().ListDisplayMessagesByState(ctx, ListDisplayMessagesByStateParams{
			ChargeStationID: chargeStationId,
			State:           pgtype.Text{String: string(*state), Valid: true},
		})
	} else if priority != nil {
		rows, err = s.readQueries().ListDisplayMessagesByPriority(ctx, ListDisplayMessagesByPriorityParams{
			ChargeStationID: chargeStationId,
			Priority:        string(*priority),
		})
	} else {
		rows, err = s.readQueries().ListDisplayMessages(ctx, chargeStationId)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list display messages: %w", err)
	}

	result := make([]*store.DisplayMessage, len(rows))
	for i := range rows {
		result[i] = toStoreDisplayMessage(&rows[i])
	}
	return result, nil
}

func (s *Store) DeleteDisplayMessage(ctx context.Context, chargeStationId string, messageId int) error {
	err := s.writeQueries().DeleteDisplayMessage(ctx, DeleteDisplayMessageParams{
		ChargeStationID: chargeStationId,
		MessageID:       int32(messageId),
	})
	if err != nil {
		return fmt.Errorf("failed to delete display message: %w", err)
	}
	return nil
}

func (s *Store) DeleteAllDisplayMessages(ctx context.Context, chargeStationId string) error {
	err := s.writeQueries().DeleteAllDisplayMessages(ctx, chargeStationId)
	if err != nil {
		return fmt.Errorf("failed to delete all display messages: %w", err)
	}
	return nil
}

func toStoreDisplayMessage(m *DisplayMessage) *store.DisplayMessage {
	msg := &store.DisplayMessage{
		ChargeStationId: m.ChargeStationID,
		Id:              int(m.MessageID),
		Priority:        store.MessagePriority(m.Priority),
		Message: store.MessageContent{
			Content: m.Content,
			Format:  store.MessageFormat(m.Format),
		},
		CreatedAt: m.CreatedAt.Time,
		UpdatedAt: m.UpdatedAt.Time,
	}

	if m.State.Valid {
		state := store.MessageState(m.State.String)
		msg.State = &state
	}
	if m.StartDateTime.Valid {
		msg.StartDateTime = &m.StartDateTime.Time
	}
	if m.EndDateTime.Valid {
		msg.EndDateTime = &m.EndDateTime.Time
	}
	if m.TransactionID.Valid {
		msg.TransactionId = &m.TransactionID.String
	}
	if m.Language.Valid {
		msg.Message.Language = &m.Language.String
	}

	return msg
}
