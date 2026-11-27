// SPDX-License-Identifier: Apache-2.0

package tenancy

import "strings"

// stationKeySeparator separates the org key from the station id. An org key never
// contains it; a station id may.
const stationKeySeparator = "."

// StationKey returns "{orgKey}.{stationId}", or stationId when orgKey is "" (single-tenant).
func StationKey(orgKey, stationId string) string {
	if orgKey == "" {
		return stationId
	}
	return orgKey + stationKeySeparator + stationId
}

// SplitStationKey splits on the first "."; ok is false when there is no ".".
// Without a "." the key is returned as the station id, with an empty org key.
func SplitStationKey(key string) (orgKey, stationId string, ok bool) {
	orgKey, stationId, ok = strings.Cut(key, stationKeySeparator)
	if !ok {
		return "", key, false
	}
	return orgKey, stationId, true
}
