// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestPageRequestEffectiveLimit(t *testing.T) {
	for limit, want := range map[int]int{
		0:   store.DefaultPageLimit,
		-5:  1,
		1:   1,
		120: 120,
		200: 200,
		201: store.MaxPageLimit,
	} {
		assert.Equal(t, want, store.PageRequest{Limit: limit}.EffectiveLimit(), limit)
	}
}

func TestCursor(t *testing.T) {
	afterId, err := store.DecodeCursor("")
	require.NoError(t, err)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", afterId, "the first page")

	const id = "0193b2c5-0000-7000-8000-000000000001"
	cursor := store.EncodeCursor(id)
	assert.NotContains(t, cursor, id, "opaque")
	afterId, err = store.DecodeCursor(cursor)
	require.NoError(t, err)
	assert.Equal(t, id, afterId)

	for _, invalid := range []string{"not a cursor!", base64.RawURLEncoding.EncodeToString([]byte("not-a-uuid"))} {
		_, err := store.DecodeCursor(invalid)
		assert.ErrorIs(t, err, store.ErrInvalidCursor, invalid)
	}
}

func TestKeyCursor(t *testing.T) {
	afterKey, err := store.DecodeKeyCursor("")
	require.NoError(t, err)
	assert.Equal(t, "", afterKey, "the first page starts before every key")

	for _, key := range []string{"k7f2.CP001", "cs001", "ünïcödé/ key"} {
		cursor := store.EncodeKeyCursor(key)
		assert.NotContains(t, cursor, key, "opaque")
		afterKey, err = store.DecodeKeyCursor(cursor)
		require.NoError(t, err)
		assert.Equal(t, key, afterKey)
	}

	for _, invalid := range []string{
		"not a cursor!",
		base64.RawURLEncoding.EncodeToString([]byte{0xff, 0xfe}),  // not UTF-8
		base64.RawURLEncoding.EncodeToString([]byte("cs\x00001")), // NUL, which no text column holds
	} {
		_, err := store.DecodeKeyCursor(invalid)
		assert.ErrorIs(t, err, store.ErrInvalidCursor, invalid)
	}
}
