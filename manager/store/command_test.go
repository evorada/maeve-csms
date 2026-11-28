// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestCommandStatusIsTerminal(t *testing.T) {
	tests := map[store.CommandStatus]bool{
		store.CommandStatusQueued:    false,
		store.CommandStatusSent:      false,
		store.CommandStatusAccepted:  true,
		store.CommandStatusRejected:  true,
		store.CommandStatusFailed:    true,
		store.CommandStatusTimeout:   true,
		store.CommandStatusCancelled: true,
	}
	for status, terminal := range tests {
		assert.Equal(t, terminal, status.IsTerminal(), status)
	}
}

func TestCommandFilterLimitAndOffset(t *testing.T) {
	limits := map[int]int{-1: 50, 0: 50, 1: 1, 50: 50, 500: 500, 501: 500}
	for limit, want := range limits {
		assert.Equal(t, want, store.CommandFilter{Limit: limit}.EffectiveLimit(), "limit %d", limit)
	}
	offsets := map[int]int{-1: 0, 0: 0, 7: 7}
	for offset, want := range offsets {
		assert.Equal(t, want, store.CommandFilter{Offset: offset}.EffectiveOffset(), "offset %d", offset)
	}
}
