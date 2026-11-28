// SPDX-License-Identifier: Apache-2.0

package events

// URN returns the URN of a resource, urn:<product>:<org_id>:<type>/<id>, e.g.
// "urn:cpms:<org_id>:session/<id>". The parts are used as they are, without escaping.
func URN(product, orgId, resourceType, resourceId string) string {
	return "urn:" + product + ":" + orgId + ":" + resourceType + "/" + resourceId
}
