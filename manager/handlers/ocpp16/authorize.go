// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// AuthorizeHandler answers an Authorize.req with what the token store knows about the idTag.
type AuthorizeHandler struct {
	TokenStore store.TokenStore
	Clock      clock.PassiveClock
}

func (a AuthorizeHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.AuthorizeJson)

	auth, err := authorizeIdTag(ctx, a.TokenStore, a.Clock, req.IdTag)
	if err != nil {
		return nil, err
	}

	span.SetAttributes(attribute.String("request.status", auth.Status))

	return &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status:      types.AuthorizeResponseJsonIdTagInfoStatus(auth.Status),
			ExpiryDate:  auth.ExpiryDate,
			ParentIdTag: auth.ParentIdTag,
		},
	}, nil
}
