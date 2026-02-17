// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetLocalListVersionResultHandler struct {
	Store store.LocalAuthListStore
}

func (h GetLocalListVersionResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	resp := response.(*types.GetLocalListVersionResponseJson)

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("get_local_list_version.version_number", resp.VersionNumber))

	if h.Store != nil {
		if err := h.Store.UpdateLocalAuthList(ctx, chargeStationId, resp.VersionNumber, store.LocalAuthListUpdateTypeDifferential, nil); err != nil {
			return err
		}
	}

	return nil
}
