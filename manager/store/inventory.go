// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"strconv"
)

// The types of the resources that an inventory lists. They are also the types of the
// URNs of the resources.
const (
	// InventoryStation is a charge station; its id is the station key.
	InventoryStation = "station"
	// InventoryConnector is a connector of an EVSE; its id is ConnectorSubjectKey.
	InventoryConnector = "connector"
	// InventoryLocation is an asset location; its id is the id of the location.
	InventoryLocation = "location"
	// InventoryToken is a token; its id is "{country_code}*{party_id}*{uid}".
	InventoryToken = "token"
)

// InventoryTypes lists the types of resources that an inventory can list.
var InventoryTypes = []string{InventoryStation, InventoryConnector, InventoryLocation, InventoryToken}

const (
	// DefaultInventoryPageLimit is the page size of an inventory without a limit.
	DefaultInventoryPageLimit = 1000
	// MaxInventoryPageLimit is the largest page of an inventory.
	MaxInventoryPageLimit = 5000
)

// InventoryPageLimit is the size of a page of an inventory: page.Limit clamped to
// [1, MaxInventoryPageLimit], and DefaultInventoryPageLimit when it is 0.
func InventoryPageLimit(page PageRequest) int {
	switch {
	case page.Limit == 0:
		return DefaultInventoryPageLimit
	case page.Limit < 1:
		return 1
	case page.Limit > MaxInventoryPageLimit:
		return MaxInventoryPageLimit
	default:
		return page.Limit
	}
}

// InventoryItem is a resource that an org owns.
type InventoryItem struct {
	// Id is the id of the resource, of the form that its type gives.
	Id string
	// Attrs are a few attributes of the resource that depend on its type:
	//   - station: "state" and, when it has one, "location_id"
	//   - connector: "station_key", "evse_uid" and "connector_uid"
	//   - location: "ocpi_id", "party_ref" and "publish" ("true" or "false")
	//   - token: "type" and "valid" ("true" or "false")
	Attrs map[string]string
}

// InventoryStore lists the resources that an org owns: the resources whose own org is
// orgId, never those of the orgs below it. An org that has no such resources has an
// empty inventory. Without includeInactive the inventory leaves out the decommissioned
// charge stations, the connectors of a REMOVED EVSE or of a charge station that is not
// active, and the tokens that are not valid; locations are never inactive.
type InventoryStore interface {
	// ListInventory returns a page of the inventory of the type, ordered by id (byte by
	// byte), after the cursor, and the cursor of the next page ("" after the last page).
	// The cursor holds the id that the previous page ended with (EncodeKeyCursor); the
	// page has InventoryPageLimit(page) items at most.
	ListInventory(ctx context.Context, orgId, typ string, includeInactive bool, page PageRequest) ([]InventoryItem, string, error)
	// CountInventory counts the whole inventory of the type.
	CountInventory(ctx context.Context, orgId, typ string, includeInactive bool) (int, error)
}

// ConnectorSubjectKey is the id of a connector in the inventory and in the usage facts:
// "{station_key}#{ocpp_evse_id}" when its EVSE has one connector (always so for OCPP 1.6,
// where the EVSE id is the 1.6 connector id), "{station_key}#{ocpp_evse_id}.{ocpp_connector_id}"
// when the EVSE has more. evseConnectors is the number of connectors of the EVSE.
func ConnectorSubjectKey(stationKey string, ocppEvseId, ocppConnectorId, evseConnectors int) string {
	key := stationKey + "#" + strconv.Itoa(ocppEvseId)
	if evseConnectors > 1 {
		key += "." + strconv.Itoa(ocppConnectorId)
	}
	return key
}

// TokenInventoryId is the id of a token in the inventory: "{country_code}*{party_id}*{uid}".
func TokenInventoryId(countryCode, partyId, uid string) string {
	return countryCode + "*" + partyId + "*" + uid
}
