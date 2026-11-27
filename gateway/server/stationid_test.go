// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The cases are the ones of the manager, which applies the same rules.
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
			assert.NoError(t, validateStationId(id))
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
			assert.Error(t, validateStationId(id))
		})
	}
}
