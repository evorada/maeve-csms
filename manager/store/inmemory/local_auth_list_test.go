// SPDX-License-Identifier: Apache-2.0

package inmemory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestGetLocalListVersion_Default(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})

	version, err := s.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 0, version)
}

func TestUpdateLocalAuthList_FullUpdate(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	entries := []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
		{IdTag: "tag2", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusBlocked}},
	}

	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, entries)
	require.NoError(t, err)

	version, err := s.GetLocalListVersion(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, "tag1", list[0].IdTag)
	assert.Equal(t, store.IdTagStatusAccepted, list[0].IdTagInfo.Status)
	assert.Equal(t, "tag2", list[1].IdTag)
	assert.Equal(t, store.IdTagStatusBlocked, list[1].IdTagInfo.Status)
}

func TestUpdateLocalAuthList_FullReplace(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	// Set initial list
	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
		{IdTag: "tag2", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	// Full replace with different list
	err = s.UpdateLocalAuthList(ctx, "cs001", 2, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag3", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusExpired}},
	})
	require.NoError(t, err)

	version, err := s.GetLocalListVersion(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "tag3", list[0].IdTag)
}

func TestUpdateLocalAuthList_DifferentialAdd(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	// Set initial list
	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	// Differential: add a new entry
	err = s.UpdateLocalAuthList(ctx, "cs001", 2, store.LocalAuthListUpdateTypeDifferential, []*store.LocalAuthListEntry{
		{IdTag: "tag2", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestUpdateLocalAuthList_DifferentialRemove(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	// Set initial list
	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
		{IdTag: "tag2", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	// Differential: remove tag1 (nil IdTagInfo)
	err = s.UpdateLocalAuthList(ctx, "cs001", 2, store.LocalAuthListUpdateTypeDifferential, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: nil},
	})
	require.NoError(t, err)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "tag2", list[0].IdTag)
}

func TestUpdateLocalAuthList_DifferentialUpdate(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	// Set initial list
	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	// Differential: update tag1 status
	err = s.UpdateLocalAuthList(ctx, "cs001", 2, store.LocalAuthListUpdateTypeDifferential, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusBlocked}},
	})
	require.NoError(t, err)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, store.IdTagStatusBlocked, list[0].IdTagInfo.Status)
}

func TestUpdateLocalAuthList_WithOptionalFields(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	expiry := "2026-12-31T23:59:59Z"
	parent := "parentTag"

	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{
			Status:      store.IdTagStatusAccepted,
			ExpiryDate:  &expiry,
			ParentIdTag: &parent,
		}},
	})
	require.NoError(t, err)

	list, err := s.GetLocalAuthList(ctx, "cs001")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, &expiry, list[0].IdTagInfo.ExpiryDate)
	assert.Equal(t, &parent, list[0].IdTagInfo.ParentIdTag)
}

func TestGetLocalAuthList_EmptyList(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})

	list, err := s.GetLocalAuthList(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestUpdateLocalAuthList_IsolatedPerStation(t *testing.T) {
	s := inmemory.NewStore(clock.RealClock{})
	ctx := context.Background()

	err := s.UpdateLocalAuthList(ctx, "cs001", 1, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tag1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
	})
	require.NoError(t, err)

	err = s.UpdateLocalAuthList(ctx, "cs002", 5, store.LocalAuthListUpdateTypeFull, []*store.LocalAuthListEntry{
		{IdTag: "tagA", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusBlocked}},
	})
	require.NoError(t, err)

	v1, _ := s.GetLocalListVersion(ctx, "cs001")
	v2, _ := s.GetLocalListVersion(ctx, "cs002")
	assert.Equal(t, 1, v1)
	assert.Equal(t, 5, v2)

	list1, _ := s.GetLocalAuthList(ctx, "cs001")
	list2, _ := s.GetLocalAuthList(ctx, "cs002")
	assert.Len(t, list1, 1)
	assert.Len(t, list2, 1)
	assert.Equal(t, "tag1", list1[0].IdTag)
	assert.Equal(t, "tagA", list2[0].IdTag)
}
