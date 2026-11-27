// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// the time of the handler's clock in the Authorize tests
const authorizeClockTime = "2023-06-15T15:05:00Z"

// newAuthorizeHandler returns a handler whose token store has the given tokens.
func newAuthorizeHandler(t *testing.T, tokens ...*store.Token) handlers.AuthorizeHandler {
	t.Helper()

	now, err := time.Parse(time.RFC3339, authorizeClockTime)
	require.NoError(t, err)
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)
	for _, tok := range tokens {
		require.NoError(t, engine.SetToken(context.Background(), tok))
	}

	return handlers.AuthorizeHandler{
		TokenStore:  engine,
		OrgResolver: &tenancy.Resolver{Stations: engine},
		Clock:       clk,
	}
}

// rfidToken is a valid token that the station may cache and that does not expire.
func rfidToken(uid string) *store.Token {
	return &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         uid,
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   store.CacheModeAlways,
	}
}

func authorize(t *testing.T, handler handlers.AuthorizeHandler, idTag string) *types.AuthorizeResponseJson {
	t.Helper()

	resp, err := handler.HandleCall(context.Background(), "cs001", &types.AuthorizeJson{IdTag: idTag})
	require.NoError(t, err)
	got, ok := resp.(*types.AuthorizeResponseJson)
	require.True(t, ok, "unexpected response type %T", resp)
	return got
}

