// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend keeps no org of its tokens and no asset model: it has no
// inventory.

func (s *Store) ListInventory(context.Context, string, string, bool, store.PageRequest) ([]store.InventoryItem, string, error) {
	return nil, "", fmt.Errorf("ListInventory: %w", store.ErrNotImplemented)
}

func (s *Store) CountInventory(context.Context, string, string, bool) (int, error) {
	return 0, fmt.Errorf("CountInventory: %w", store.ErrNotImplemented)
}
