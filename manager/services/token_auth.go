// SPDX-License-Identifier: Apache-2.0

package services

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/utils/clock"
)

type TokenAuthService interface {
	// Authorize gives the authorisation of the token at the charge station.
	Authorize(ctx context.Context, chargeStationId string, token ocpp201.IdTokenType) ocpp201.IdTokenInfoType
}

// OcppTokenAuthService authorises a token with the tokens of the org that owns the
// charge station: a token that only another org has is Unknown.
type OcppTokenAuthService struct {
	TokenStore  store.TokenStore
	OrgResolver tenancy.StationOrgResolver
	Clock       clock.PassiveClock
}

func (o *OcppTokenAuthService) Authorize(ctx context.Context, chargeStationId string, token ocpp201.IdTokenType) ocpp201.IdTokenInfoType {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("token_auth.id", token.IdToken),
		attribute.String("token_auth.type", string(token.Type)))

	var tokenInfo *ocpp201.IdTokenInfoType

	switch token.Type {
	case ocpp201.IdTokenEnumTypeNoAuthorization:
		tokenInfo = &ocpp201.IdTokenInfoType{
			Status: ocpp201.AuthorizationStatusEnumTypeAccepted,
		}
	case ocpp201.IdTokenEnumTypeCentral:
		tokenInfo = &ocpp201.IdTokenInfoType{
			Status: ocpp201.AuthorizationStatusEnumTypeAccepted,
		}
	case ocpp201.IdTokenEnumTypeLocal:
		// local auth must be implemented in a different TokenAuthService
		tokenInfo = &ocpp201.IdTokenInfoType{
			Status: ocpp201.AuthorizationStatusEnumTypeInvalid,
		}
	default:
		var foundToken *store.Token
		orgId, err := o.OrgResolver.OrgIdForStation(ctx, chargeStationId)
		if err == nil {
			foundToken, err = o.TokenStore.LookupTokenForOrg(ctx, orgId, token.IdToken)
		}
		if err != nil {
			span.RecordError(err)
			tokenInfo = &ocpp201.IdTokenInfoType{
				Status: ocpp201.AuthorizationStatusEnumTypeUnknown,
			}
		} else if foundToken == nil {
			tokenInfo = &ocpp201.IdTokenInfoType{
				Status: ocpp201.AuthorizationStatusEnumTypeUnknown,
			}
		} else {
			status := ocpp201.AuthorizationStatusEnumTypeInvalid
			now := o.Clock.Now()

			if foundToken.Valid {
				status = ocpp201.AuthorizationStatusEnumTypeAccepted
				// the token is no longer accepted once its expiry time is reached
				if foundToken.ExpiresAt != nil && !now.Before(*foundToken.ExpiresAt) {
					status = ocpp201.AuthorizationStatusEnumTypeExpired
				}
			}

			// if the cache mode is never, prevent the charge station
			// from caching the token by setting its expiry time to now
			var cacheExpiryTime *string
			if foundToken.CacheMode == "NEVER" {
				expiryTime := now.Format(time.RFC3339)
				cacheExpiryTime = &expiryTime
			} else if status == ocpp201.AuthorizationStatusEnumTypeAccepted && foundToken.ExpiresAt != nil {
				// an accepted token must leave the authorisation cache when it expires
				expiryTime := foundToken.ExpiresAt.UTC().Format(time.RFC3339)
				cacheExpiryTime = &expiryTime
			}

			var groupIdToken *ocpp201.IdTokenType
			if foundToken.GroupId != nil {
				groupIdToken = &ocpp201.IdTokenType{
					Type:    ocpp201.IdTokenEnumTypeCentral,
					IdToken: *foundToken.GroupId,
				}
				span.SetAttributes(attribute.String("token_auth.group_id", *foundToken.GroupId))
			}

			tokenInfo = &ocpp201.IdTokenInfoType{
				Status:              status,
				GroupIdToken:        groupIdToken,
				CacheExpiryDateTime: cacheExpiryTime,
			}
		}
	}

	span.SetAttributes(
		attribute.String("token_auth.status", string(tokenInfo.Status)))
	return *tokenInfo
}
