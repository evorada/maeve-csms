// SPDX-License-Identifier: Apache-2.0

package inmemory

import "context"

type memTxKey struct{}

// memTx is one in-memory transaction. It holds what store methods set aside until the
// transaction ends; a method finds it with memTxFrom.
type memTx struct{}

// withMemTx returns a context that carries tx: store calls made with it are inside tx.
func withMemTx(ctx context.Context, tx *memTx) context.Context {
	return context.WithValue(ctx, memTxKey{}, tx)
}

// memTxFrom returns the in-memory transaction that ctx carries, or nil when it carries
// none.
func memTxFrom(ctx context.Context) *memTx {
	tx, _ := ctx.Value(memTxKey{}).(*memTx)
	return tx
}

// RunInTx runs fn and returns its error. The ctx given to fn says that the store calls
// made with it are inside a transaction; a RunInTx inside fn joins that transaction.
//
// It is not a database transaction. The store mutex is not held while fn runs (every
// store method takes it), so other callers see each write of fn as soon as it is made,
// and the writes are not undone when fn returns an error or panics. The in-memory store
// is for tests and development, where that is enough.
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if memTxFrom(ctx) != nil {
		return fn(ctx)
	}
	return fn(withMemTx(ctx, &memTx{}))
}
