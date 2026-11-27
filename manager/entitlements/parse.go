// SPDX-License-Identifier: Apache-2.0

package entitlements

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// LimitConnectorsMax is the limit on the number of connectors.
const LimitConnectorsMax = "cpms.connectors.max"

// The values of Limit.Enforcement.
const (
	EnforcementHard = "hard"
	EnforcementSoft = "soft"
)

// The values of Limit.Scope.
const (
	ScopeOrg     = "org"
	ScopeSubtree = "subtree"
)

// Limit is one limit of an entitlement document.
type Limit struct {
	Value int64 `json:"value"`
	// Enforcement is hard for a limit that refuses what goes beyond it and soft for one
	// that is only billed.
	Enforcement string `json:"enforcement"`
	// Scope is org for a limit of the org alone and subtree for one that is shared with
	// the orgs below.
	Scope string `json:"scope"`
	// PoolOrgId is the org whose subtree shares a limit with the scope subtree. Empty is
	// the org of the document.
	PoolOrgId string `json:"pool_org_id,omitempty"`
}

// Document is the entitlement document of a tenant.
type Document struct {
	OrgId    string           `json:"org_id"`
	Version  int64            `json:"version"`
	Status   string           `json:"status"`
	Features map[string]bool  `json:"features"`
	Limits   map[string]Limit `json:"limits"`
}

// Parse reads an entitlement document as the tenant store keeps it. No document, null
// and {} give an empty document, which has no limits. Members that are not known are
// ignored. A document that is not a JSON object, or whose known members have another
// type, is an error.
func Parse(raw json.RawMessage) (*Document, error) {
	doc := &Document{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("entitlement document: %w", err)
	}
	return doc, nil
}
