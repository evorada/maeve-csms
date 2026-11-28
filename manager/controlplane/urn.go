// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"errors"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/events"
)

// URN names a resource of an org: urn:<product>:<org_id>:<type>/<id>, as the subject of
// a domain event does (events.URN).
type URN struct {
	Product string
	// OrgId is the org that owns the resource, in lowercase.
	OrgId string
	// Type is the type of the resource, like events.ResourceStation.
	Type string
	// Id is the id of the resource, like the key of a charge station.
	Id string
}

// String returns the text of the URN.
func (u URN) String() string {
	return events.URN(u.Product, u.OrgId, u.Type, u.Id)
}

// errInvalidURN is a text that is not the URN of a resource of the manager.
var errInvalidURN = errors.New("must be a URN of the form urn:" + events.Product + ":<org_id>:<type>/<id>")

// ParseURN reads the URN of a resource of the manager: the product must be cpms, the org
// a UUID, and the type and the id must not be empty. s is the URN itself, not its
// percent-encoded form in a path.
func ParseURN(s string) (URN, error) {
	rest, ok := strings.CutPrefix(s, "urn:")
	if !ok {
		return URN{}, errInvalidURN
	}
	product, rest, ok := strings.Cut(rest, ":")
	if !ok || product != events.Product {
		return URN{}, errInvalidURN
	}
	org, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return URN{}, errInvalidURN
	}
	orgId, ok := canonicalOrgId(org)
	if !ok {
		return URN{}, errInvalidURN
	}
	resourceType, id, ok := strings.Cut(rest, "/")
	if !ok || resourceType == "" || id == "" {
		return URN{}, errInvalidURN
	}
	return URN{Product: product, OrgId: orgId, Type: resourceType, Id: id}, nil
}
