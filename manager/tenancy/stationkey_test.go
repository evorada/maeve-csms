// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

func TestStationKeyRoundTrip(t *testing.T) {
	key := tenancy.StationKey("k7f2", "CP001")
	assert.Equal(t, "k7f2.CP001", key)

	orgKey, stationId, ok := tenancy.SplitStationKey(key)
	assert.True(t, ok)
	assert.Equal(t, "k7f2", orgKey)
	assert.Equal(t, "CP001", stationId)
}

func TestStationKeyWithDotInStationId(t *testing.T) {
	key := tenancy.StationKey("k7f2", "CP.001")
	assert.Equal(t, "k7f2.CP.001", key)

	// only the first "." separates: the org key never contains one
	orgKey, stationId, ok := tenancy.SplitStationKey(key)
	assert.True(t, ok)
	assert.Equal(t, "k7f2", orgKey)
	assert.Equal(t, "CP.001", stationId)
}

func TestSplitStationKeyWithoutDot(t *testing.T) {
	orgKey, stationId, ok := tenancy.SplitStationKey("CP001")
	assert.False(t, ok)
	assert.Equal(t, "", orgKey)
	assert.Equal(t, "CP001", stationId)
}

func TestStationKeyWithoutOrgKey(t *testing.T) {
	assert.Equal(t, "CP001", tenancy.StationKey("", "CP001"))
}

func TestValidateStationId(t *testing.T) {
	valid := map[string]string{
		"plain id":              "CP001",
		"dot in the middle":     "CP.001",
		"48 characters":         strings.Repeat("a", 48),
		"hyphen and underscore": "cp-001_a",
		"one character":         "1",
	}
	for name, id := range valid {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, tenancy.ValidateStationId(id))
		})
	}

	invalid := map[string]string{
		"empty":                "",
		"49 characters":        strings.Repeat("a", 49),
		"plus":                 "CP+001",
		"hash":                 "CP#001",
		"asterisk":             "CP*001",
		"greater than":         "CP>001",
		"slash":                "CP/001",
		"space":                "CP 001",
		"tab":                  "CP\t001",
		"newline at the end":   "CP001\n",
		"leading dot":          ".CP001",
		"trailing dot":         "CP001.",
		"only a dot":           ".",
		"control character":    "CP\x00001",
		"delete character":     "CP\x7f001",
		"non-breaking space":   "CP\u00a0001",
		"zero width space":     "CP\u200b001",
		"invalid UTF-8":        "CP\xff001",
		"48 runes of 2 bytes":  strings.Repeat("é", 48),
		"wildcard as whole id": "#",
	}
	for name, id := range invalid {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, tenancy.ValidateStationId(id))
		})
	}
}
