// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// stationKeySeparator separates the org key from the station id. An org key never
// contains it; a station id may.
const stationKeySeparator = "."

// maxStationIdLength is the length of an OCPP 2.0.1 charging station identity.
const maxStationIdLength = 48

// stationIdForbiddenChars are the path separator and the wildcards of MQTT and NATS.
const stationIdForbiddenChars = "/+#*>"

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

// ValidateStationId checks the id that a charge station presents (the plain id, without
// an org key): 1 to 48 bytes of valid UTF-8, none of "/ + # * >", no "." at the start or
// at the end, no whitespace and nothing that is not printable. A "." in the middle is
// allowed.
func ValidateStationId(id string) error {
	if id == "" {
		return errors.New("station id is empty")
	}
	if len(id) > maxStationIdLength {
		return fmt.Errorf("station id is longer than %d bytes", maxStationIdLength)
	}
	if !utf8.ValidString(id) {
		return errors.New("station id is not valid UTF-8")
	}
	if strings.HasPrefix(id, stationKeySeparator) || strings.HasSuffix(id, stationKeySeparator) {
		return fmt.Errorf("station id starts or ends with %q", stationKeySeparator)
	}
	for _, r := range id {
		switch {
		case strings.ContainsRune(stationIdForbiddenChars, r):
			return fmt.Errorf("station id contains %q", r)
		case unicode.IsSpace(r):
			return errors.New("station id contains whitespace")
		case !unicode.IsPrint(r):
			return errors.New("station id contains a character that is not printable")
		}
	}
	return nil
}
