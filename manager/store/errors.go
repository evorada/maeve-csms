// SPDX-License-Identifier: Apache-2.0

package store

import "errors"

// Sentinel errors returned by the storage backends. Backends wrap them with context,
// for example fmt.Errorf("transaction %s/%s: %w", cs, tx, ErrNotFound); callers test
// with errors.Is.
var (
	// ErrNotImplemented is returned by a backend that does not support a method
	// (Firestore for the stores it does not have, such as the tenant store).
	ErrNotImplemented = errors.New("not implemented by this storage backend")
	// ErrNotFound is returned when an update targets a record that does not exist.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists is returned when a create would violate a uniqueness constraint.
	ErrAlreadyExists = errors.New("already exists")
	// ErrVersionConflict is returned when an optimistic update names a version that is not
	// the stored version of the record any more.
	ErrVersionConflict = errors.New("version conflict")
	// ErrInUse is returned when a delete is refused because other records refer to the
	// record.
	ErrInUse = errors.New("in use")
)
