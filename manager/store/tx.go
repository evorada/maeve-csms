// SPDX-License-Identifier: Apache-2.0

package store

import "context"

// Transactor runs fn in one storage transaction. Every store call made with the ctx
// passed to fn joins that transaction; nested RunInTx calls join the outer one.
// If fn returns an error the transaction is rolled back and the error returned.
// Backends without transactions (Firestore) call fn directly.
//
// The ctx passed to fn must not be used by several goroutines at the same time: the
// transaction behind it is one database connection, which runs one statement at a time.
// It must not be used after fn returns either, because the transaction is over by then.
type Transactor interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}
