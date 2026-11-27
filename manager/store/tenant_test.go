// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestOrgPathLabel(t *testing.T) {
	tests := map[string]string{
		"0193b2c4-7a1e-7c3d-9f00-1234567890ab": "0193b2c47a1e7c3d9f001234567890ab",
		"0193B2C4-7A1E-7C3D-9F00-1234567890AB": "0193b2c47a1e7c3d9f001234567890ab",
		store.LegacyOrgId:                      "00000000000000000000000000000000",
	}
	for orgId, want := range tests {
		t.Run(orgId, func(t *testing.T) {
			assert.Equal(t, want, store.OrgPathLabel(orgId))
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := map[string]string{
		"ChargeCo.OCPP.example.com:443.": "chargeco.ocpp.example.com",
		"ChargeCo.OCPP.example.com:443":  "chargeco.ocpp.example.com",
		"chargeco.ocpp.example.com.:443": "chargeco.ocpp.example.com",
		"chargeco.ocpp.example.com.":     "chargeco.ocpp.example.com",
		"chargeco.ocpp.example.com":      "chargeco.ocpp.example.com",
		"LOCALHOST:9410":                 "localhost",
		"[::1]:9410":                     "::1",
		"":                               "",
	}
	for host, want := range tests {
		t.Run(host, func(t *testing.T) {
			assert.Equal(t, want, store.NormalizeHost(host))
		})
	}
}
