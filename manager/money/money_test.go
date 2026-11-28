// SPDX-License-Identifier: Apache-2.0

package money_test

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
)

func TestParseAmount(t *testing.T) {
	for text, want := range map[string]money.Amount{
		"0":                    0,
		"-0":                   0,
		"0.345":                3450,
		"12":                   120000,
		"-1.5":                 -15000,
		"0.0001":               1,
		"-0.0001":              -1,
		"007.10":               71000,
		"0.30000":              3000, // zeros after the fourth decimal are no decimals
		"3.45e-1":              3450,
		"1E2":                  1000000,
		"1e+2":                 1000000,
		"12000e-4":             12000,
		"922337203685477.5807": math.MaxInt64,
	} {
		got, err := money.ParseAmount(text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
	}
}

func TestParseAmountRejects(t *testing.T) {
	for _, text := range []string{
		"", "-", "+1", "abc", "1,5", "1.", ".5", "1.2.3", " 1", "1 ", "0x10", "1/3", "1_000",
		"0.34567", // more than 4 decimals
		"0.00001", // more than 4 decimals
		"1e-5",    // more than 4 decimals
		"1e", "1e+", "1e+-2", "1e1.5", "1e31", "1e1000",
		"922337203685477.5808", // too large
		"NaN", "Infinity",
	} {
		_, err := money.ParseAmount(text)
		assert.ErrorIs(t, err, money.ErrInvalidNumber, "%q", text)
	}
}

func TestAmountString(t *testing.T) {
	for amount, want := range map[money.Amount]string{
		0:             "0.0000",
		3450:          "0.3450",
		120000:        "12.0000",
		-15000:        "-1.5000",
		1:             "0.0001",
		-1:            "-0.0001",
		math.MaxInt64: "922337203685477.5807",
		math.MinInt64: "-922337203685477.5808",
	} {
		assert.Equal(t, want, amount.String())
	}
}

func TestAmountJSON(t *testing.T) {
	for amount, want := range map[money.Amount]string{
		0:       "0",
		3450:    "0.345",
		120000:  "12",
		-15000:  "-1.5",
		1:       "0.0001",
		-1:      "-0.0001",
		8571:    "0.8571",
		1000000: "100",
	} {
		document, err := json.Marshal(amount)
		require.NoError(t, err)
		assert.Equal(t, want, string(document))

		var back money.Amount
		require.NoError(t, json.Unmarshal(document, &back))
		assert.Equal(t, amount, back, want)
	}

	t.Run("a number or a text", func(t *testing.T) {
		var doc struct {
			Number  money.Amount  `json:"number"`
			Text    money.Amount  `json:"text"`
			Pointer *money.Amount `json:"pointer"`
			Absent  *money.Amount `json:"absent,omitempty"`
			Null    money.Amount  `json:"null"`
		}
		doc.Null = 7
		require.NoError(t, json.Unmarshal([]byte(`{"number": 0.3450, "text": "12.5", "pointer": 1e-1, "null": null}`), &doc))
		assert.Equal(t, money.Amount(3450), doc.Number)
		assert.Equal(t, money.Amount(125000), doc.Text)
		require.NotNil(t, doc.Pointer)
		assert.Equal(t, money.Amount(1000), *doc.Pointer)
		assert.Nil(t, doc.Absent)
		assert.Equal(t, money.Amount(7), doc.Null, "null leaves the amount as it is")

		document, err := json.Marshal(doc)
		require.NoError(t, err)
		assert.JSONEq(t, `{"number": 0.345, "text": 12.5, "pointer": 0.1, "null": 0.0007}`, string(document))
	})

	t.Run("what is not an amount", func(t *testing.T) {
		for _, document := range []string{`0.34567`, `"0.34567"`, `"abc"`, `true`, `{}`, `[1]`, `""`} {
			var amount money.Amount
			assert.ErrorIs(t, json.Unmarshal([]byte(document), &amount), money.ErrInvalidNumber, document)
		}
	})
}

func TestAmountRat(t *testing.T) {
	assert.Equal(t, "69/200", money.Amount(3450).Rat().String())
	assert.Equal(t, "-3/2", money.Amount(-15000).Rat().String())
	assert.Equal(t, "0/1", money.Amount(0).Rat().String())
}

func TestRoundRat(t *testing.T) {
	rat := func(text string) *big.Rat {
		r, ok := new(big.Rat).SetString(text)
		require.True(t, ok, text)
		return r
	}
	for _, tc := range []struct {
		value string
		scale int
		mode  money.RoundingMode
		want  string
	}{
		{"2.5", 0, money.RoundHalfUp, "3.0000"},
		{"-2.5", 0, money.RoundHalfUp, "-3.0000"},
		{"3.025", 2, money.RoundHalfUp, "3.0300"},
		{"-3.025", 2, money.RoundHalfUp, "-3.0300"},
		{"3.0249", 2, money.RoundHalfUp, "3.0200"},
		{"3.025", 2, money.RoundHalfEven, "3.0200"},
		{"3.035", 2, money.RoundHalfEven, "3.0400"},
		{"-3.025", 2, money.RoundHalfEven, "-3.0200"},
		{"3.0251", 2, money.RoundHalfEven, "3.0300"},
		{"2.5", 0, money.RoundHalfEven, "2.0000"},
		{"3.5", 0, money.RoundHalfEven, "4.0000"},
		{"0.5", 0, money.RoundHalfEven, "0.0000"},
		{"6/7", 4, money.RoundHalfUp, "0.8571"},
		{"1/3", 4, money.RoundHalfUp, "0.3333"},
		{"2/3", 4, money.RoundHalfUp, "0.6667"},
		{"0.00005", 4, money.RoundHalfUp, "0.0001"},
		{"0.00005", 4, money.RoundHalfEven, "0.0000"},
		{"7.875", 2, money.RoundHalfUp, "7.8800"},
		{"9.52875", 2, money.RoundHalfUp, "9.5300"},
		{"12", 2, money.RoundHalfUp, "12.0000"},
		{"0", 2, money.RoundHalfUp, "0.0000"},
		{"1.23456", 9, money.RoundHalfUp, "1.2346"},    // a scale above 4 is 4
		{"1.5", -1, money.RoundHalfUp, "2.0000"},       // a scale below 0 is 0
		{"2.5", 0, money.RoundingMode(""), "3.0000"},   // a mode that is not known is half up
		{"2.5", 0, money.RoundingMode("up"), "3.0000"}, //
		{"1e30", 2, money.RoundHalfUp, "922337203685477.5807"},
		{"-1e30", 2, money.RoundHalfUp, "-922337203685477.5808"},
	} {
		got := money.RoundRat(rat(tc.value), tc.scale, tc.mode)
		assert.Equal(t, tc.want, got.String(), "%s at scale %d, %s", tc.value, tc.scale, tc.mode)
	}

	t.Run("the value is not changed", func(t *testing.T) {
		r := big.NewRat(5, 2)
		money.RoundRat(r, 0, money.RoundHalfUp)
		assert.Equal(t, "5/2", r.String())
	})
}

func TestPercent(t *testing.T) {
	for text, want := range map[string]money.Percent{
		"21":    2100,
		"21.0":  2100,
		"5.5":   550,
		"0":     0,
		"100":   10000,
		"19.25": 1925,
		"-1":    -100,
	} {
		got, err := money.ParsePercent(text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
	}
	for _, text := range []string{"", "21%", "5.555", "abc"} {
		_, err := money.ParsePercent(text)
		assert.ErrorIs(t, err, money.ErrInvalidNumber, "%q", text)
	}

	assert.Equal(t, "21.00", money.Percent(2100).String())
	assert.Equal(t, "5.50", money.Percent(550).String())
	assert.Equal(t, "21/1", money.Percent(2100).Rat().String())
	assert.Equal(t, "11/2", money.Percent(550).Rat().String())
}

func TestPercentJSON(t *testing.T) {
	for percent, want := range map[money.Percent]string{
		2100:  "21",
		550:   "5.5",
		1925:  "19.25",
		0:     "0",
		10000: "100",
	} {
		document, err := json.Marshal(percent)
		require.NoError(t, err)
		assert.Equal(t, want, string(document))

		var back money.Percent
		require.NoError(t, json.Unmarshal(document, &back))
		assert.Equal(t, percent, back, want)
	}

	var doc struct {
		Vat    *money.Percent `json:"vat,omitempty"`
		Absent *money.Percent `json:"absent,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"vat": 21.0}`), &doc))
	require.NotNil(t, doc.Vat)
	assert.Equal(t, money.Percent(2100), *doc.Vat)
	assert.Nil(t, doc.Absent)
	require.NoError(t, json.Unmarshal([]byte(`{"vat": "5.5"}`), &doc))
	assert.Equal(t, money.Percent(550), *doc.Vat)

	var percent money.Percent
	assert.ErrorIs(t, json.Unmarshal([]byte(`21.125`), &percent), money.ErrInvalidNumber)
}

func TestPriceJSON(t *testing.T) {
	price := money.Price{ExclVat: 10000, InclVat: 12100}
	document, err := json.Marshal(price)
	require.NoError(t, err)
	assert.JSONEq(t, `{"excl_vat": 1, "incl_vat": 1.21}`, string(document))

	var back money.Price
	require.NoError(t, json.Unmarshal([]byte(`{"excl_vat": 1.00, "incl_vat": "1.21"}`), &back))
	assert.Equal(t, price, back)
}
