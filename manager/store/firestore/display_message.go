// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func displayMessageKey(chargeStationId string, messageId int) string {
	return fmt.Sprintf("DisplayMessage/%s/%d", chargeStationId, messageId)
}

func (s *Store) SetDisplayMessage(ctx context.Context, message *store.DisplayMessage) error {
	ref := s.client.Doc(displayMessageKey(message.ChargeStationId, message.Id))
	_, err := ref.Set(ctx, message)
	if err != nil {
		return fmt.Errorf("set display message %s/%d: %w", message.ChargeStationId, message.Id, err)
	}
	return nil
}

func (s *Store) GetDisplayMessage(ctx context.Context, chargeStationId string, messageId int) (*store.DisplayMessage, error) {
	ref := s.client.Doc(displayMessageKey(chargeStationId, messageId))
	snap, err := ref.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get display message %s/%d: %w", chargeStationId, messageId, err)
	}
	var m store.DisplayMessage
	if err := snap.DataTo(&m); err != nil {
		return nil, fmt.Errorf("map display message %s/%d: %w", chargeStationId, messageId, err)
	}
	return &m, nil
}

func (s *Store) ListDisplayMessages(ctx context.Context, chargeStationId string, state *store.MessageState, priority *store.MessagePriority) ([]*store.DisplayMessage, error) {
	query := s.client.Collection("DisplayMessage").Where("chargeStationId", "==", chargeStationId)

	if state != nil {
		query = query.Where("state", "==", string(*state))
	}
	if priority != nil {
		query = query.Where("priority", "==", string(*priority))
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var result []*store.DisplayMessage
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterate display messages: %w", err)
		}
		var m store.DisplayMessage
		if err := snap.DataTo(&m); err != nil {
			return nil, fmt.Errorf("map display message: %w", err)
		}
		result = append(result, &m)
	}

	if result == nil {
		result = make([]*store.DisplayMessage, 0)
	}
	return result, nil
}

func (s *Store) DeleteDisplayMessage(ctx context.Context, chargeStationId string, messageId int) error {
	ref := s.client.Doc(displayMessageKey(chargeStationId, messageId))
	_, err := ref.Delete(ctx)
	if err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("delete display message %s/%d: %w", chargeStationId, messageId, err)
	}
	return nil
}

func (s *Store) DeleteAllDisplayMessages(ctx context.Context, chargeStationId string) error {
	iter := s.client.Collection("DisplayMessage").
		Where("chargeStationId", "==", chargeStationId).
		Documents(ctx)
	defer iter.Stop()

	batch := s.client.Batch()
	count := 0
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("iterate display messages to delete: %w", err)
		}
		batch.Delete(snap.Ref)
		count++
		// Firestore batch limit is 500 operations
		if count >= 500 {
			if _, err := batch.Commit(ctx); err != nil {
				return fmt.Errorf("commit batch delete: %w", err)
			}
			batch = s.client.Batch()
			count = 0
		}
	}
	if count > 0 {
		if _, err := batch.Commit(ctx); err != nil {
			return fmt.Errorf("commit final batch delete: %w", err)
		}
	}
	return nil
}
