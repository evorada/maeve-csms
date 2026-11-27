// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
)

// errStopTx is what a test returns from fn to make RunInTx roll back.
var errStopTx = errors.New("stop the transaction")

// openTransactions selects the connections, other than the one that asks, that are
// inside a transaction and wait for their next statement.
const openTransactions = `FROM pg_stat_activity
	WHERE datname = current_database() AND pid <> pg_backend_pid() AND state LIKE 'idle in transaction%'`

// requireNoOpenTransaction fails the test when a connection is still inside a
// transaction. A write that was never committed cannot be seen by a reader, whether its
// transaction was rolled back or was left open; only this tells the two apart.
func requireNoOpenTransaction(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	open := -1
	ended := assert.Eventually(t, func() bool {
		if err := testPool.QueryRow(ctx, `SELECT count(*) `+openTransactions).Scan(&open); err != nil {
			return false
		}
		return open == 0
	}, 5*time.Second, 20*time.Millisecond, "a transaction was left open")
	if !ended {
		// an open transaction holds its locks, and the clean-up of the test would wait for them
		_, err := testPool.Exec(ctx, `SELECT pg_terminate_backend(pid) `+openTransactions)
		require.NoError(t, err)
		t.Fatalf("%d connection(s) still inside a transaction", open)
	}
}

// committedToken reads a token outside every transaction: it returns what is committed.
func committedToken(t *testing.T, uid string) *store.Token {
	t.Helper()
	token, err := testStore.LookupToken(context.Background(), uid)
	require.NoError(t, err)
	return token
}

func txTestTenant(orgId, slug, orgKey string, hostnames ...string) *store.Tenant {
	root := "0193b2c4-0000-7000-8000-00000000000a" // never provisioned
	return &store.Tenant{
		OrgId:     orgId,
		ParentId:  &root,
		Path:      store.OrgPathLabel(root) + "." + store.OrgPathLabel(orgId),
		Slug:      slug,
		OrgKey:    orgKey,
		Hostnames: hostnames,
	}
}

func TestRunInTx_CommitsWhenFnSucceeds(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	calls := 0
	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		calls++
		return testStore.SetToken(ctx, orgTestToken("TXCOMMIT", testOrgA))
	})
	require.NoError(t, err)

	assert.Equal(t, 1, calls)
	requireNoOpenTransaction(t)
	assert.NotNil(t, committedToken(t, "TXCOMMIT"))
}

func TestRunInTx_RollsBackWhenFnFails(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		if err := testStore.SetToken(ctx, orgTestToken("TXROLLBACK", testOrgA)); err != nil {
			return err
		}
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)

	requireNoOpenTransaction(t)
	assert.Nil(t, committedToken(t, "TXROLLBACK"))
}

func TestRunInTx_NestedJoinsOuterTransaction(t *testing.T) {
	t.Run("inner writes roll back with the outer transaction", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXOUTER", testOrgA)))

			err := testStore.RunInTx(ctx, func(ctx context.Context) error {
				// the inner call is in the transaction of the outer one, so it reads its write
				outer, err := testStore.LookupToken(ctx, "TXOUTER")
				require.NoError(t, err)
				assert.NotNil(t, outer, "the inner call should read what the outer call wrote")
				return testStore.SetToken(ctx, orgTestToken("TXINNER", testOrgA))
			})
			require.NoError(t, err)

			// the inner call has returned without an error, and has committed nothing
			assert.Nil(t, committedToken(t, "TXINNER"))
			return errStopTx
		})
		require.ErrorIs(t, err, errStopTx)

		requireNoOpenTransaction(t)
		assert.Nil(t, committedToken(t, "TXOUTER"))
		assert.Nil(t, committedToken(t, "TXINNER"))
	})

	t.Run("an inner error that the outer call does not return undoes nothing", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXOUTER", testOrgA)))

			err := testStore.RunInTx(ctx, func(ctx context.Context) error {
				require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXINNER", testOrgA)))
				return errStopTx
			})
			require.ErrorIs(t, err, errStopTx)

			// the inner call has rolled nothing back: the transaction still has both writes
			inner, err := testStore.LookupToken(ctx, "TXINNER")
			require.NoError(t, err)
			assert.NotNil(t, inner)
			return nil
		})
		require.NoError(t, err)

		requireNoOpenTransaction(t)
		assert.NotNil(t, committedToken(t, "TXOUTER"))
		assert.NotNil(t, committedToken(t, "TXINNER"))
	})
}

