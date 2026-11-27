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
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// AuthorizeHandler answers an Authorize.req with what the token store knows about the
// idTag in the org of the charge station. An unknown idTag is accepted when the charge
// station accepts any token (allow_any_token in its registry record).
type AuthorizeHandler struct {
	TokenStore store.TokenStore
	// RegistryStore holds allow_any_token of the charge station. When it is nil, no
	// unknown idTag is accepted.
	RegistryStore store.ChargeStationRegistryStore
	OrgResolver   tenancy.StationOrgResolver
	Clock         clock.PassiveClock
}

func (a AuthorizeHandler) HandleCall(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
	span := trace.SpanFromContext(ctx)

	req := request.(*types.AuthorizeJson)

	auth, err := authorizeIdTag(ctx, a.TokenStore, a.RegistryStore, a.OrgResolver, a.Clock, chargeStationId, req.IdTag)
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
