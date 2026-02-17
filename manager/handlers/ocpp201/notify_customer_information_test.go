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

func TestNotifyCustomerInformationHandler(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyCustomerInformationHandler{Store: memStore}

	tracer, exporter := testutil.GetTracer()
	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, "test")
		defer span.End()

		req := &types.NotifyCustomerInformationRequestJson{
			RequestId:   55,
			SeqNo:       0,
			GeneratedAt: "2026-02-16T18:20:00Z",
			Data:        "Customer profile fragment",
		}

		resp, err := handler.HandleCall(ctx, "cs001", req)
		require.NoError(t, err)
		assert.Equal(t, &types.NotifyCustomerInformationResponseJson{}, resp)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"notify_customer_information.request_id":   55,
		"notify_customer_information.seq_no":       0,
		"notify_customer_information.tbc":          false,
		"notify_customer_information.generated_at": "2026-02-16T18:20:00Z",
		"notify_customer_information.data_length":  25,
	})

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	stored := settings.Settings["ocpp201.customer_information.55.0"]
	require.NotNil(t, stored)
	assert.Equal(t, store.ChargeStationSettingStatusAccepted, stored.Status)
	assert.Contains(t, stored.Value, "Customer profile fragment")
	assert.Contains(t, stored.Value, "2026-02-16T18:20:00Z")
}

func TestNotifyCustomerInformationHandlerStoresFragmentsBySeqNo(t *testing.T) {
	memStore := inmemory.NewStore(clock.RealClock{})
	handler := ocpp201.NotifyCustomerInformationHandler{Store: memStore}
	ctx := context.Background()

	_, err := handler.HandleCall(ctx, "cs001", &types.NotifyCustomerInformationRequestJson{
		RequestId:   8,
		SeqNo:       0,
		GeneratedAt: "2026-02-16T18:21:00Z",
		Data:        "fragment-0",
		Tbc:         true,
	})
	require.NoError(t, err)

	_, err = handler.HandleCall(ctx, "cs001", &types.NotifyCustomerInformationRequestJson{
		RequestId:   8,
		SeqNo:       1,
		GeneratedAt: "2026-02-16T18:21:01Z",
		Data:        "fragment-1",
		Tbc:         false,
	})
	require.NoError(t, err)

	settings, err := memStore.LookupChargeStationSettings(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, settings)

	require.NotNil(t, settings.Settings["ocpp201.customer_information.8.0"])
	require.NotNil(t, settings.Settings["ocpp201.customer_information.8.1"])
}
