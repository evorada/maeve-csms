// SPDX-License-Identifier: Apache-2.0

package tariffs_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tariffs"
)

// refusingTransactions is a transaction store that refuses every tariff with err.
type refusingTransactions struct {
	store.TransactionStore
	err error
}

func (s refusingTransactions) SetTransactionTariff(context.Context, string, string, *string, *int, string) error {
	return s.err
}

func TestPin(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for _, transactionId := range []string{"tx-1", "tx-2"} {
		require.NoError(t, engine.CreateTransaction(ctx, selectStation, transactionId, "RFID0001", "ISO14443", nil, 0, false))
	}
	tariffId := "0193b2c5-0000-7000-8000-000000000050"
	find := func(transactionId string) *store.Transaction {
		transaction, err := engine.FindTransaction(ctx, selectStation, transactionId)
		require.NoError(t, err)
		require.NotNil(t, transaction)
		return transaction
	}

	t.Run("a tariff version", func(t *testing.T) {
		pinned, err := tariffs.Pin(ctx, engine, selectStation, "tx-1", tariffs.Choice{TariffId: tariffId, Version: 3, Source: tariffs.SourceTokenGroup})
		require.NoError(t, err)
		assert.True(t, pinned)
		transaction := find("tx-1")
		version, source := 3, "token_group"
		assert.Equal(t, &tariffId, transaction.TariffId)
		assert.Equal(t, &version, transaction.TariffVersion)
		assert.Equal(t, &source, transaction.TariffSource)

		// the tariff of a transaction is chosen once
		pinned, err = tariffs.Pin(ctx, engine, selectStation, "tx-1", tariffs.Choice{TariffId: tariffId, Version: 4, Source: tariffs.SourceConnector})
		require.NoError(t, err)
		assert.False(t, pinned)
		assert.Equal(t, transaction, find("tx-1"))
	})

	t.Run("no tariff", func(t *testing.T) {
		pinned, err := tariffs.Pin(ctx, engine, selectStation, "tx-2", tariffs.Choice{Source: tariffs.SourceFallback})
		require.NoError(t, err)
		assert.True(t, pinned)
		transaction := find("tx-2")
		source := "fallback"
		assert.Nil(t, transaction.TariffId)
		assert.Nil(t, transaction.TariffVersion)
		assert.Equal(t, &source, transaction.TariffSource)
	})

	t.Run("what is not stored", func(t *testing.T) {
		choice := tariffs.Choice{TariffId: tariffId, Version: 1, Source: tariffs.SourceConnector}

		pinned, err := tariffs.Pin(ctx, engine, selectStation, "tx-unknown", choice)
		assert.ErrorIs(t, err, store.ErrNotFound)
		assert.False(t, pinned)

		pinned, err = tariffs.Pin(ctx, engine, selectStation, "tx-2", tariffs.Choice{})
		assert.Error(t, err, "a choice without a source is no choice")
		assert.False(t, pinned)

		// a backend that keeps no tariff with a transaction: not an error
		backend := refusingTransactions{err: fmt.Errorf("SetTransactionTariff: %w", store.ErrNotImplemented)}
		pinned, err = tariffs.Pin(ctx, backend, selectStation, "tx-2", choice)
		require.NoError(t, err)
		assert.False(t, pinned)

		backend = refusingTransactions{err: errStoreDown}
		pinned, err = tariffs.Pin(ctx, backend, selectStation, "tx-2", choice)
		assert.ErrorIs(t, err, errStoreDown)
		assert.False(t, pinned)
	})
}

// stuckAssets is an asset store whose lookups do not answer until their context ends.
type stuckAssets struct {
	store.AssetStore
}

func (stuckAssets) LookupAssetEvseByOcpp(ctx context.Context, _ string, _ int) (*store.AssetEvse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// The start of a transaction does not wait for a selection that does not come: after
// SelectTimeout it has an error, and the context of the caller is as it was.
func TestSelectForStartDoesNotWait(t *testing.T) {
	f := newSelectFixture(t)
	f.selector.Assets = stuckAssets{AssetStore: f.engine}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	begin := time.Now()
	choice, err := f.selector.SelectForStart(ctx, in16(1, "RFID0001"))
	waited := time.Since(begin)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, tariffs.Choice{}, choice)
	assert.GreaterOrEqual(t, waited, tariffs.SelectTimeout)
	assert.Less(t, waited, tariffs.SelectTimeout+10*time.Second)
	assert.NoError(t, ctx.Err(), "the caller goes on")

	// a selection that answers in time is the selection
	f.selector.Assets = f.engine
	choice, err = f.selector.SelectForStart(ctx, in16(1, "RFID0001"))
	require.NoError(t, err)
	assert.Equal(t, tariffs.Choice{TariffId: f.regular.Id, Version: 1, Source: tariffs.SourceConnector}, choice)
}
