// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealth(t *testing.T) {
	ctx := context.Background()
	err := testStore.Health(ctx)
	require.NoError(t, err)
}
