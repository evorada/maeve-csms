// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The Firestore backend has no token groups.

func (s *Store) CreateTokenGroup(context.Context, *store.TokenGroup) error {
	return fmt.Errorf("CreateTokenGroup: %w", store.ErrNotImplemented)
}

func (s *Store) UpdateTokenGroup(context.Context, *store.TokenGroup) error {
	return fmt.Errorf("UpdateTokenGroup: %w", store.ErrNotImplemented)
}

func (s *Store) LookupTokenGroup(context.Context, string) (*store.TokenGroup, error) {
	return nil, fmt.Errorf("LookupTokenGroup: %w", store.ErrNotImplemented)
}

func (s *Store) ListTokenGroups(context.Context, []string, store.PageRequest) ([]*store.TokenGroup, string, error) {
	return nil, "", fmt.Errorf("ListTokenGroups: %w", store.ErrNotImplemented)
}

func (s *Store) DeleteTokenGroup(context.Context, string) error {
	return fmt.Errorf("DeleteTokenGroup: %w", store.ErrNotImplemented)
}

func (s *Store) SetTokenGroup(context.Context, string, string, *string) error {
	return fmt.Errorf("SetTokenGroup: %w", store.ErrNotImplemented)
}

func (s *Store) ListTokenGroupMembers(context.Context, string, store.PageRequest) ([]*store.Token, string, error) {
	return nil, "", fmt.Errorf("ListTokenGroupMembers: %w", store.ErrNotImplemented)
}

func (s *Store) FindTokenGroupForUid(context.Context, string, string) (*store.TokenGroup, error) {
	return nil, fmt.Errorf("FindTokenGroupForUid: %w", store.ErrNotImplemented)
}
