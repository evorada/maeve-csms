// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

const (
	// a registered station that accepts any token, and one of the same org that does not
	allowAnyStation  = "cs-any"
	tokenOnlyStation = "cs001"
)

// allowAnyToken registers the two stations in the store and sets allow_any_token on the
// first one.
func allowAnyToken(t *testing.T, engine *inmemory.Store) {
	t.Helper()
	ctx := context.Background()
	for _, stationKey := range []string{allowAnyStation, tokenOnlyStation} {
		require.NoError(t, engine.SetChargeStationAuth(ctx, stationKey, &store.ChargeStationAuth{}))
	}
	record, err := engine.LookupChargeStation(ctx, allowAnyStation)
	require.NoError(t, err)
	record.AllowAnyToken = true
	require.NoError(t, engine.UpdateChargeStation(ctx, record))
}

// registryLookupStub answers LookupChargeStation with err.
type registryLookupStub struct {
	store.ChargeStationRegistryStore
	err error
}

func (r registryLookupStub) LookupChargeStation(context.Context, string) (*store.ChargeStation, error) {
	return nil, r.err
}

func TestAuthorizeWithAllowAnyToken(t *testing.T) {
	expired := rfidToken("EXPIREDCARD")
	expiredAt := time.Date(2023, 6, 15, 15, 4, 59, 0, time.UTC) // just before the clock of the handler
	expired.ExpiresAt = &expiredAt
	blocked := rfidToken("BLOCKEDCARD")
	blocked.Valid = false
	ah := newAuthorizeHandler(t, rfidToken("MYRFIDCARD"), expired, blocked)
	allowAnyToken(t, ah.TokenStore.(*inmemory.Store))

	authorizeAt := func(stationKey, idTag string) *types.AuthorizeResponseJson {
		t.Helper()
		resp, err := ah.HandleCall(context.Background(), stationKey, &types.AuthorizeJson{IdTag: idTag})
		require.NoError(t, err)
		return resp.(*types.AuthorizeResponseJson)
	}

	t.Run("an unknown token is accepted", func(t *testing.T) {
		assert.Equal(t, &types.AuthorizeResponseJson{
			IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{Status: types.AuthorizeResponseJsonIdTagInfoStatusAccepted},
		}, authorizeAt(allowAnyStation, "UNKNOWNCARD"))
	})

	t.Run("only at that station", func(t *testing.T) {
		for _, stationKey := range []string{tokenOnlyStation, "cs-not-registered"} {
			assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, authorizeAt(stationKey, "UNKNOWNCARD").IdTagInfo.Status, stationKey)
		}
	})

	t.Run("a known token that is not accepted is still rejected", func(t *testing.T) {
		assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusBlocked, authorizeAt(allowAnyStation, "BLOCKEDCARD").IdTagInfo.Status)
		assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusExpired, authorizeAt(allowAnyStation, "EXPIREDCARD").IdTagInfo.Status)
	})

	t.Run("a known token is answered as without it", func(t *testing.T) {
		assert.Equal(t, authorizeAt(tokenOnlyStation, "MYRFIDCARD"), authorizeAt(allowAnyStation, "MYRFIDCARD"))
	})
}

func TestAuthorizeRecordsAllowAnyTokenInTheSpan(t *testing.T) {
	ah := newAuthorizeHandler(t)
	allowAnyToken(t, ah.TokenStore.(*inmemory.Store))
	tracer, exporter := testutil.GetTracer()

	for _, stationKey := range []string{allowAnyStation, tokenOnlyStation} {
		ctx, span := tracer.Start(context.Background(), stationKey)
		_, err := ah.HandleCall(ctx, stationKey, &types.AuthorizeJson{IdTag: "UNKNOWNCARD"})
		span.End()
		require.NoError(t, err)
	}

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	testutil.AssertSpan(t, &spans[0], allowAnyStation, map[string]any{
		"authorize.token":           "UNKNOWNCARD",
		"authorize.allow_any_token": true,
		"authorize.status":          "Accepted",
		"request.status":            "Accepted",
	})
	// the span of a station without it is as before there was allow_any_token
	testutil.AssertSpan(t, &spans[1], tokenOnlyStation, map[string]any{
		"authorize.token":  "UNKNOWNCARD",
		"authorize.status": "Invalid",
		"request.status":   "Invalid",
	})
}

