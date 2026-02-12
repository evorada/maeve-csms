// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) GetLocalListVersion(ctx context.Context, chargeStationId string) (int, error) {
	version, err := s.q.GetLocalListVersion(ctx, chargeStationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get local list version for %s: %w", chargeStationId, err)
	}
	return int(version), nil
}

func (s *Store) UpdateLocalAuthList(ctx context.Context, chargeStationId string, version int, updateType string, entries []*store.LocalAuthListEntry) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := s.q.WithTx(tx)

	if updateType == store.LocalAuthListUpdateTypeFull {
		if err := qtx.DeleteAllLocalAuthListEntries(ctx, chargeStationId); err != nil {
			return fmt.Errorf("delete all entries for %s: %w", chargeStationId, err)
		}
		for _, entry := range entries {
			if entry.IdTagInfo == nil {
				continue
			}
			if err := qtx.UpsertLocalAuthListEntry(ctx, UpsertLocalAuthListEntryParams{
				ChargeStationID: chargeStationId,
				IDTag:           entry.IdTag,
				Status:          entry.IdTagInfo.Status,
				ExpiryDate:      textFromString(entry.IdTagInfo.ExpiryDate),
				ParentIDTag:     textFromString(entry.IdTagInfo.ParentIdTag),
			}); err != nil {
				return fmt.Errorf("upsert entry %s: %w", entry.IdTag, err)
			}
		}
	} else {
		// Differential
		for _, entry := range entries {
			if entry.IdTagInfo == nil {
				if err := qtx.DeleteLocalAuthListEntry(ctx, DeleteLocalAuthListEntryParams{
					ChargeStationID: chargeStationId,
					IDTag:           entry.IdTag,
				}); err != nil {
					return fmt.Errorf("delete entry %s: %w", entry.IdTag, err)
				}
			} else {
				if err := qtx.UpsertLocalAuthListEntry(ctx, UpsertLocalAuthListEntryParams{
					ChargeStationID: chargeStationId,
					IDTag:           entry.IdTag,
					Status:          entry.IdTagInfo.Status,
					ExpiryDate:      textFromString(entry.IdTagInfo.ExpiryDate),
					ParentIDTag:     textFromString(entry.IdTagInfo.ParentIdTag),
				}); err != nil {
					return fmt.Errorf("upsert entry %s: %w", entry.IdTag, err)
				}
			}
		}
	}

	if err := qtx.UpsertLocalListVersion(ctx, UpsertLocalListVersionParams{
		ChargeStationID: chargeStationId,
		Version:         int32(version),
	}); err != nil {
		return fmt.Errorf("upsert version for %s: %w", chargeStationId, err)
	}

	return tx.Commit(ctx)
}

func (s *Store) GetLocalAuthList(ctx context.Context, chargeStationId string) ([]*store.LocalAuthListEntry, error) {
	rows, err := s.q.GetLocalAuthListEntries(ctx, chargeStationId)
	if err != nil {
		return nil, fmt.Errorf("get local auth list for %s: %w", chargeStationId, err)
	}

	entries := make([]*store.LocalAuthListEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, &store.LocalAuthListEntry{
			IdTag: row.IDTag,
			IdTagInfo: &store.IdTagInfo{
				Status:      row.Status,
				ExpiryDate:  stringFromText(row.ExpiryDate),
				ParentIdTag: stringFromText(row.ParentIDTag),
			},
		})
	}
	return entries, nil
}
