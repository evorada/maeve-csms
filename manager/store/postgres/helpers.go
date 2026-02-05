// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Helper functions for pgtype conversions
// These functions provide consistent type conversions between Go types and PostgreSQL types.

// textFromString converts a *string to pgtype.Text
func textFromString(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// stringFromText converts pgtype.Text to *string
func stringFromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

// toNullableText converts a string to pgtype.Text (treats empty string as NULL)
func toNullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// timestampFromTime converts time.Time to pgtype.Timestamp
func timestampFromTime(t time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  t,
		Valid: true,
	}
}

// timeFromTimestamp converts pgtype.Timestamp to time.Time
func timeFromTimestamp(ts pgtype.Timestamp) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}

// toPgTimestamp converts time.Time to pgtype.Timestamp (treats zero time as NULL)
func toPgTimestamp(t time.Time) pgtype.Timestamp {
	if t.IsZero() {
		return pgtype.Timestamp{Valid: false}
	}
	return pgtype.Timestamp{Time: t, Valid: true}
}

// fromNullableText converts pgtype.Text to string (returns empty string for NULL)
func fromNullableText(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// fromPgTimestamp is an alias for timeFromTimestamp for consistency
func fromPgTimestamp(ts pgtype.Timestamp) time.Time {
	return timeFromTimestamp(ts)
}

// securityProfileFromInt32 safely converts int32 to SecurityProfile with validation
func securityProfileFromInt32(val int32) (store.SecurityProfile, error) {
	// SecurityProfile is int8, so valid range is -128 to 127
	// In practice, valid values are 0-2
	if val < 0 || val > 127 {
		return 0, fmt.Errorf("security profile value %d is out of valid range", val)
	}
	return store.SecurityProfile(val), nil
}

// safeIntToInt32 safely converts int to int32 with overflow check
func safeIntToInt32(val int) (int32, error) {
	if val < -2147483648 || val > 2147483647 {
		return 0, fmt.Errorf("value %d overflows int32", val)
	}
	return int32(val), nil
}
