// SPDX-License-Identifier: Apache-2.0

package firestore

import "context"

// RunInTx calls fn with ctx and returns its error. The Firestore backend has no
// transaction that spans store calls: every store call that fn makes is stored on its
// own, and nothing is undone when fn returns an error.
func (s *Store) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
