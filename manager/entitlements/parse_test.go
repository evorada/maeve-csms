// SPDX-License-Identifier: Apache-2.0

package entitlements_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
)

func TestParseReadsTheDocumentOfThePlatform(t *testing.T) {
	doc, err := entitlements.Parse(json.RawMessage(`{
		"org_id": "0193b2c4-0000-7000-8000-0000000000a1",
		"version": 12,
		"status": "active",
		"features": {"cpms": true, "cpms.ocpi_roaming": true},
		"limits": {"cpms.connectors.max": {"value": 200, "enforcement": "soft", "scope": "org"}}
	}`))
	require.NoError(t, err)

	assert.Equal(t, &entitlements.Document{
		OrgId:    "0193b2c4-0000-7000-8000-0000000000a1",
		Version:  12,
		Status:   "active",
		Features: map[string]bool{"cpms": true, "cpms.ocpi_roaming": true},
		Limits: map[string]entitlements.Limit{
			entitlements.LimitConnectorsMax: {Value: 200, Enforcement: entitlements.EnforcementSoft, Scope: entitlements.ScopeOrg},
		},
	}, doc)
}

func TestParseReadsALimitThatIsSharedByASubtree(t *testing.T) {
	doc, err := entitlements.Parse(json.RawMessage(`{"version":3,"limits":{
		"cpms.connectors.max":{"value":50,"enforcement":"hard","scope":"subtree","pool_org_id":"0193b2c4-0000-7000-8000-0000000000f0"},
		"cpms.locations.max":{"value":5,"enforcement":"hard","scope":"org"}
	}}`))
	require.NoError(t, err)

	assert.Equal(t, entitlements.Limit{
		Value:       50,
		Enforcement: entitlements.EnforcementHard,
		Scope:       entitlements.ScopeSubtree,
		PoolOrgId:   "0193b2c4-0000-7000-8000-0000000000f0",
	}, doc.Limits[entitlements.LimitConnectorsMax])
	assert.Len(t, doc.Limits, 2)
}

func TestParseIgnoresWhatItDoesNotKnow(t *testing.T) {
	doc, err := entitlements.Parse(json.RawMessage(`{
		"version": 4,
		"billing": {"plan": "pro", "seats": 3},
		"limits": {"cpms.connectors.max": {"value": 10, "enforcement": "hard", "scope": "org", "grace": {"days": 7}}}
	}`))
	require.NoError(t, err)

	assert.Equal(t, int64(4), doc.Version)
	assert.Equal(t, map[string]entitlements.Limit{
		entitlements.LimitConnectorsMax: {Value: 10, Enforcement: entitlements.EnforcementHard, Scope: entitlements.ScopeOrg},
	}, doc.Limits)
}

func TestParseOfNoDocumentIsAnEmptyDocument(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"an empty object": json.RawMessage(`{}`),
		"nothing":         nil,
		"empty":           json.RawMessage(``),
		"white space":     json.RawMessage(" \n"),
		"null":            json.RawMessage(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := entitlements.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, &entitlements.Document{}, doc)
			assert.Empty(t, doc.Limits)
		})
	}
}

func TestParseRefusesADocumentThatItCannotRead(t *testing.T) {
	for name, raw := range map[string]string{
		"not JSON":                    `{"version":`,
		"a list":                      `[]`,
		"a text":                      `"limits"`,
		"a limit that is a number":    `{"version":3,"limits":{"cpms.connectors.max":10}}`,
		"a value that is a text":      `{"limits":{"cpms.connectors.max":{"value":"10","enforcement":"hard","scope":"org"}}}`,
		"a value with a fraction":     `{"limits":{"cpms.connectors.max":{"value":10.5,"enforcement":"hard","scope":"org"}}}`,
		"a value beyond 64 bits":      `{"limits":{"cpms.connectors.max":{"value":92233720368547758080,"enforcement":"hard","scope":"org"}}}`,
		"limits that are a list":      `{"limits":[]}`,
		"a feature that is not a bit": `{"features":{"cpms":"yes"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := entitlements.Parse(json.RawMessage(raw))
			require.Error(t, err)
			assert.Nil(t, doc)
		})
	}
}
