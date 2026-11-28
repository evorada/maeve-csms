// SPDX-License-Identifier: Apache-2.0

package assets_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/assets"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestGenerateEvseId(t *testing.T) {
	assert.Equal(t, "ES*CCM*ECP001A*2", assets.GenerateEvseId("ES", "CCM", "cp-001_a", 2))
	assert.Equal(t, "ES*CCM*ECP001A*2", assets.GenerateEvseId("es", "ccm", "cp-001_a", 2))
	assert.Equal(t, "NL*ABC*E1*12", assets.GenerateEvseId("NL", "ABC", "ü.1", 12), "characters outside A-Z and 0-9 are dropped")
}

func TestGenerateEvseIdCutsLongStationIds(t *testing.T) {
	stationId := strings.Repeat("abcdefghij", 6) // 60 characters
	for _, ocppEvseId := range []int{1, 12, 123456} {
		id := assets.GenerateEvseId("ES", "CCM", stationId, ocppEvseId)
		assert.LessOrEqual(t, len(id), 48, id)
		assert.Len(t, id, 38, "power outlet id of 30 characters")
		assert.True(t, strings.HasPrefix(id, "ES*CCM*EABCDEFGHIJ"), id)
		assert.NoError(t, assets.ValidateEvse(&store.AssetEvse{OcppEvseId: ocppEvseId, EvseId: &id}), id)
	}
	assert.Equal(t, "ES*CCM*EABCDEFGHIJABCDEFGHIJABCDEFGH*1", assets.GenerateEvseId("ES", "CCM", stationId, 1))
}

func TestConnectorSubjectKey(t *testing.T) {
	single := &store.AssetEvse{StationKey: "k7f2.CP001", OcppEvseId: 1, Connectors: []*store.AssetConnector{{OcppConnectorId: 1}}}
	assert.Equal(t, "k7f2.CP001#1", assets.ConnectorSubjectKey(single, single.Connectors[0]), "the connector of an EVSE with one connector is named by the EVSE")

	// an OCPP 1.6 station: every connector is an EVSE of its own, with connector id 1
	ocpp16 := &store.AssetEvse{StationKey: "k7f2.CP002", OcppEvseId: 3, Connectors: []*store.AssetConnector{{OcppConnectorId: 1}}}
	assert.Equal(t, "k7f2.CP002#3", assets.ConnectorSubjectKey(ocpp16, ocpp16.Connectors[0]), "the 1.6 connector id")

	multi := &store.AssetEvse{StationKey: "k7f2.CP003", OcppEvseId: 2, Connectors: []*store.AssetConnector{{OcppConnectorId: 1}, {OcppConnectorId: 2}}}
	assert.Equal(t, "k7f2.CP003#2.1", assets.ConnectorSubjectKey(multi, multi.Connectors[0]))
	assert.Equal(t, "k7f2.CP003#2.2", assets.ConnectorSubjectKey(multi, multi.Connectors[1]), "each connector of an EVSE with several is counted")
}
