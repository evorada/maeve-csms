// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

func TestSendLocalListResultHandler_Accepted_FullUpdate(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	expiryDate := "2027-01-01T00:00:00Z"
	parentIdTag := "parent001"

	request := &types.SendLocalListJson{
		ListVersion: 1,
		UpdateType:  types.SendLocalListJsonUpdateTypeFull,
		LocalAuthorizationList: []types.SendLocalListJsonLocalAuthorizationListEntry{
			{
				IdTag: "tag001",
				IdTagInfo: &types.SendLocalListJsonIdTagInfo{
					Status:      types.SendLocalListJsonIdTagInfoStatusAccepted,
					ExpiryDate:  &expiryDate,
					ParentIdTag: &parentIdTag,
				},
			},
			{
				IdTag: "tag002",
				IdTagInfo: &types.SendLocalListJsonIdTagInfo{
					Status: types.SendLocalListJsonIdTagInfoStatusBlocked,
				},
			},
		},
	}

	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.NoError(t, err)

	// Verify the store was updated
	version, err := engine.GetLocalListVersion(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	entries, err := engine.GetLocalAuthList(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestSendLocalListResultHandler_Accepted_DifferentialUpdate(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	// Set up initial list
	err := engine.UpdateLocalAuthList(ctx, chargeStationId, 1, "Full", nil)
	require.NoError(t, err)

	request := &types.SendLocalListJson{
		ListVersion: 2,
		UpdateType:  types.SendLocalListJsonUpdateTypeDifferential,
		LocalAuthorizationList: []types.SendLocalListJsonLocalAuthorizationListEntry{
			{
				IdTag: "newtag",
				IdTagInfo: &types.SendLocalListJsonIdTagInfo{
					Status: types.SendLocalListJsonIdTagInfoStatusAccepted,
				},
			},
		},
	}

	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusAccepted,
	}

	err = handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.NoError(t, err)

	version, err := engine.GetLocalListVersion(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Equal(t, 2, version)
}

func TestSendLocalListResultHandler_Accepted_EmptyList(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}
	ctx := context.Background()
	chargeStationId := "cs001"

	request := &types.SendLocalListJson{
		ListVersion: 1,
		UpdateType:  types.SendLocalListJsonUpdateTypeFull,
	}

	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, chargeStationId, request, response, nil)
	require.NoError(t, err)

	version, err := engine.GetLocalListVersion(ctx, chargeStationId)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestSendLocalListResultHandler_Failed(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}
	ctx := context.Background()

	request := &types.SendLocalListJson{
		ListVersion: 1,
		UpdateType:  types.SendLocalListJsonUpdateTypeFull,
	}

	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusFailed,
	}

	err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
	require.NoError(t, err)

	// Store should NOT be updated
	version, err := engine.GetLocalListVersion(ctx, "cs001")
	require.NoError(t, err)
	assert.Equal(t, 0, version)
}

func TestSendLocalListResultHandler_NotSupported(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}

	request := &types.SendLocalListJson{
		ListVersion: 1,
		UpdateType:  types.SendLocalListJsonUpdateTypeFull,
	}
	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusNotSupported,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)
}

func TestSendLocalListResultHandler_VersionMismatch(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}

	request := &types.SendLocalListJson{
		ListVersion: 5,
		UpdateType:  types.SendLocalListJsonUpdateTypeDifferential,
	}
	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusVersionMismatch,
	}

	err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
	require.NoError(t, err)

	// Store should NOT be updated
	version, err := engine.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 0, version)
}

func TestSendLocalListResultHandler_Accepted_EntryWithoutIdTagInfo(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp16.SendLocalListResultHandler{
		LocalAuthListStore: engine,
	}
	ctx := context.Background()

	request := &types.SendLocalListJson{
		ListVersion: 1,
		UpdateType:  types.SendLocalListJsonUpdateTypeDifferential,
		LocalAuthorizationList: []types.SendLocalListJsonLocalAuthorizationListEntry{
			{
				IdTag: "removetag",
				// No IdTagInfo means remove in differential mode
			},
		},
	}

	response := &types.SendLocalListResponseJson{
		Status: types.SendLocalListResponseJsonStatusAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
	require.NoError(t, err)
}

func TestSendLocalListResultHandler_AllResponseStatuses(t *testing.T) {
	statuses := []types.SendLocalListResponseJsonStatus{
		types.SendLocalListResponseJsonStatusAccepted,
		types.SendLocalListResponseJsonStatusFailed,
		types.SendLocalListResponseJsonStatusNotSupported,
		types.SendLocalListResponseJsonStatusVersionMismatch,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			handler := ocpp16.SendLocalListResultHandler{
				LocalAuthListStore: engine,
			}

			request := &types.SendLocalListJson{
				ListVersion: 1,
				UpdateType:  types.SendLocalListJsonUpdateTypeFull,
			}
			response := &types.SendLocalListResponseJson{
				Status: status,
			}

			err := handler.HandleCallResult(context.Background(), "cs001", request, response, nil)
			require.NoError(t, err, "status: %s", status)
		})
	}
}
