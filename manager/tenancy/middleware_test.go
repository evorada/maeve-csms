// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

// verifierFunc is a Verifier that records the token it was asked about.
type verifierFunc func(rawToken string) (*tenancy.Principal, error)

func (f verifierFunc) Verify(_ context.Context, rawToken string) (*tenancy.Principal, error) {
	return f(rawToken)
}

// serve sends a request with the given Authorization headers through the middleware.
// It returns the response and the caller that the next handler saw, nil when the
// handler was not reached.
func serve(t *testing.T, v tenancy.Verifier, authorization ...string) (*httptest.ResponseRecorder, *tenancy.Principal) {
	t.Helper()

	var seen *tenancy.Principal
	handler := tenancy.Middleware(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := tenancy.FromContext(r.Context())
		require.True(t, ok, "the handler is only reached with a caller")
		seen = p
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/token", nil)
	for _, value := range authorization {
		req.Header.Add("Authorization", value)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr, seen
}

func requireUnauthorized(t *testing.T, rr *httptest.ResponseRecorder, seen *tenancy.Principal, wantBody string) {
	t.Helper()

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Nil(t, seen, "the handler must not be reached")
	assert.Equal(t, `Bearer error="invalid_token"`, rr.Header().Get("WWW-Authenticate"))
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, wantBody, rr.Body.String())
}

func TestMiddlewareWithTheTokenVerifier(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)
	token := console.Token(tokenTime)

	const missing = `{"status":"Unauthorized","error":"missing bearer token"}`
	const invalid = `{"status":"Unauthorized","error":"invalid token"}`

	t.Run("no header", func(t *testing.T) {
		rr, seen := serve(t, verifier)
		requireUnauthorized(t, rr, seen, missing)
	})

	t.Run("basic authentication", func(t *testing.T) {
		rr, seen := serve(t, verifier, "Basic dXNlcjpwYXNz")
		requireUnauthorized(t, rr, seen, missing)
	})

	t.Run("a valid token under another scheme", func(t *testing.T) {
		rr, seen := serve(t, verifier, "Basic "+token)
		requireUnauthorized(t, rr, seen, missing)
		rr, seen = serve(t, verifier, token)
		requireUnauthorized(t, rr, seen, missing)
	})

	t.Run("bearer without a token", func(t *testing.T) {
		rr, seen := serve(t, verifier, "Bearer")
		requireUnauthorized(t, rr, seen, missing)
		rr, seen = serve(t, verifier, "Bearer ")
		requireUnauthorized(t, rr, seen, missing)
	})

	t.Run("two authorization headers", func(t *testing.T) {
		rr, seen := serve(t, verifier, "Bearer "+token, "Bearer "+token)
		requireUnauthorized(t, rr, seen, missing)
	})

	t.Run("a token that is not valid", func(t *testing.T) {
		claims := console.Claims(tokenTime)
		claims["aud"] = "billing"
		rr, seen := serve(t, verifier, "Bearer "+console.Sign(claims))
		// the answer does not say which check failed
		requireUnauthorized(t, rr, seen, invalid)

		rr, seen = serve(t, verifier, "Bearer letmein")
		requireUnauthorized(t, rr, seen, invalid)
		rr, seen = serve(t, verifier, "Bearer "+token+" "+token)
		requireUnauthorized(t, rr, seen, invalid)
	})

	t.Run("a valid token", func(t *testing.T) {
		for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
			rr, seen := serve(t, verifier, scheme+" "+token)
			require.Equal(t, http.StatusNoContent, rr.Code, scheme)
			require.NotNil(t, seen)
			assert.Equal(t, "user:alice", seen.Subject)
			assert.Equal(t, testutil.ConsoleOrgId, seen.OrgId)
			assert.Equal(t, tenancy.OrgScopeSubtree, seen.OrgScope)
			assert.Equal(t, "token-1", seen.Jti)
			assert.Empty(t, rr.Header().Get("WWW-Authenticate"))
		}
	})
}

func TestMiddlewareWhenTheKeysAreUnavailable(t *testing.T) {
	console := testutil.NewConsole(t)
	console.SetDown(true)
	verifier, _ := newTestVerifier(t, console)

	rr, seen := serve(t, verifier, "Bearer "+console.Token(tokenTime))
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	assert.Nil(t, seen)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"status":"Service Unavailable","error":"auth keys unavailable"}`, rr.Body.String())

	// a request without a token is not one that could be let in later
	rr, seen = serve(t, verifier)
	requireUnauthorized(t, rr, seen, `{"status":"Unauthorized","error":"missing bearer token"}`)
}

func TestMiddlewareFailsClosed(t *testing.T) {
	const invalid = `{"status":"Unauthorized","error":"invalid token"}`

	t.Run("the reason for a refusal stays in the manager", func(t *testing.T) {
		rr, seen := serve(t, verifierFunc(func(string) (*tenancy.Principal, error) {
			return nil, fmt.Errorf("%w: wrong issuer", tenancy.ErrInvalidToken)
		}), "Bearer some-token")
		requireUnauthorized(t, rr, seen, invalid)
		assert.NotContains(t, rr.Body.String(), "issuer")
	})

	t.Run("an error that the middleware does not know", func(t *testing.T) {
		rr, seen := serve(t, verifierFunc(func(string) (*tenancy.Principal, error) {
			return nil, context.DeadlineExceeded
		}), "Bearer some-token")
		requireUnauthorized(t, rr, seen, invalid)
	})

	t.Run("a caller is returned together with an error", func(t *testing.T) {
		rr, seen := serve(t, verifierFunc(func(string) (*tenancy.Principal, error) {
			return &tenancy.Principal{Subject: "user:mallory"}, tenancy.ErrInvalidToken
		}), "Bearer some-token")
		requireUnauthorized(t, rr, seen, invalid)
	})

	t.Run("no caller and no error", func(t *testing.T) {
		rr, seen := serve(t, verifierFunc(func(string) (*tenancy.Principal, error) {
			return nil, nil
		}), "Bearer some-token")
		requireUnauthorized(t, rr, seen, invalid)
	})

	t.Run("no verifier", func(t *testing.T) {
		assert.Panics(t, func() { tenancy.Middleware(nil) })
	})
}

func TestMiddlewareGivesTheVerifierOnlyTheToken(t *testing.T) {
	var got []string
	verifier := verifierFunc(func(rawToken string) (*tenancy.Principal, error) {
		got = append(got, rawToken)
		return &tenancy.Principal{Subject: "user:alice"}, nil
	})

	for _, authorization := range [][]string{
		{"Bearer abc.def.ghi"},
		{"Bearer   abc.def.ghi"},
		{},
		{"Basic abc.def.ghi"},
	} {
		rr, _ := serve(t, verifier, authorization...)
		require.Equal(t, http.StatusNoContent, rr.Code)
	}
	assert.Equal(t, []string{"abc.def.ghi", "abc.def.ghi", "", ""}, got)
}

func TestMiddlewareInDevMode(t *testing.T) {
	verifier := tenancy.DevVerifier{Principal: tenancy.Principal{Subject: "user:dev", OrgId: testutil.ConsoleOrgId}}

	for _, authorization := range [][]string{{}, {"Bearer letmein"}, {"Basic dXNlcjpwYXNz"}} {
		rr, seen := serve(t, verifier, authorization...)
		require.Equal(t, http.StatusNoContent, rr.Code)
		require.NotNil(t, seen)
		assert.Equal(t, "user:dev", seen.Subject)
	}
}