// A backend without a registry (Firestore) and a handler without one accept the tokens
// of the token store only, as before there was allow_any_token.
func TestAuthorizeWithoutRegistry(t *testing.T) {
	for name, registry := range map[string]store.ChargeStationRegistryStore{
		"backend without registry": registryLookupStub{err: fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented)},
		"no registry store":        nil,
	} {
		t.Run(name, func(t *testing.T) {
			ah := newAuthorizeHandler(t, rfidToken("MYRFIDCARD"))
			ah.RegistryStore = registry

			assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, authorize(t, ah, "UNKNOWNCARD").IdTagInfo.Status)
			assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, authorize(t, ah, "MYRFIDCARD").IdTagInfo.Status)
		})
	}
}

func TestAuthorizeReturnsRegistryError(t *testing.T) {
	registryErr := errors.New("registry is down")
	ah := newAuthorizeHandler(t, rfidToken("MYRFIDCARD"))
	ah.RegistryStore = registryLookupStub{err: registryErr}

	resp, err := ah.HandleCall(context.Background(), "cs001", &types.AuthorizeJson{IdTag: "UNKNOWNCARD"})
	assert.ErrorIs(t, err, registryErr)
	assert.Nil(t, resp)

	// the state of the station is read for every idTag: a known one fails the same way
	resp, err = ah.HandleCall(context.Background(), "cs001", &types.AuthorizeJson{IdTag: "MYRFIDCARD"})
	assert.ErrorIs(t, err, registryErr)
	assert.Nil(t, resp)
}

func TestStartTransactionWithAllowAnyToken(t *testing.T) {
	handler, engine := newStartTransactionHandler(t)
	allowAnyToken(t, engine)
	ctx := context.Background()

	accepted := startTransaction(t, handler, allowAnyStation, startTransactionRequest("UNKNOWNCARD"))
	invalid := startTransaction(t, handler, tokenOnlyStation, startTransactionRequest("UNKNOWNCARD"))

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfo{Status: types.StartTransactionResponseJsonIdTagInfoStatusAccepted}, accepted.IdTagInfo)
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfo{Status: types.StartTransactionResponseJsonIdTagInfoStatusInvalid}, invalid.IdTagInfo)
	found, err := engine.FindTransaction(ctx, allowAnyStation, handlers.ConvertToUUID(accepted.TransactionId))
	require.NoError(t, err)
	require.NotNil(t, found, "the transaction is recorded")
	assert.Equal(t, "UNKNOWNCARD", found.IdToken)
	assert.Nil(t, found.IdTokenGroup, "an unknown idTag has no group")

	// a known token that is not accepted is still rejected
	tok, err := engine.LookupToken(ctx, startTransactionIdTag)
	require.NoError(t, err)
	tok.Valid = false
	require.NoError(t, engine.SetToken(ctx, tok))
	blocked := startTransaction(t, handler, allowAnyStation, startTransactionRequest(startTransactionIdTag))
	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusBlocked, blocked.IdTagInfo.Status)
}

func TestStartTransactionWithoutRegistry(t *testing.T) {
	handler, _ := newStartTransactionHandler(t)
	handler.RegistryStore = registryLookupStub{err: fmt.Errorf("LookupChargeStation: %w", store.ErrNotImplemented)}

	got := startTransaction(t, handler, startTransactionChargeStationId, startTransactionRequest("UNKNOWNCARD"))

	assert.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusInvalid, got.IdTagInfo.Status)
}

// The station updates its authorisation cache from the idTagInfo of a
// StopTransaction.conf: an idTag accepted through allow_any_token stays accepted there.
func TestStopTransactionWithAllowAnyToken(t *testing.T) {
	handler, engine := newStopTransactionHandler(t)
	allowAnyToken(t, engine)

	statusAt := func(stationKey string) types.StopTransactionResponseJsonIdTagInfoStatus {
		t.Helper()
		idTag := "UNKNOWNCARD"
		req := stopTransactionRequest(4711)
		req.IdTag = &idTag
		resp, err := handler.HandleCall(context.Background(), stationKey, req)
		require.NoError(t, err)
		got := resp.(*types.StopTransactionResponseJson)
		require.NotNil(t, got.IdTagInfo)
		return got.IdTagInfo.Status
	}

	assert.Equal(t, types.StopTransactionResponseJsonIdTagInfoStatusAccepted, statusAt(allowAnyStation))
	assert.Equal(t, types.StopTransactionResponseJsonIdTagInfoStatusInvalid, statusAt(tokenOnlyStation))
}
