// SPDX-License-Identifier: Apache-2.0

// Package money holds the types for prices and costs: amounts and percentages as fixed
// point numbers, so that no float rounding gets into a price, and the rounding of the
// exact results of a calculation.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const (
	// amountDecimals is the number of decimals of an Amount (OCPI numbers have 4).
	amountDecimals = 4
	// percentDecimals is the number of decimals of a Percent.
	percentDecimals = 2
	// maxExponent is the largest exponent, either way, of a number in exponent notation.
	maxExponent = 30
)

// ErrInvalidNumber is returned for a text that is not a decimal number, has more
// decimals than the type holds, or is too large for it.
var ErrInvalidNumber = errors.New("invalid number")

// Amount is a monetary value in 1/10000 of the currency unit: 0.345 is 3450.
type Amount int64

// ParseAmount reads a decimal number such as "0.345", "12" or "-1.5". Exponent notation
// ("3.45e-1") is read too. A number with more than 4 decimals that are not zero is an
// error wrapping ErrInvalidNumber.
func ParseAmount(s string) (Amount, error) {
	units, err := parseFixed(s, amountDecimals)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	return Amount(units), nil
}

// String returns the amount with its 4 decimals, e.g. "0.3450".
func (a Amount) String() string {
	return formatFixed(int64(a), amountDecimals, false)
}

// MarshalJSON writes the amount as a JSON number without trailing zeros, e.g. 0.345.
func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(formatFixed(int64(a), amountDecimals, true)), nil
}

// UnmarshalJSON reads a JSON number, or a JSON string that holds a number. null leaves
// the amount as it is.
func (a *Amount) UnmarshalJSON(b []byte) error {
	units, isNull, err := unmarshalFixed(b, amountDecimals)
	if err != nil {
		return fmt.Errorf("amount %s: %w", b, err)
	}
	if !isNull {
		*a = Amount(units)
	}
	return nil
}

// Rat returns the amount, in currency units, as an exact rational number.
func (a Amount) Rat() *big.Rat {
	return big.NewRat(int64(a), pow10(amountDecimals))
}

// RoundingMode says how a result that lies exactly between two values is rounded.
type RoundingMode string

const (
	// RoundHalfUp rounds a half away from zero: 2.5 to 3 and -2.5 to -3.
	RoundHalfUp RoundingMode = "half_up"
	// RoundHalfEven rounds a half to the even neighbour: 2.5 to 2 and 3.5 to 4.
	RoundHalfEven RoundingMode = "half_even"
)

// RoundRat rounds r to scale decimals and returns it as an Amount. scale is from 0 to 4
// (a scale outside of it is the nearest of the two); a mode that is not known rounds
// like RoundHalfUp. A result that is too large for an Amount is the largest (or the
// smallest) Amount.
func RoundRat(r *big.Rat, scale int, mode RoundingMode) Amount {
	scale = min(max(scale, 0), amountDecimals)

	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt64(pow10(scale)))
	negative := scaled.Sign() < 0
	num := new(big.Int).Abs(scaled.Num())
	den := scaled.Denom()

	quotient, remainder := new(big.Int).QuoRem(num, den, new(big.Int))
	// the remainder against a half: twice the remainder against the denominator
	half := new(big.Int).Lsh(remainder, 1).Cmp(den)
	roundUp := half > 0
	if half == 0 {
		roundUp = mode != RoundHalfEven || quotient.Bit(0) == 1
	}
	if roundUp {
		quotient.Add(quotient, big.NewInt(1))
	}

	quotient.Mul(quotient, big.NewInt(pow10(amountDecimals-scale)))
	if negative {
		quotient.Neg(quotient)
	}
	switch {
	case quotient.IsInt64():
		return Amount(quotient.Int64())
	case negative:
		return Amount(math.MinInt64)
	default:
		return Amount(math.MaxInt64)
	}
}

// Percent is a percentage in 1/100 of a percent: 21% is 2100.
type Percent int64

// ParsePercent reads a percentage such as "21" or "5.5". A number with more than 2
// decimals that are not zero is an error wrapping ErrInvalidNumber.
func ParsePercent(s string) (Percent, error) {
	units, err := parseFixed(s, percentDecimals)
	if err != nil {
		return 0, fmt.Errorf("percentage %q: %w", s, err)
	}
	return Percent(units), nil
}

