// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

// toNullString converts *string to sql.NullString
func toNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
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

// timestamptzFromTime converts *time.Time to pgtype.Timestamptz
func timestamptzFromTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// timestamptzToTime converts pgtype.Timestamptz to *time.Time
func timestamptzToTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// toNullableTimestamptz converts time.Time to pgtype.Timestamptz (treats zero time as NULL)
func toNullableTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// fromNullableText converts pgtype.Text to string (returns empty string for NULL)
func fromNullableText(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// fromNullString converts sql.NullString to *string
func fromNullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// fromPgTimestamp is an alias for timeFromTimestamp for consistency
func fromPgTimestamp(ts pgtype.Timestamp) time.Time {
	return timeFromTimestamp(ts)
}
