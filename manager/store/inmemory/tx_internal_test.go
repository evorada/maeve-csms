// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestRunInTx_RunsFn(t *testing.T) {
	ctx := context.Background()
	s := NewStore(clock.RealClock{})

	calls := 0
	err := s.RunInTx(ctx, func(ctx context.Context) error {
		calls++
		assert.NotNil(t, memTxFrom(ctx), "the context of fn should say that it is inside a transaction")

		// A store method takes the store mutex: this returns only because RunInTx does
		// not hold the mutex while fn runs.
		return s.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
			SecurityProfile: store.UnsecuredTransportWithBasicAuth,
		})
	})
	require.NoError(t, err)

	assert.Equal(t, 1, calls)
	assert.Nil(t, memTxFrom(ctx), "the context of the caller should stay outside the transaction")
	auth, err := s.LookupChargeStationAuth(ctx, "cs001")
	require.NoError(t, err)
	assert.NotNil(t, auth)
}

func TestRunInTx_ReturnsErrorOfFn(t *testing.T) {
	s := NewStore(clock.RealClock{})
	errFn := errors.New("fn failed")

	err := s.RunInTx(context.Background(), func(context.Context) error {
		return errFn
	})

	assert.ErrorIs(t, err, errFn)
}

func TestRunInTx_NestedJoinsOuterTransaction(t *testing.T) {
	s := NewStore(clock.RealClock{})
	errInner := errors.New("inner failed")

	innerCalls := 0
	err := s.RunInTx(context.Background(), func(outerCtx context.Context) error {
		err := s.RunInTx(outerCtx, func(innerCtx context.Context) error {
			innerCalls++
			// joining means that there is no second transaction: the inner fn gets the
			// very context of the outer one
			assert.True(t, innerCtx == outerCtx, "the inner call should run in the transaction of the outer call")
			assert.NotNil(t, memTxFrom(innerCtx))
			return errInner
		})
		assert.ErrorIs(t, err, errInner, "the inner call should return the error of its fn")
		return nil
	})
	require.NoError(t, err)

	assert.Equal(t, 1, innerCalls)
}