func TestAuthorizeKnownRfidCard(t *testing.T) {
	ah := newAuthorizeHandler(t, rfidToken("MYRFIDCARD"))

	got := authorize(t, ah, "MYRFIDCARD")

	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status: types.AuthorizeResponseJsonIdTagInfoStatusAccepted,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeWithUnknownRfidCard(t *testing.T) {
	ah := newAuthorizeHandler(t)

	got := authorize(t, ah, "MYBADRFID")

	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status: types.AuthorizeResponseJsonIdTagInfoStatusInvalid,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeBlockedRfidCard(t *testing.T) {
	tok := rfidToken("MYRFIDCARD")
	tok.Valid = false
	ah := newAuthorizeHandler(t, tok)

	got := authorize(t, ah, "MYRFIDCARD")

	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status: types.AuthorizeResponseJsonIdTagInfoStatusBlocked,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeExpiredRfidCard(t *testing.T) {
	expiresAt := time.Date(2023, 6, 15, 15, 4, 59, 0, time.UTC)
	tok := rfidToken("MYRFIDCARD")
	tok.ExpiresAt = &expiresAt
	ah := newAuthorizeHandler(t, tok)

	got := authorize(t, ah, "MYRFIDCARD")

	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status: types.AuthorizeResponseJsonIdTagInfoStatusExpired,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeReturnsTheGroupAsParentIdTag(t *testing.T) {
	groupId := "MYGROUP"
	accepted := rfidToken("MYRFIDCARD")
	accepted.GroupId = &groupId
	blocked := rfidToken("BLOCKEDCARD")
	blocked.GroupId = &groupId
	blocked.Valid = false
	ah := newAuthorizeHandler(t, accepted, blocked)

	got := authorize(t, ah, "MYRFIDCARD")
	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status:      types.AuthorizeResponseJsonIdTagInfoStatusAccepted,
			ParentIdTag: &groupId,
		},
	}
	assert.Equal(t, want, got)

	// the group is returned whenever the token is known
	got = authorize(t, ah, "BLOCKEDCARD")
	want = &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status:      types.AuthorizeResponseJsonIdTagInfoStatusBlocked,
			ParentIdTag: &groupId,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeReturnsTheExpiryDateOfTheAuthorizationCache(t *testing.T) {
	expiresAt := time.Date(2023, 12, 31, 23, 0, 0, 0, time.UTC)
	expiring := rfidToken("EXPIRINGCARD")
	expiring.ExpiresAt = &expiresAt
	neverCached := rfidToken("NEVERCACHED")
	neverCached.CacheMode = store.CacheModeNever
	neverCached.ExpiresAt = &expiresAt
	ah := newAuthorizeHandler(t, expiring, neverCached)

	// the station drops the idTag from its cache when the token expires
	got := authorize(t, ah, "EXPIRINGCARD")
	expiryDate := "2023-12-31T23:00:00Z"
	want := &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status:     types.AuthorizeResponseJsonIdTagInfoStatusAccepted,
			ExpiryDate: &expiryDate,
		},
	}
	assert.Equal(t, want, got)

	// a token that must not be cached expires in the cache at once
	got = authorize(t, ah, "NEVERCACHED")
	now := authorizeClockTime
	want = &types.AuthorizeResponseJson{
		IdTagInfo: types.AuthorizeResponseJsonIdTagInfo{
			Status:     types.AuthorizeResponseJsonIdTagInfoStatusAccepted,
			ExpiryDate: &now,
		},
	}
	assert.Equal(t, want, got)
}

func TestAuthorizeReturnsStoreError(t *testing.T) {
	lookupErr := errors.New("token store is down")
	ah := handlers.AuthorizeHandler{
		TokenStore:  failingTokenLookupStore{err: lookupErr},
		OrgResolver: &tenancy.Resolver{Stations: inmemory.NewStore(clockTest.NewFakePassiveClock(time.Now()))},
		Clock:       clockTest.NewFakePassiveClock(time.Now()),
	}

	resp, err := ah.HandleCall(context.Background(), "cs001", &types.AuthorizeJson{IdTag: "MYRFIDCARD"})

	assert.ErrorIs(t, err, lookupErr)
	assert.Nil(t, resp)
}

// failingTokenLookupStore fails every lookup of a token.
type failingTokenLookupStore struct {
	store.TokenStore
	err error
}

func (s failingTokenLookupStore) LookupTokenForOrg(context.Context, string, string) (*store.Token, error) {
	return nil, s.err
}

// A token belongs to an org: a charge station is only authorised with the tokens of
// the org that owns it.
func TestAuthorizeOnlyKnowsTheTokensOfTheOrgOfTheStation(t *testing.T) {
	const (
		orgA = "0193b2c4-0000-7000-8000-0000000000a1"
		orgB = "0193b2c4-0000-7000-8000-0000000000b2"
	)
	onlyOrgB := rfidToken("ONLYORGB")
	onlyOrgB.OrgId = orgB
	// both orgs have a token with this UID: it is valid in org A and blocked in org B
	validInOrgA := rfidToken("BOTHORGS")
	validInOrgA.OrgId = orgA
	blockedInOrgB := rfidToken("BOTHORGS")
	blockedInOrgB.OrgId = orgB
	blockedInOrgB.Valid = false

	ah := newAuthorizeHandler(t, onlyOrgB, validInOrgA, blockedInOrgB)
	stations := ah.TokenStore.(*inmemory.Store)
	ctx := context.Background()
	require.NoError(t, stations.SetChargeStationAuth(ctx, "cs-org-a", &store.ChargeStationAuth{OrgId: orgA}))
	require.NoError(t, stations.SetChargeStationAuth(ctx, "cs-org-b", &store.ChargeStationAuth{OrgId: orgB}))

	status := func(chargeStationId, idTag string) types.AuthorizeResponseJsonIdTagInfoStatus {
		t.Helper()
		resp, err := ah.HandleCall(ctx, chargeStationId, &types.AuthorizeJson{IdTag: idTag})
		require.NoError(t, err)
		return resp.(*types.AuthorizeResponseJson).IdTagInfo.Status
	}

	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, status("cs-org-a", "ONLYORGB"))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, status("cs-org-b", "ONLYORGB"))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, status("cs-org-a", "BOTHORGS"))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusBlocked, status("cs-org-b", "BOTHORGS"))
	// a charge station that is not registered belongs to the default org
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, status("cs-unknown", "ONLYORGB"))
	assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusInvalid, status("cs-unknown", "BOTHORGS"))
}

// failingOrgResolver fails every lookup of the org of a charge station.
type failingOrgResolver struct {
	err error
}

func (r failingOrgResolver) OrgIdForStation(context.Context, string) (string, error) {
	return "", r.err
}

