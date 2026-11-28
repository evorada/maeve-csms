// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/codes"
	"k8s.io/utils/clock"
)

func TestUnlockConnectorResult(t *testing.T) {
	handler := ocpp201.UnlockConnectorResultHandler{}

	tracer, exporter := testutil.GetTracer()

	ctx := context.TODO()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.UnlockConnectorRequestJson{
			EvseId:      1,
			ConnectorId: 2,
		}

		resp := &types.UnlockConnectorResponseJson{
			Status: types.UnlockStatusEnumTypeUnlocked,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"unlock_connector.evse_id":      1,
		"unlock_connector.connector_id": 2,
		"unlock_connector.status":       "Unlocked",
	})
}

// TestUnlockConnectorResultIsRouted sends an UnlockConnector call result through the
// router: the response must pass schema validation and reach the result handler.
func TestUnlockConnectorResultIsRouted(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})

	router := ocpp201.NewRouter(&fakeEmitter{},
		clock.RealClock{},
		engine,
		&tenancy.Resolver{Stations: engine},
		nil,
		&fakeTariffService{},
		&fakeCertValidationService{},
		&fakeChargeStationCertProvider{},
		&fakeContractCertProvider{},
		5*time.Minute,
		schemas.OcppSchemas,
		nil,
		nil,
		nil,
	)

	reqBytes, err := json.Marshal(&types.UnlockConnectorRequestJson{
		EvseId:      1,
		ConnectorId: 2,
	})
	require.NoError(t, err)

	respBytes, err := json.Marshal(&types.UnlockConnectorResponseJson{
		Status: types.UnlockStatusEnumTypeUnlocked,
	})
	require.NoError(t, err)

	msg := transport.Message{
		MessageType:     transport.MessageTypeCallResult,
		Action:          "UnlockConnector",
		MessageId:       "a7caa7a1-c309-43ad-a4a5-6a1e8a2b5f2c",
		RequestPayload:  reqBytes,
		ResponsePayload: respBytes,
	}

	tracer, exporter := testutil.GetTracer()

	func() {
		ctx, span := tracer.Start(context.Background(), "test")
		defer span.End()
		router.Handle(ctx, "cs001", &msg)
	}()

	// no routing error
	require.Greater(t, len(exporter.GetSpans()), 0)
	assert.Equal(t, codes.Ok, exporter.GetSpans()[0].Status.Code)
	assert.Empty(t, exporter.GetSpans()[0].Events)

	// the attributes are set by the handler, so it was reached
	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"unlock_connector.evse_id":      1,
		"unlock_connector.connector_id": 2,
		"unlock_connector.status":       "Unlocked",
	})
}
