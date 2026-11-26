// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The OCPP 1.6 AuthorizationStatus values that the token store can give.
// ConcurrentTx is not one of them: it needs the active transactions of the token.
const (
	idTagStatusAccepted = "Accepted"
	idTagStatusBlocked  = "Blocked"
	idTagStatusExpired  = "Expired"
	idTagStatusInvalid  = "Invalid"
)

// idTokenMaxLength is the length of an OCPP 1.6 IdToken (CiString20Type),
// the type of idTag and parentIdTag.
const idTokenMaxLength = 20

// idTagAuthorization is the token-store verdict for an OCPP 1.6 idTag.
// Its Status, ExpiryDate and ParentIdTag are the fields of an IdTagInfo.
type idTagAuthorization struct {
	// Status is Accepted, Blocked, Expired or Invalid
	Status string
	// ExpiryDate is when the station must remove the idTag from its authorisation
	// cache (RFC 3339). It is only set when the status is Accepted.
	ExpiryDate *string
	// ParentIdTag is the group of the token. It is set whenever the token is known
	// and has a group.
	ParentIdTag *string
	// Token is the stored token, nil when the idTag is not known
	Token *store.Token
}

// authorizeIdTag looks the idTag up in the token store and returns the authorisation
// that an OCPP 1.6 station gets for it:
//   - an unknown idTag is Invalid
//   - a token that is not valid is Blocked
//   - a token whose expiry time has been reached is Expired
//   - any other token is Accepted
//
// An accepted token that must not be cached (cache mode NEVER) gets the current time
// as its expiry date, so that the station drops it from its authorisation cache at
// once. Otherwise the expiry date is the expiry time of the token, if it has one.
func authorizeIdTag(ctx context.Context, tokens store.TokenStore, clk clock.PassiveClock, idTag string) (idTagAuthorization, error) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("authorize.token", idTag))

	tok, err := tokens.LookupToken(ctx, idTag)
	if err != nil {
		return idTagAuthorization{}, err
	}
	if tok == nil {
		span.SetAttributes(attribute.String("authorize.status", idTagStatusInvalid))
		return idTagAuthorization{Status: idTagStatusInvalid}, nil
	}

	now := clk.Now()
	auth := idTagAuthorization{
		Status:      idTagStatusAccepted,
		ParentIdTag: parentIdTag(tok),
		Token:       tok,
	}
	switch {
	case !tok.Valid:
		auth.Status = idTagStatusBlocked
	case tok.ExpiresAt != nil && !now.Before(*tok.ExpiresAt):
		auth.Status = idTagStatusExpired
	case tok.CacheMode == store.CacheModeNever:
		auth.ExpiryDate = idTagExpiryDate(now)
	case tok.ExpiresAt != nil:
		auth.ExpiryDate = idTagExpiryDate(*tok.ExpiresAt)
	}

	span.SetAttributes(attribute.String("authorize.status", auth.Status))
	return auth, nil
}

// parentIdTag returns the group of the token as an OCPP 1.6 parentIdTag. A group that
// does not fit in an IdToken is left out: the station would reject the whole response.
func parentIdTag(tok *store.Token) *string {
	if tok.GroupId == nil || *tok.GroupId == "" {
		return nil
	}
	if len(*tok.GroupId) > idTokenMaxLength {
		slog.Warn("token group is too long for an OCPP 1.6 parentIdTag, leaving it out",
			slog.String("token", tok.Uid), slog.String("group", *tok.GroupId))
		return nil
	}
	parent := *tok.GroupId
	return &parent
}

// idTagExpiryDate formats the expiry date of an IdTagInfo. OCPP recommends UTC.
func idTagExpiryDate(t time.Time) *string {
	expiryDate := t.UTC().Format(time.RFC3339)
	return &expiryDate
}