func TestRunInTx_ReadsSeeUncommittedWritesOfTheTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// A store with a read replica (here the same database through a second pool): a
	// read outside a transaction goes to the replica pool, a read inside must not.
	replicated, err := postgres.NewStore(ctx, connString, connString)
	require.NoError(t, err)
	defer replicated.Close()

	err = replicated.RunInTx(ctx, func(txCtx context.Context) error {
		require.NoError(t, replicated.SetToken(txCtx, orgTestToken("TXREAD", testOrgA)))

		inside, err := replicated.LookupToken(txCtx, "TXREAD")
		require.NoError(t, err)
		assert.NotNil(t, inside, "a read inside the transaction should see its write")

		listed, err := replicated.ListTokens(txCtx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, listed, 1, "a list inside the transaction should see its write")

		outside, err := replicated.LookupToken(ctx, "TXREAD")
		require.NoError(t, err)
		assert.Nil(t, outside, "a read outside the transaction should not see a write that is not committed")
		return nil
	})
	require.NoError(t, err)

	outside, err := replicated.LookupToken(ctx, "TXREAD")
	require.NoError(t, err)
	assert.NotNil(t, outside, "the write should be committed")
}

// UpdateLocalAuthList needs its statements to happen together and starts a transaction
// for them. Inside RunInTx that is a savepoint: its writes belong to the outer transaction.
func TestRunInTx_UpdateLocalAuthListJoins(t *testing.T) {
	entries := []*store.LocalAuthListEntry{
		{IdTag: "TAG1", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusAccepted}},
		{IdTag: "TAG2", IdTagInfo: &store.IdTagInfo{Status: store.IdTagStatusBlocked}},
	}

	update := func(t *testing.T, result error) error {
		return testStore.RunInTx(context.Background(), func(ctx context.Context) error {
			if err := testStore.UpdateLocalAuthList(ctx, "cs001", 3, store.LocalAuthListUpdateTypeFull, entries); err != nil {
				return err
			}

			// the method is done, and only this transaction has its writes
			version, err := testStore.GetLocalListVersion(ctx, "cs001")
			require.NoError(t, err)
			assert.Equal(t, 3, version)
			list, err := testStore.GetLocalAuthList(ctx, "cs001")
			require.NoError(t, err)
			assert.Len(t, list, 2)

			version, err = testStore.GetLocalListVersion(context.Background(), "cs001")
			require.NoError(t, err)
			assert.Equal(t, 0, version, "the method should not commit on its own")
			return result
		})
	}

	committed := func(t *testing.T) (int, []*store.LocalAuthListEntry) {
		version, err := testStore.GetLocalListVersion(context.Background(), "cs001")
		require.NoError(t, err)
		list, err := testStore.GetLocalAuthList(context.Background(), "cs001")
		require.NoError(t, err)
		return version, list
	}

	t.Run("rolls back with the transaction", func(t *testing.T) {
		defer truncateAll(t)

		require.ErrorIs(t, update(t, errStopTx), errStopTx)

		requireNoOpenTransaction(t)
		version, list := committed(t)
		assert.Equal(t, 0, version)
		assert.Empty(t, list)
	})

	t.Run("commits with the transaction", func(t *testing.T) {
		defer truncateAll(t)

		require.NoError(t, update(t, nil))

		requireNoOpenTransaction(t)
		version, list := committed(t)
		assert.Equal(t, 3, version)
		assert.Len(t, list, 2)
	})
}

func TestRunInTx_PutTenantJoins(t *testing.T) {
	t.Run("rolls back with the transaction", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()

		err := testStore.RunInTx(ctx, func(txCtx context.Context) error {
			if err := testStore.PutTenant(txCtx, txTestTenant(testOrgA, "tx-a", "txa001", "a.tx.example")); err != nil {
				return err
			}

			inside, err := testStore.GetTenantByHost(txCtx, "a.tx.example")
			require.NoError(t, err)
			assert.NotNil(t, inside, "the transaction should see the tenant and its hostname")

			outside, err := testStore.GetTenant(ctx, testOrgA)
			require.NoError(t, err)
			assert.Nil(t, outside, "the put should not commit on its own")
			return errStopTx
		})
		require.ErrorIs(t, err, errStopTx)

		requireNoOpenTransaction(t)
		tenant, err := testStore.GetTenant(ctx, testOrgA)
		require.NoError(t, err)
		assert.Nil(t, tenant)
		tenant, err = testStore.GetTenantByHost(ctx, "a.tx.example")
		require.NoError(t, err)
		assert.Nil(t, tenant)
	})

	t.Run("a put that fails undoes only itself", func(t *testing.T) {
		defer truncateAll(t)
		ctx := context.Background()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.PutTenant(ctx, txTestTenant(testOrgA, "tx-a", "txa001", "shared.tx.example")))

			// The hostname is taken. The put has inserted the tenant row by then; the
			// rollback to its savepoint removes the row and keeps the transaction usable.
			err := testStore.PutTenant(ctx, txTestTenant(testOrgB, "tx-b", "txb001", "shared.tx.example"))
			require.ErrorIs(t, err, store.ErrTenantConflict)

			return testStore.SetToken(ctx, orgTestToken("TXAFTERPUT", testOrgA))
		})
		require.NoError(t, err)

		requireNoOpenTransaction(t)
		tenant, err := testStore.GetTenant(ctx, testOrgA)
		require.NoError(t, err)
		assert.NotNil(t, tenant)
		tenant, err = testStore.GetTenant(ctx, testOrgB)
		require.NoError(t, err)
		assert.Nil(t, tenant)
		assert.NotNil(t, committedToken(t, "TXAFTERPUT"))
	})
}

