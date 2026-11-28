// SPDX-License-Identifier: Apache-2.0

package assets

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// maxPowerOutletIdLength is the most characters that an eMI3 EVSE id has after its "E".
const maxPowerOutletIdLength = 30

var notAlphanumeric = regexp.MustCompile(`[^A-Z0-9]`)

// GenerateEvseId returns the eMI3 EVSE id {CC}*{PID}*E{STATIONID}*{N} of the EVSE with
// the OCPP EVSE id N of a station of the party. STATIONID is the station id in upper case
// without the characters outside A-Z and 0-9, cut so that the power outlet id
// "{STATIONID}*{N}" has at most 30 characters, as eMI3 allows. With a two-letter country
// and a three-character party the id then has at most 38 characters, within the 48 of
// OCPI.
func GenerateEvseId(countryCode, partyId, stationId string, ocppEvseId int) string {
	suffix := "*" + strconv.Itoa(ocppEvseId)
	station := notAlphanumeric.ReplaceAllString(strings.ToUpper(stationId), "")
	if room := max(maxPowerOutletIdLength-len(suffix), 0); len(station) > room {
		station = station[:room]
	}
	return strings.ToUpper(countryCode) + "*" + strings.ToUpper(partyId) + "*E" + station + suffix
}

// ConnectorSubjectKey returns the id of the connector of the EVSE in the inventory of its
// org and in the usage facts, so that both name a connector the same way:
// "{station_key}#{ocpp_evse_id}" when the EVSE has one connector (every EVSE of an OCPP 1.6
// station, whose EVSE id is the 1.6 connector id), "{station_key}#{ocpp_evse_id}.{ocpp_connector_id}"
// when it has more. The EVSE must carry its connectors, as every read of an EVSE does.
func ConnectorSubjectKey(evse *store.AssetEvse, connector *store.AssetConnector) string {
	return store.ConnectorSubjectKey(evse.StationKey, evse.OcppEvseId, connector.OcppConnectorId, len(evse.Connectors))
}
