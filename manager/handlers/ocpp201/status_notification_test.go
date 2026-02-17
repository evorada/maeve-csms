// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestStatusNotificationHandler(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := handlers.StatusNotificationHandler{Store: memStore}

	req := &types.StatusNotificationRequestJson{
		Timestamp:       "2023-05-01T01:00:00+01:00",
		EvseId:          1,
		ConnectorId:     2,
		ConnectorStatus: types.ConnectorStatusEnumTypeOccupied,
	}

	got, err := handler.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)
	assert.Equal(t, &types.StatusNotificationResponseJson{}, got)

	settings, err := memStore.LookupChargeStationSettings(context.Background(), "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	stored := settings.Settings["ocpp201.connector_status.1.2"]
	require.NotNil(t, stored)
	assert.Equal(t, "Occupied", stored.Value)
	assert.Equal(t, store.ChargeStationSettingStatusAccepted, stored.Status)
}
