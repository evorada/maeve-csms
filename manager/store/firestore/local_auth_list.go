// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type localAuthListMeta struct {
	Version int `firestore:"version"`
}

type localAuthListEntry struct {
	IdTag       string  `firestore:"idTag"`
	Status      string  `firestore:"status"`
	ExpiryDate  *string `firestore:"expiryDate"`
	ParentIdTag *string `firestore:"parentIdTag"`
}

func (s *Store) GetLocalListVersion(ctx context.Context, chargeStationId string) (int, error) {
	metaRef := s.client.Doc(fmt.Sprintf("ChargeStation/%s/LocalAuthList/meta", chargeStationId))
	snap, err := metaRef.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return 0, nil
		}
		return 0, fmt.Errorf("get local list version for %s: %w", chargeStationId, err)
	}

	var meta localAuthListMeta
	if err := snap.DataTo(&meta); err != nil {
		return 0, fmt.Errorf("map local auth list meta for %s: %w", chargeStationId, err)
	}
	return meta.Version, nil
}

func (s *Store) UpdateLocalAuthList(ctx context.Context, chargeStationId string, version int, updateType string, entries []*store.LocalAuthListEntry) error {
	collPath := fmt.Sprintf("ChargeStation/%s/LocalAuthList/entries/Items", chargeStationId)

	if updateType == store.LocalAuthListUpdateTypeFull {
		// Delete all existing entries first
		iter := s.client.Collection(collPath).Documents(ctx)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return fmt.Errorf("iterating local auth list for deletion: %w", err)
			}
			if _, err := doc.Ref.Delete(ctx); err != nil {
				return fmt.Errorf("deleting local auth entry: %w", err)
			}
		}
	}

	// Write entries
	if updateType == store.LocalAuthListUpdateTypeDifferential {
		for _, entry := range entries {
			ref := s.client.Doc(fmt.Sprintf("%s/%s", collPath, entry.IdTag))
			if entry.IdTagInfo == nil {
				// Remove
				if _, err := ref.Delete(ctx); err != nil {
					return fmt.Errorf("deleting local auth entry %s: %w", entry.IdTag, err)
				}
			} else {
				fsEntry := &localAuthListEntry{
					IdTag:       entry.IdTag,
					Status:      entry.IdTagInfo.Status,
					ExpiryDate:  entry.IdTagInfo.ExpiryDate,
					ParentIdTag: entry.IdTagInfo.ParentIdTag,
				}
				if _, err := ref.Set(ctx, fsEntry); err != nil {
					return fmt.Errorf("setting local auth entry %s: %w", entry.IdTag, err)
				}
			}
		}
	} else {
		// Full update - write all entries
		for _, entry := range entries {
			ref := s.client.Doc(fmt.Sprintf("%s/%s", collPath, entry.IdTag))
			fsEntry := &localAuthListEntry{
				IdTag:       entry.IdTag,
				Status:      entry.IdTagInfo.Status,
				ExpiryDate:  entry.IdTagInfo.ExpiryDate,
				ParentIdTag: entry.IdTagInfo.ParentIdTag,
			}
			if _, err := ref.Set(ctx, fsEntry); err != nil {
				return fmt.Errorf("setting local auth entry %s: %w", entry.IdTag, err)
			}
		}
	}

	// Update version
	metaRef := s.client.Doc(fmt.Sprintf("ChargeStation/%s/LocalAuthList/meta", chargeStationId))
	_, err := metaRef.Set(ctx, &localAuthListMeta{Version: version})
	if err != nil {
		return fmt.Errorf("setting local auth list version for %s: %w", chargeStationId, err)
	}

	return nil
}

func (s *Store) GetLocalAuthList(ctx context.Context, chargeStationId string) ([]*store.LocalAuthListEntry, error) {
	collPath := fmt.Sprintf("ChargeStation/%s/LocalAuthList/entries/Items", chargeStationId)
	iter := s.client.Collection(collPath).OrderBy("idTag", firestore.Asc).Documents(ctx)

	entries := make([]*store.LocalAuthListEntry, 0)
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterating local auth list for %s: %w", chargeStationId, err)
		}

		var fsEntry localAuthListEntry
		if err := doc.DataTo(&fsEntry); err != nil {
			return nil, fmt.Errorf("mapping local auth entry: %w", err)
		}

		entries = append(entries, &store.LocalAuthListEntry{
			IdTag: fsEntry.IdTag,
			IdTagInfo: &store.IdTagInfo{
				Status:      fsEntry.Status,
				ExpiryDate:  fsEntry.ExpiryDate,
				ParentIdTag: fsEntry.ParentIdTag,
			},
		})
	}

	return entries, nil
}
