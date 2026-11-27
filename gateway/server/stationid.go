// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxStationIdLength is the length of an OCPP 2.0.1 charging station identity.
const maxStationIdLength = 48

// stationIdForbiddenChars are the path separator and the wildcards of MQTT and NATS.
const stationIdForbiddenChars = "/+#*>"

// validateStationId checks the id that a charge station presents in the URL: 1 to 48
// bytes of valid UTF-8, none of "/ + # * >", no "." at the start or at the end, no
// whitespace and nothing that is not printable. A "." in the middle is allowed.
//
// The manager applies the same rules to the lookup of a charge station; an id that only
// the manager refuses is answered with an internal server error instead of a bad request.
func validateStationId(id string) error {
	if id == "" {
		return errors.New("station id is empty")
	}
	if len(id) > maxStationIdLength {
		return fmt.Errorf("station id is longer than %d bytes", maxStationIdLength)
	}
	if !utf8.ValidString(id) {
		return errors.New("station id is not valid UTF-8")
	}
	if strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") {
		return errors.New(`station id starts or ends with "."`)
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
