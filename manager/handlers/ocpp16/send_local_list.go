// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type SendLocalListResultHandler struct {
	LocalAuthListStore store.LocalAuthListStore
}

func (h SendLocalListResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*ocpp16.SendLocalListJson)
	resp := response.(*ocpp16.SendLocalListResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.Int("local_auth_list.version", req.ListVersion),
		attribute.String("local_auth_list.update_type", string(req.UpdateType)),
		attribute.String("local_auth_list.status", string(resp.Status)))

	if len(req.LocalAuthorizationList) > 0 {
		span.SetAttributes(attribute.Int("local_auth_list.entry_count", len(req.LocalAuthorizationList)))
	}

	if resp.Status == ocpp16.SendLocalListResponseJsonStatusAccepted {
		slog.Info("send local list accepted",
			"chargeStationId", chargeStationId,
			"listVersion", req.ListVersion,
			"updateType", req.UpdateType,
			"entryCount", len(req.LocalAuthorizationList))

		// Update the store to reflect the accepted list
		entries := make([]*store.LocalAuthListEntry, len(req.LocalAuthorizationList))
		for i, e := range req.LocalAuthorizationList {
			entry := &store.LocalAuthListEntry{
				IdTag: e.IdTag,
			}
			if e.IdTagInfo != nil {
				entry.IdTagInfo = &store.IdTagInfo{
					Status:      string(e.IdTagInfo.Status),
					ExpiryDate:  e.IdTagInfo.ExpiryDate,
					ParentIdTag: e.IdTagInfo.ParentIdTag,
				}
			}
			entries[i] = entry
		}

		updateType := store.LocalAuthListUpdateTypeFull
		if req.UpdateType == ocpp16.SendLocalListJsonUpdateTypeDifferential {
			updateType = store.LocalAuthListUpdateTypeDifferential
		}

		if err := h.LocalAuthListStore.UpdateLocalAuthList(ctx, chargeStationId, req.ListVersion, updateType, entries); err != nil {
			slog.Error("failed to update local auth list in store",
				"chargeStationId", chargeStationId,
				"error", err)
			return err
		}
	} else {
		slog.Warn("send local list not accepted",
			"chargeStationId", chargeStationId,
			"listVersion", req.ListVersion,
			"updateType", req.UpdateType,
			"status", resp.Status)
	}

	return nil
}