func TestRunInTx_AssignLegacyOrgJoins(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXLEGACY", store.LegacyOrgId)))

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		moved, err := testStore.AssignLegacyOrg(ctx, testOrgA)
		require.NoError(t, err)
		assert.EqualValues(t, 1, moved)

		token, err := testStore.LookupTokenForOrg(ctx, testOrgA, "TXLEGACY")
		require.NoError(t, err)
		assert.NotNil(t, token, "the transaction should see the token in its new org")
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)

	requireNoOpenTransaction(t)
	token, err := testStore.LookupTokenForOrg(ctx, store.LegacyOrgId, "TXLEGACY")
	require.NoError(t, err)
	assert.NotNil(t, token, "the token should still belong to the legacy org")
	token, err = testStore.LookupTokenForOrg(ctx, testOrgA, "TXLEGACY")
	require.NoError(t, err)
	assert.Nil(t, token)
}

// UpdateTransactionCost runs a statement that is not a generated query.
func TestRunInTx_UpdateTransactionCostJoins(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	require.NoError(t, testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	}))
	require.NoError(t, testStore.CreateTransaction(ctx, "cs001", "tx001", "TOKEN001", "RFID", nil, 1, false))

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		if err := testStore.UpdateTransactionCost(ctx, "cs001", "tx001", 15.50); err != nil {
			return err
		}

		inside, err := testStore.FindTransaction(ctx, "cs001", "tx001")
		require.NoError(t, err)
		require.NotNil(t, inside)
		require.NotNil(t, inside.LastCost, "the transaction should see the cost")
		assert.InDelta(t, 15.50, *inside.LastCost, 0.01)
		return errStopTx
	})
	require.ErrorIs(t, err, errStopTx)

	requireNoOpenTransaction(t)
	transaction, err := testStore.FindTransaction(ctx, "cs001", "tx001")
	require.NoError(t, err)
	require.NotNil(t, transaction)
	assert.Nil(t, transaction.LastCost)
}

func TestRunInTx_PanicRollsBackAndCarriesOn(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	require.PanicsWithValue(t, "panic inside the transaction", func() {
		_ = testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXPANIC", testOrgA)))
			panic("panic inside the transaction")
		})
	})

	requireNoOpenTransaction(t)
	assert.Nil(t, committedToken(t, "TXPANIC"))
}

func TestRunInTx_CancelledContextRollsBack(t *testing.T) {
	t.Run("fn returns the error of the context", func(t *testing.T) {
		defer truncateAll(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXCANCEL", testOrgA)))
			cancel()
			return ctx.Err()
		})
		require.ErrorIs(t, err, context.Canceled)

		requireNoOpenTransaction(t)
		assert.Nil(t, committedToken(t, "TXCANCEL"))
	})

	t.Run("fn returns nil", func(t *testing.T) {
		defer truncateAll(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err := testStore.RunInTx(ctx, func(ctx context.Context) error {
			require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXCANCEL", testOrgA)))
			cancel()
			return nil
		})
		require.ErrorIs(t, err, context.Canceled, "a transaction whose context is cancelled should not report a commit")

		requireNoOpenTransaction(t)
		assert.Nil(t, committedToken(t, "TXCANCEL"))
	})
}

// A statement that fails makes PostgreSQL refuse the rest of the transaction. When fn
// does not return that error, the commit must not look like a success.
func TestRunInTx_ReturnsCommitError(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.RunInTx(ctx, func(ctx context.Context) error {
		require.NoError(t, testStore.SetToken(ctx, orgTestToken("TXABORTED", testOrgA)))

		tooLong := orgTestToken("TXABORTED2", testOrgA)
		tooLong.CountryCode = "GBR" // the column holds two characters
		require.Error(t, testStore.SetToken(ctx, tooLong))
		return nil
	})
	require.ErrorIs(t, err, pgx.ErrTxCommitRollback)

	requireNoOpenTransaction(t)
	assert.Nil(t, committedToken(t, "TXABORTED"))
}
