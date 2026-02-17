// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func TestSendLocalListResultHandler_AcceptedPersistsLocalAuthList(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SendLocalListResultHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()
	expiry := "2026-02-16T20:00:00Z"
	groupToken := "PARENT-1"

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.SendLocalListRequestJson{
			LocalAuthorizationList: []types.AuthorizationData{
				{
					IdToken: types.IdTokenType{
						Type:    types.IdTokenEnumTypeISO14443,
						IdToken: "ABCD1234",
					},
					IdTokenInfo: &types.IdTokenInfoType{
						Status:              types.AuthorizationStatusEnumTypeAccepted,
						CacheExpiryDateTime: &expiry,
						GroupIdToken: &types.IdTokenType{
							Type:    types.IdTokenEnumTypeISO14443,
							IdToken: groupToken,
						},
					},
				},
			},
			UpdateType:    types.UpdateEnumTypeFull,
			VersionNumber: 42,
		}
		resp := &types.SendLocalListResponseJson{
			Status: types.SendLocalListStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"send_local_list.update_type":    "Full",
		"send_local_list.version_number": 42,
		"send_local_list.status":         "Accepted",
	})

	version, err := memStore.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 42, version)

	entries, err := memStore.GetLocalAuthList(context.Background(), "cs001")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "ABCD1234", entries[0].IdTag)
	require.NotNil(t, entries[0].IdTagInfo)
	assert.Equal(t, "Accepted", entries[0].IdTagInfo.Status)
	assert.Equal(t, &expiry, entries[0].IdTagInfo.ExpiryDate)
	assert.Equal(t, &groupToken, entries[0].IdTagInfo.ParentIdTag)
}

func TestSendLocalListResultHandler_NotAcceptedDoesNotPersist(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.SendLocalListResultHandler{Store: memStore}

	ctx := context.Background()

	req := &types.SendLocalListRequestJson{
		LocalAuthorizationList: []types.AuthorizationData{
			{
				IdToken: types.IdTokenType{
					Type:    types.IdTokenEnumTypeISO14443,
					IdToken: "ABCD1234",
				},
				IdTokenInfo: &types.IdTokenInfoType{
					Status: types.AuthorizationStatusEnumTypeAccepted,
				},
			},
		},
		UpdateType:    types.UpdateEnumTypeDifferential,
		VersionNumber: 7,
	}
	resp := &types.SendLocalListResponseJson{Status: types.SendLocalListStatusEnumTypeVersionMismatch}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	require.NoError(t, err)

	version, err := memStore.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 0, version)

	entries, err := memStore.GetLocalAuthList(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Empty(t, entries)
}
