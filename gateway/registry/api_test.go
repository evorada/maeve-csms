// SPDX-License-Identifier: Apache-2.0

package registry_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
)

func TestNormalizeHost(t *testing.T) {
	hosts := map[string]string{
		"a.example":            "a.example",
		"A.Example":            "a.example",
		"a.example:443":        "a.example",
		"a.example.":           "a.example",
		"A.EXAMPLE.:8080":      "a.example",
		"127.0.0.1:9310":       "127.0.0.1",
		"[::1]:9310":           "::1",
		"":                     "",
		":9310":                "",
		".":                    "",
		"a.example:443:443":    "a.example:443:443",
		"b.example\x00:80":     "b.example\x00",
		"a.example@b.example":  "a.example@b.example",
		"b.example/a.example":  "b.example/a.example",
		"a.example, b.example": "a.example, b.example",
	}
	for host, want := range hosts {
		assert.Equal(t, want, registry.NormalizeHost(host), "host %q", host)
	}
}
