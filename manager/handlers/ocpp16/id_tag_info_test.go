// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

// failingTokenStore fails every lookup.
type failingTokenStore struct {
	store.TokenStore
	err error
}

func (s failingTokenStore) LookupToken(context.Context, string) (*store.Token, error) {
	return nil, s.err
}

func TestAuthorizeIdTag(t *testing.T) {
	// the clock is not in UTC and has a fraction of a second: expiry dates are
	// sent in UTC, without the fraction
	now := time.Date(2026, 11, 27, 13, 30, 0, 250_000_000, time.FixedZone("CET", 3600))
	nowExpiryDate := "2026-11-27T12:30:00Z"
	past := now.Add(-time.Second)
	future := time.Date(2027, 1, 31, 8, 0, 0, 0, time.FixedZone("CET", 3600))
	futureExpiryDate := "2027-01-31T07:00:00Z"

	group := "FLEET1"
	longGroup := "GROUP-OF-MORE-THAN-20-CHARACTERS"
	emptyGroup := ""
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name string
		// token is stored with the uid "TAG1"; nil stores no token
		token           *store.Token
		wantStatus      string
		wantExpiryDate  *string
		wantParentIdTag *string
	}{
		{
			name:       "unknown idTag is Invalid",
			token:      nil,
			wantStatus: "Invalid",
		},
		{
			name:       "valid token without group is Accepted",
			token:      &store.Token{Valid: true, CacheMode: store.CacheModeAlways},
			wantStatus: "Accepted",
		},
		{
			name:            "valid token has its group as parentIdTag",
			token:           &store.Token{Valid: true, CacheMode: store.CacheModeAlways, GroupId: &group},
			wantStatus:      "Accepted",
			wantParentIdTag: strPtr("FLEET1"),
		},
		{
			name:            "token that is not valid is Blocked",
			token:           &store.Token{Valid: false, CacheMode: store.CacheModeAlways, GroupId: &group},
			wantStatus:      "Blocked",
			wantParentIdTag: strPtr("FLEET1"),
		},
		{
			name:            "token that expired is Expired",
			token:           &store.Token{Valid: true, CacheMode: store.CacheModeAlways, GroupId: &group, ExpiresAt: &past},
			wantStatus:      "Expired",
			wantParentIdTag: strPtr("FLEET1"),
		},
		{
			name:       "token that expires exactly now is Expired",
			token:      &store.Token{Valid: true, CacheMode: store.CacheModeAlways, ExpiresAt: &now},
			wantStatus: "Expired",
		},
		{
			name:           "token that expires later is Accepted until its expiry time",
			token:          &store.Token{Valid: true, CacheMode: store.CacheModeAllowed, ExpiresAt: &future},
			wantStatus:     "Accepted",
			wantExpiryDate: &futureExpiryDate,
		},
		{
			name:           "token that must not be cached expires now",
			token:          &store.Token{Valid: true, CacheMode: store.CacheModeNever},
			wantStatus:     "Accepted",
			wantExpiryDate: &nowExpiryDate,
		},
		{
			name:           "token that must not be cached expires now, not at its expiry time",
			token:          &store.Token{Valid: true, CacheMode: store.CacheModeNever, ExpiresAt: &future},
			wantStatus:     "Accepted",
			wantExpiryDate: &nowExpiryDate,
		},
		{
			name:       "blocked token has no expiry date",
			token:      &store.Token{Valid: false, CacheMode: store.CacheModeNever, ExpiresAt: &future},
			wantStatus: "Blocked",
		},
		{
			name:       "expired token has no expiry date",
			token:      &store.Token{Valid: true, CacheMode: store.CacheModeNever, ExpiresAt: &past},
			wantStatus: "Expired",
		},
		{
			name:       "token that is not valid and expired is Blocked",
			token:      &store.Token{Valid: false, CacheMode: store.CacheModeAlways, ExpiresAt: &past},
			wantStatus: "Blocked",
		},
		{
			// an IdToken has at most 20 characters
			name:       "group that does not fit in a parentIdTag is left out",
			token:      &store.Token{Valid: true, CacheMode: store.CacheModeAlways, GroupId: &longGroup},
			wantStatus: "Accepted",
		},
		{
			name:       "empty group is left out",
			token:      &store.Token{Valid: true, CacheMode: store.CacheModeAlways, GroupId: &emptyGroup},
			wantStatus: "Accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := clockTest.NewFakePassiveClock(now)
			engine := inmemory.NewStore(clk)
			if tt.token != nil {
				tt.token.Uid = "TAG1"
				require.NoError(t, engine.SetToken(context.Background(), tt.token))
			}

			tracer, exporter := testutil.GetTracer()
			ctx, span := tracer.Start(context.Background(), "test")
			got, err := authorizeIdTag(ctx, engine, clk, "TAG1")
			span.End()
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantExpiryDate, got.ExpiryDate)
			assert.Equal(t, tt.wantParentIdTag, got.ParentIdTag)
			// the token itself is returned whenever it is known, whatever its status
			if tt.token == nil {
				assert.Nil(t, got.Token)
			} else {
				assert.Same(t, tt.token, got.Token)
			}

			testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
				"authorize.token":  "TAG1",
				"authorize.status": tt.wantStatus,
			})
		})
	}
}

func TestAuthorizeIdTagReturnsStoreError(t *testing.T) {
	lookupErr := errors.New("token store is down")
	clk := clockTest.NewFakePassiveClock(time.Now())

	got, err := authorizeIdTag(context.Background(), failingTokenStore{err: lookupErr}, clk, "TAG1")

	assert.ErrorIs(t, err, lookupErr)
	assert.Equal(t, idTagAuthorization{}, got)
}
