// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// rollbackTimeout is how long the rollback of a transaction may take when the context
// of the caller can no longer be used for it.
const rollbackTimeout = 5 * time.Second

type txKey struct{}

// withTx returns a context that carries tx: every store call made with it runs in tx.
func withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// txFrom returns the transaction that ctx carries, or nil when it carries none.
func txFrom(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(txKey{}).(pgx.Tx)
	return tx
}

// RunInTx runs fn in one database transaction on the primary. Every store call that fn
// makes with the ctx it is given runs in that transaction, reads included, so fn reads
// what it wrote before anything is committed. The transaction is committed when fn
// returns nil and rolled back when fn returns an error or panics (the panic carries on
// after the rollback). An error of the commit is returned.
//
// A RunInTx inside fn (with the ctx of fn) joins the transaction: it runs its own fn
// and leaves commit and rollback to the outer call, so an error it returns undoes
// nothing unless the outer fn returns an error too.
//
// A statement that fails in the database leaves PostgreSQL refusing every further
// statement of the transaction, so fn should return the error of a store call. When fn
// returns nil all the same, the commit fails with pgx.ErrTxCommitRollback and nothing
// is stored. The store methods that start a transaction of their own (see begin) are
// the exception: one that fails undoes only what it wrote.
//
// The ctx given to fn is for one goroutine at a time (the transaction is one
// connection, and pgx does not allow concurrent use of it), and it is of no use after
// fn has returned.
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if txFrom(ctx) != nil {
		return fn(ctx)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// A deferred call instead of a recover: it also runs when fn panics, and the panic
	// then goes on to the caller with the stack of the place where it started.
	finished := false
	defer func() {
		if !finished {
			rollback(ctx, tx)
		}
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		return err
	}

	// pgx sends no commit for a context that is cancelled: it closes the connection. A
	// rollback ends the transaction the same and keeps the connection.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	// Commit ends the transaction and gives the connection back whether it succeeds or
	// not, so there is nothing left to roll back after it.
	finished = true
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// rollback ends tx without storing anything. The context of the caller may be cancelled
// by now (a request that timed out, a client that went away): pgx would then not send
// the rollback and would close the connection instead. So the rollback runs with a
// context that is not cancelled and has its own deadline, and the connection goes back
// to the pool.
func rollback(ctx context.Context, tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	// A rollback that fails needs no handling: pgx closes the connection then, and
	// PostgreSQL rolls the transaction back when the connection is gone.
	_ = tx.Rollback(ctx)
}

// writeQueries returns the queries for a write: in the transaction that ctx carries,
// otherwise on the primary.
func (s *Store) writeQueries(ctx context.Context) *Queries {
	if tx := txFrom(ctx); tx != nil {
		return New(tx)
	}
	return s.q
}

// readQueries returns the queries for a read: in the transaction that ctx carries, so
// that the read sees what the transaction wrote; otherwise on the read replica when one
// is configured, and on the primary when not.
func (s *Store) readQueries(ctx context.Context) *Queries {
	if tx := txFrom(ctx); tx != nil {
		return New(tx)
	}
	return s.readQ
}

// db returns where a statement that is not a generated query runs: the transaction that
// ctx carries, otherwise the primary pool.
func (s *Store) db(ctx context.Context) DBTX {
	if tx := txFrom(ctx); tx != nil {
		return tx
	}
	return s.pool
}

// begin starts the transaction of a store method that needs several statements to
// happen together. Inside a transaction that ctx carries it is a savepoint: Commit
// releases the savepoint, and what the method wrote is still committed or rolled back
// with the outer transaction; Rollback undoes what the method wrote and leaves the
// outer transaction usable. Without one it is a transaction of its own on the primary.
func (s *Store) begin(ctx context.Context) (pgx.Tx, error) {
	if tx := txFrom(ctx); tx != nil {
		return tx.Begin(ctx)
	}
	return s.pool.Begin(ctx)
}
