// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"math"
	"math/big"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
)

func TestNumericFromAmount(t *testing.T) {
	for amount, want := range map[money.Amount]string{
		0:             "0e-4",
		3450:          "3450e-4",
		-15000:        "-15000e-4",
		math.MaxInt64: "9223372036854775807e-4",
	} {
		n := numericFromAmount(amount)
		require.True(t, n.Valid)
		assert.Equal(t, want, n.Int.String()+"e"+big.NewInt(int64(n.Exp)).String())

		back, ok, err := amountFromNumeric(n)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, amount, back)
	}
}

// PostgreSQL gives a NUMERIC as a whole number and a power of ten, which is not always
// the scale of the column: 0.5000 can arrive as 5 x 10^-1.
func TestAmountFromNumeric(t *testing.T) {
	numeric := func(digits int64, exp int32) pgtype.Numeric {
		return pgtype.Numeric{Int: big.NewInt(digits), Exp: exp, Valid: true}
	}
	for name, tc := range map[string]struct {
		numeric pgtype.Numeric
		want    money.Amount
	}{
		"the scale of the column":   {numeric(5000, -4), 5000},
		"fewer decimals":            {numeric(5, -1), 5000},
		"a whole number":            {numeric(12, 0), 120000},
		"thousands":                 {numeric(12, 3), 120000000},
		"zeros after the decimals":  {numeric(34500000, -8), 3450},
		"negative":                  {numeric(-15, -1), -15000},
		"zero":                      {numeric(0, 0), 0},
		"zero with a large power":   {numeric(0, 100), 0},
		"zero with a small power":   {numeric(0, -100), 0},
		"the largest of the column": {numeric(99999999999999, -4), 99999999999999},
	} {
		got, ok, err := amountFromNumeric(tc.numeric)
		require.NoError(t, err, name)
		assert.True(t, ok, name)
		assert.Equal(t, tc.want, got, name)
	}

	got, ok, err := amountFromNumeric(pgtype.Numeric{})
	require.NoError(t, err)
	assert.False(t, ok, "NULL is no amount")
	assert.Zero(t, got)

	for name, n := range map[string]pgtype.Numeric{
		"more than 4 decimals": numeric(12345, -5),
		"far more decimals":    numeric(1, -30),
		"too large":            numeric(math.MaxInt64, 0),
		"far too large":        numeric(1, 30),
		"not a number":         {NaN: true, Valid: true},
		"infinity":             {InfinityModifier: pgtype.Infinity, Valid: true},
		"no digits":            {Valid: true},
	} {
		_, ok, err := amountFromNumeric(n)
		assert.Error(t, err, name)
		assert.False(t, ok, name)
	}
}
