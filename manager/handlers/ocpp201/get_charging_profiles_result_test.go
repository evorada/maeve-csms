// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
)

func makeGetChargingProfilesRequest(requestId int, evseId *int, purpose *types.ChargingProfilePurposeEnumType) *types.GetChargingProfilesRequestJson {
	return &types.GetChargingProfilesRequestJson{
		RequestId:       requestId,
		EvseId:          evseId,
		ChargingProfile: types.ChargingProfileCriterionType{},
	}
}

func TestGetChargingProfilesResultHandler_Accepted(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetChargingProfilesResultHandler{}

	req := makeGetChargingProfilesRequest(42, nil, nil)
	resp := &types.GetChargingProfilesResponseJson{
		Status: types.GetChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetChargingProfilesResultHandler_NoProfiles(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetChargingProfilesResultHandler{}

	req := makeGetChargingProfilesRequest(43, nil, nil)
	resp := &types.GetChargingProfilesResponseJson{
		Status: types.GetChargingProfileStatusEnumTypeNoProfiles,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetChargingProfilesResultHandler_WithEvseIdFilter(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetChargingProfilesResultHandler{}

	evseId := 1
	purpose := types.ChargingProfilePurposeEnumTypeTxDefaultProfile
	req := makeGetChargingProfilesRequest(44, &evseId, &purpose)
	resp := &types.GetChargingProfilesResponseJson{
		Status: types.GetChargingProfileStatusEnumTypeAccepted,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}

func TestGetChargingProfilesResultHandler_WithProfileIdFilter(t *testing.T) {
	ctx := context.Background()
	handler := ocpp201.GetChargingProfilesResultHandler{}

	req := &types.GetChargingProfilesRequestJson{
		RequestId: 45,
		ChargingProfile: types.ChargingProfileCriterionType{
			ChargingProfileId: []int{1, 2, 3},
		},
	}
	resp := &types.GetChargingProfilesResponseJson{
		Status: types.GetChargingProfileStatusEnumTypeNoProfiles,
	}

	err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
	assert.NoError(t, err)
}
