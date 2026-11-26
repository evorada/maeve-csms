// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"
)

// OCPP 1.6 transaction ids are 32-bit integers: after the largest one the allocator
// starts again at 1.
func TestNextOcpp16TransactionId_Wraps(t *testing.T) {
	ctx := context.Background()

	transactionStore := NewStore(clock.RealClock{})
	transactionStore.ocpp16TransactionSeq = math.MaxInt32 - 1

	got, err := transactionStore.NextOcpp16TransactionId(ctx)
	require.NoError(t, err)
	assert.Equal(t, math.MaxInt32, got)

	got, err = transactionStore.NextOcpp16TransactionId(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}
