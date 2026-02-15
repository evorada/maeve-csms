// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"k8s.io/utils/clock"
)

func makeReportChargingProfilesRequest(requestId int, evseId int, source types.ChargingLimitSourceEnumType, profiles ...types.ChargingProfileType) *types.ReportChargingProfilesRequestJson {
	return &types.ReportChargingProfilesRequestJson{
		RequestId:           requestId,
		ChargingLimitSource: source,
		EvseId:              evseId,
		ChargingProfile:     profiles,
	}
}

func makeChargingProfileType(id int, purpose types.ChargingProfilePurposeEnumType, limit float64) types.ChargingProfileType {
	return types.ChargingProfileType{
		Id:                     id,
		StackLevel:             0,
		ChargingProfilePurpose: purpose,
		ChargingProfileKind:    types.ChargingProfileKindEnumTypeAbsolute,
		ChargingSchedule: []types.ChargingScheduleType{
			{
				Id:               id,
				ChargingRateUnit: types.ChargingRateUnitEnumTypeW,
				ChargingSchedulePeriod: []types.ChargingSchedulePeriodType{
					{StartPeriod: 0, Limit: limit},
				},
			},
		},
	}
}

func TestReportChargingProfilesSingleProfile(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReportChargingProfilesHandler{Store: engine}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := makeReportChargingProfilesRequest(
			10, 1, types.ChargingLimitSourceEnumTypeCSO,
			makeChargingProfileType(1, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 7400.0),
		)

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)

		got := resp.(*types.ReportChargingProfilesResponseJson)
		assert.NotNil(t, got)
	}()

	// Verify profile was stored
	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 1, profiles[0].ChargingProfileId)
	assert.Equal(t, store.ChargingProfilePurposeTxDefaultProfile, profiles[0].ChargingProfilePurpose)
	assert.Equal(t, 7400.0, profiles[0].ChargingSchedule.ChargingSchedulePeriod[0].Limit)

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"report_charging_profiles.request_id":           10,
		"report_charging_profiles.charging_limit_source": "CSO",
		"report_charging_profiles.evse_id":              1,
		"report_charging_profiles.profile_count":        1,
		"report_charging_profiles.tbc":                  false,
	})
}

func TestReportChargingProfilesMultipleProfiles(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReportChargingProfilesHandler{Store: engine}

	ctx := context.Background()

	req := makeReportChargingProfilesRequest(
		20, 0, types.ChargingLimitSourceEnumTypeEMS,
		makeChargingProfileType(10, types.ChargingProfilePurposeEnumTypeChargingStationMaxProfile, 22000.0),
		makeChargingProfileType(11, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 11000.0),
	)

	resp, err := handler.HandleCall(ctx, "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Both profiles should be stored
	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 2)

	ids := []int{profiles[0].ChargingProfileId, profiles[1].ChargingProfileId}
	assert.Contains(t, ids, 10)
	assert.Contains(t, ids, 11)
}

func TestReportChargingProfilesWithTBC(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReportChargingProfilesHandler{Store: engine}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	tbc := true

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.ReportChargingProfilesRequestJson{
			RequestId:           30,
			ChargingLimitSource: types.ChargingLimitSourceEnumTypeSO,
			EvseId:              2,
			Tbc:                 &tbc,
			ChargingProfile: []types.ChargingProfileType{
				makeChargingProfileType(5, types.ChargingProfilePurposeEnumTypeTxProfile, 16000.0),
			},
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"report_charging_profiles.request_id":           30,
		"report_charging_profiles.charging_limit_source": "SO",
		"report_charging_profiles.evse_id":              2,
		"report_charging_profiles.profile_count":        1,
		"report_charging_profiles.tbc":                  true,
	})
}

func TestReportChargingProfilesEvseIdZero(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.ReportChargingProfilesHandler{Store: engine}

	ctx := context.Background()

	req := makeReportChargingProfilesRequest(
		40, 0, types.ChargingLimitSourceEnumTypeOther,
		makeChargingProfileType(99, types.ChargingProfilePurposeEnumTypeChargingStationMaxProfile, 50000.0),
	)

	resp, err := handler.HandleCall(ctx, "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Profile for evseId=0 (whole station) should be stored
	profiles, err := engine.GetChargingProfiles(ctx, "cs001", nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, profiles, 1)
	assert.Equal(t, 99, profiles[0].ChargingProfileId)
	assert.Equal(t, 0, profiles[0].ConnectorId)
}

func TestReportChargingProfilesNoStore(t *testing.T) {
	handler := ocpp201.ReportChargingProfilesHandler{Store: nil}

	ctx := context.Background()

	req := makeReportChargingProfilesRequest(
		50, 1, types.ChargingLimitSourceEnumTypeCSO,
		makeChargingProfileType(3, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 3700.0),
	)

	resp, err := handler.HandleCall(ctx, "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestReportChargingProfilesHandlerInterface(t *testing.T) {
	handler := ocpp201.ReportChargingProfilesHandler{}

	req := makeReportChargingProfilesRequest(
		1, 1, types.ChargingLimitSourceEnumTypeCSO,
		makeChargingProfileType(1, types.ChargingProfilePurposeEnumTypeTxDefaultProfile, 7400.0),
	)

	resp, err := handler.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}
