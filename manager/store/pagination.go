// SPDX-License-Identifier: Apache-2.0

package store

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	// DefaultPageLimit is the page size of a PageRequest without a limit.
	DefaultPageLimit = 50
	// MaxPageLimit is the largest page that a PageRequest can ask for.
	MaxPageLimit = 200
)

// firstPageAfterId is what an empty cursor decodes to: every UUID sorts after it.
const firstPageAfterId = "00000000-0000-0000-0000-000000000000"

// ErrInvalidCursor is returned by DecodeCursor for a cursor that EncodeCursor did not make.
var ErrInvalidCursor = errors.New("invalid page cursor")

// PageRequest asks for one page of a list. Page lists use keyset pagination on a UUIDv7
// id (time-ordered): a page holds the records whose id sorts after the id in the cursor.
// The cursor is opaque (base64url of the last id of the previous page); "" is the first
// page.
type PageRequest struct {
	Limit  int
	Cursor string
}

// EffectiveLimit is the size of the page: Limit clamped to [1, MaxPageLimit], and
// DefaultPageLimit when Limit is 0.
func (p PageRequest) EffectiveLimit() int {
	switch {
	case p.Limit == 0:
		return DefaultPageLimit
	case p.Limit < 1:
		return 1
	case p.Limit > MaxPageLimit:
		return MaxPageLimit
	default:
		return p.Limit
	}
}

// EncodeCursor returns the cursor of the page that follows the record with id lastId.
func EncodeCursor(lastId string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(lastId))
}

// DecodeCursor returns the id after which a page starts: the nil UUID for "", the id
// that EncodeCursor encoded otherwise, in canonical form. A cursor that does not hold a
// UUID is an error wrapping ErrInvalidCursor.
func DecodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return firstPageAfterId, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	id, err := uuid.Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	return id.String(), nil
}

// EncodeKeyCursor returns the cursor of the page that follows the record with the text
// key lastKey, for the lists that are ordered by a key that is not a UUID (the charge
// stations, by station key).
func EncodeKeyCursor(lastKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(lastKey))
}

// DecodeKeyCursor returns the key after which a page starts: "" (before every key) for
// "", the key that EncodeKeyCursor encoded otherwise. A cursor that EncodeKeyCursor
// cannot have made is an error wrapping ErrInvalidCursor.
func DecodeKeyCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return "", fmt.Errorf("%w: not a key", ErrInvalidCursor)
	}
	return string(raw), nil
}