func TestAuthorizeReturnsOrgResolverError(t *testing.T) {
	resolveErr := errors.New("charge station store is down")
	ah := newAuthorizeHandler(t, rfidToken("MYRFIDCARD"))
	ah.OrgResolver = failingOrgResolver{err: resolveErr}

	resp, err := ah.HandleCall(context.Background(), "cs001", &types.AuthorizeJson{IdTag: "MYRFIDCARD"})

	assert.ErrorIs(t, err, resolveErr)
	assert.Nil(t, resp)
}

// The status of a tenant only stops what is created or changed through the API: the
// charge stations of a suspended tenant authorize and charge like any other.
func TestStationOfASuspendedTenantStillAuthorizesAndCharges(t *testing.T) {
	const orgA = "0193b2c4-0000-7000-8000-0000000000a1"
	ctx := context.Background()
	now, err := time.Parse(time.RFC3339, authorizeClockTime)
	require.NoError(t, err)
	clk := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(clk)
	require.NoError(t, engine.PutTenant(ctx, &store.Tenant{
		OrgId: orgA, Path: store.OrgPathLabel(orgA), Slug: "tenant-a", OrgKey: "aaaa", Hostnames: []string{"a.example"},
	}))
	token := rfidToken("MYRFIDCARD")
	token.OrgId = orgA
	require.NoError(t, engine.SetToken(ctx, token))
	// the first charge station is registered, the second is only known by the org key
	// that its station key starts with
	stations := []string{"aaaa.cs001", "aaaa.cs002"}
	require.NoError(t, engine.SetChargeStationAuth(ctx, stations[0], &store.ChargeStationAuth{OrgId: orgA}))
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine}
	idTag := "MYRFIDCARD"

	for _, status := range []store.TenantStatus{store.TenantStatusSuspended, store.TenantStatusCancelled} {
		found, err := engine.SetTenantStatus(ctx, orgA, status)
		require.NoError(t, err)
		require.True(t, found)
		resolver.Invalidate(orgA)

		for _, chargeStationId := range stations {
			name := string(status) + " " + chargeStationId

			resp, err := handlers.AuthorizeHandler{TokenStore: engine, OrgResolver: resolver, Clock: clk}.
				HandleCall(ctx, chargeStationId, &types.AuthorizeJson{IdTag: idTag})
			require.NoError(t, err, name)
			assert.Equal(t, types.AuthorizeResponseJsonIdTagInfoStatusAccepted, resp.(*types.AuthorizeResponseJson).IdTagInfo.Status, name)

			started := startTransaction(t, handlers.StartTransactionHandler{
				Clock: clk, TokenStore: engine, OrgResolver: resolver, TransactionStore: engine, ReservationStore: engine,
			}, chargeStationId, startTransactionRequest(idTag))
			require.Equal(t, types.StartTransactionResponseJsonIdTagInfoStatusAccepted, started.IdTagInfo.Status, name)

			_, err = handlers.MeterValuesHandler{TransactionStore: engine, MeterValuesStore: engine}.
				HandleCall(ctx, chargeStationId, &types.MeterValuesJson{
					ConnectorId:   2,
					TransactionId: &started.TransactionId,
					MeterValue: []types.MeterValuesJsonMeterValueElem{{
						Timestamp: authorizeClockTime,
						SampledValue: []types.MeterValuesJsonMeterValueElemSampledValueElem{
							meterValuesSample("150", types.MeterValuesJsonMeterValueElemSampledValueElemMeasurandEnergyActiveImportRegister, types.MeterValuesJsonMeterValueElemSampledValueElemUnitWh),
						},
					}},
				})
			require.NoError(t, err, name)

			resp, err = handlers.StopTransactionHandler{Clock: clk, TokenStore: engine, OrgResolver: resolver, TransactionStore: engine}.
				HandleCall(ctx, chargeStationId, &types.StopTransactionJson{
					IdTag: &idTag, MeterStop: 200, Timestamp: authorizeClockTime, TransactionId: started.TransactionId,
				})
			require.NoError(t, err, name)
			stopped := resp.(*types.StopTransactionResponseJson)
			require.NotNil(t, stopped.IdTagInfo, name)
			assert.Equal(t, types.StopTransactionResponseJsonIdTagInfoStatusAccepted, stopped.IdTagInfo.Status, name)
		}
	}
}
