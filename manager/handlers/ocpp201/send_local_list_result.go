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

type SendLocalListResultHandler struct {
	Store store.LocalAuthListStore
}

func (h SendLocalListResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.SendLocalListRequestJson)
	resp := response.(*types.SendLocalListResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("send_local_list.update_type", string(req.UpdateType)),
		attribute.Int("send_local_list.version_number", req.VersionNumber),
		attribute.String("send_local_list.status", string(resp.Status)))

	if resp.Status != types.SendLocalListStatusEnumTypeAccepted {
		return nil
	}

	return h.Store.UpdateLocalAuthList(ctx, chargeStationId, req.VersionNumber, string(req.UpdateType), toStoreLocalAuthListEntries(req.LocalAuthorizationList))
}

func toStoreLocalAuthListEntries(localAuthList []types.AuthorizationData) []*store.LocalAuthListEntry {
	entries := make([]*store.LocalAuthListEntry, 0, len(localAuthList))
	for _, authorizationData := range localAuthList {
		entry := &store.LocalAuthListEntry{IdTag: authorizationData.IdToken.IdToken}
		if authorizationData.IdTokenInfo != nil {
			entry.IdTagInfo = &store.IdTagInfo{
				Status:      string(authorizationData.IdTokenInfo.Status),
				ExpiryDate:  authorizationData.IdTokenInfo.CacheExpiryDateTime,
				ParentIdTag: nil,
			}
			if authorizationData.IdTokenInfo.GroupIdToken != nil {
				entry.IdTagInfo.ParentIdTag = &authorizationData.IdTokenInfo.GroupIdToken.IdToken
			}
		}
		entries = append(entries, entry)
	}

	return entries
}