// String returns the percentage with its 2 decimals, e.g. "21.00".
func (p Percent) String() string {
	return formatFixed(int64(p), percentDecimals, false)
}

// MarshalJSON writes the percentage as a JSON number without trailing zeros: 21% is 21,
// 5.5% is 5.5.
func (p Percent) MarshalJSON() ([]byte, error) {
	return []byte(formatFixed(int64(p), percentDecimals, true)), nil
}

// UnmarshalJSON reads a JSON number, or a JSON string that holds a number. null leaves
// the percentage as it is.
func (p *Percent) UnmarshalJSON(b []byte) error {
	units, isNull, err := unmarshalFixed(b, percentDecimals)
	if err != nil {
		return fmt.Errorf("percentage %s: %w", b, err)
	}
	if !isNull {
		*p = Percent(units)
	}
	return nil
}

// Rat returns the percentage as an exact rational number: 21% is 21.
func (p Percent) Rat() *big.Rat {
	return big.NewRat(int64(p), pow10(percentDecimals))
}

// Price is an amount without and with VAT (OCPI Price).
type Price struct {
	ExclVat Amount `json:"excl_vat"`
	InclVat Amount `json:"incl_vat"`
}

func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}

// unmarshalFixed reads the JSON value b, a number or a string that holds one, as a fixed
// point number with the given decimals. isNull reports the JSON null.
func unmarshalFixed(b []byte, decimals int) (units int64, isNull bool, err error) {
	text := strings.TrimSpace(string(b))
	if text == "null" {
		return 0, true, nil
	}
	if strings.HasPrefix(text, `"`) {
		if text, err = strconv.Unquote(text); err != nil {
			return 0, false, fmt.Errorf("%w: %v", ErrInvalidNumber, err)
		}
	}
	units, err = parseFixed(text, decimals)
	return units, false, err
}

// parseFixed reads the decimal number s (digits, an optional fraction and an optional
// exponent, as in JSON) as a fixed point number with the given decimals: it returns the
// number times 10^decimals, which must be a whole number that fits an int64.
func parseFixed(s string, decimals int) (int64, error) {
	rest := s
	negative := strings.HasPrefix(rest, "-")
	if negative {
		rest = rest[1:]
	}

	mantissa, exponentText, hasExponent := strings.Cut(strings.ToLower(rest), "e")
	whole, fraction, hasFraction := strings.Cut(mantissa, ".")
	if !isDigits(whole) || (hasFraction && !isDigits(fraction)) {
		return 0, fmt.Errorf("%w: not a decimal number", ErrInvalidNumber)
	}
	exponent := 0
	if hasExponent {
		negativeExponent := strings.HasPrefix(exponentText, "-")
		if negativeExponent || strings.HasPrefix(exponentText, "+") {
			exponentText = exponentText[1:]
		}
		if !isDigits(exponentText) || len(exponentText) > 3 {
			return 0, fmt.Errorf("%w: not a decimal number", ErrInvalidNumber)
		}
		exponent, _ = strconv.Atoi(exponentText)
		if exponent > maxExponent {
			return 0, fmt.Errorf("%w: exponent out of range", ErrInvalidNumber)
		}
		if negativeExponent {
			exponent = -exponent
		}
	}

	// the number is digits x 10^(exponent - len(fraction)); the result is that number
	// times 10^decimals
	digits := whole + fraction
	shift := exponent - len(fraction) + decimals
	if shift >= 0 {
		digits += strings.Repeat("0", shift)
	} else {
		cut := max(len(digits)+shift, 0)
		if strings.Trim(digits[cut:], "0") != "" {
			return 0, fmt.Errorf("%w: more than %d decimals", ErrInvalidNumber, decimals)
		}
		digits = digits[:cut]
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, nil
	}
	if negative {
		digits = "-" + digits
	}
	units, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: out of range", ErrInvalidNumber)
	}
	return units, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// formatFixed writes a fixed point number with the given decimals; trim leaves out the
// zeros at the end of the fraction, and the fraction when nothing is left of it.
func formatFixed(units int64, decimals int, trim bool) string {
	digits := strconv.FormatInt(units, 10)
	sign := ""
	if units < 0 {
		sign, digits = "-", digits[1:]
	}
	// at least one digit before the fraction
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals-len(digits)+1) + digits
	}
	whole, fraction := digits[:len(digits)-decimals], digits[len(digits)-decimals:]
	if trim {
		fraction = strings.TrimRight(fraction, "0")
	}
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}
