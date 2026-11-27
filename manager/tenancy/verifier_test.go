// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
	clockTest "k8s.io/utils/clock/testing"
)

const (
	testMinRefreshInterval = 5 * time.Minute
	testClockSkew          = 5 * time.Second
	testMaxLifetime        = 120 * time.Second
	// how long the verifier waits after a request for the key set that failed
	testRetryInterval = 5 * time.Second
)

// tokenTime is the time of the fake clock when a test starts.
var tokenTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// newTestVerifier creates a verifier for the tokens of the console, with a clock that
// only moves when the test moves it.
func newTestVerifier(t *testing.T, console *testutil.Console) (*tenancy.JWKSVerifier, *clockTest.FakeClock) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	fakeClock := clockTest.NewFakeClock(tokenTime)
	verifier, err := tenancy.NewJWKSVerifier(ctx, tenancy.JWKSVerifierConfig{
		JWKSURL:            console.JWKSURL(),
		Issuer:             testutil.ConsoleIssuer,
		Audience:           testutil.ConsoleAudience,
		MinRefreshInterval: testMinRefreshInterval,
		ClockSkew:          testClockSkew,
		MaxLifetime:        testMaxLifetime,
		Clock:              fakeClock,
	})
	require.NoError(t, err)
	return verifier, fakeClock
}

// requireInvalid checks that the token was refused as invalid, for the given reason.
func requireInvalid(t *testing.T, p *tenancy.Principal, err error, reason string) {
	t.Helper()

	require.ErrorIs(t, err, tenancy.ErrInvalidToken)
	assert.Nil(t, p)
	assert.Equal(t, "invalid token: "+reason, err.Error())
}

func TestVerifyValidToken(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)
	ctx := context.Background()

	t.Run("the claims become the caller", func(t *testing.T) {
		claims := console.Claims(tokenTime)
		claims["sub"] = "svc:support-tool"
		claims["act"] = map[string]any{"sub": "user:staff-123"}
		claims["org_scope"] = "self"
		claims["jti"] = "jti-42"

		p, err := verifier.Verify(ctx, console.Sign(claims))
		require.NoError(t, err)
		assert.Equal(t, &tenancy.Principal{
			Subject:  "svc:support-tool",
			Actor:    "user:staff-123",
			OrgId:    testutil.ConsoleOrgId,
			OrgScope: tenancy.OrgScopeSelf,
			Perms:    []string{"cpms.station.read", "cpms.token.read"},
			Jti:      "jti-42",
		}, p)
		assert.True(t, p.IsService())
		assert.True(t, p.Has(tenancy.PermStationRead))
		assert.False(t, p.Has(tenancy.PermStationWrite))
	})

	t.Run("no actor and no permissions", func(t *testing.T) {
		claims := console.Claims(tokenTime)
		delete(claims, "perms")

		p, err := verifier.Verify(ctx, console.Sign(claims))
		require.NoError(t, err)
		assert.Equal(t, "user:alice", p.Subject)
		assert.Equal(t, "", p.Actor)
		assert.Equal(t, tenancy.OrgScopeSubtree, p.OrgScope)
		assert.Empty(t, p.Perms)
		assert.False(t, p.Has(tenancy.PermStationRead))
	})

	t.Run("one of several audiences", func(t *testing.T) {
		claims := console.Claims(tokenTime)
		claims["aud"] = []string{"billing", testutil.ConsoleAudience}

		_, err := verifier.Verify(ctx, console.Sign(claims))
		require.NoError(t, err)
	})

	t.Run("the org is returned in lower case", func(t *testing.T) {
		claims := console.Claims(tokenTime)
		claims["org"] = strings.ToUpper(testutil.ConsoleOrgId)

		p, err := verifier.Verify(ctx, console.Sign(claims))
		require.NoError(t, err)
		assert.Equal(t, testutil.ConsoleOrgId, p.OrgId)
	})

	t.Run("times at the edge of the leeway", func(t *testing.T) {
		for name, change := range map[string]func(claims map[string]any){
			"expired for less than the skew": func(claims map[string]any) {
				claims["exp"] = tokenTime.Add(-testClockSkew + time.Second).Unix()
				claims["iat"] = tokenTime.Add(-time.Minute).Unix()
			},
			"issued in the future within the skew": func(claims map[string]any) {
				claims["iat"] = tokenTime.Add(testClockSkew).Unix()
				claims["exp"] = tokenTime.Add(time.Minute).Unix()
			},
			"not before within the skew": func(claims map[string]any) {
				claims["nbf"] = tokenTime.Add(testClockSkew).Unix()
			},
			"the longest lifetime": func(claims map[string]any) {
				claims["exp"] = tokenTime.Add(testMaxLifetime).Unix()
			},
		} {
			claims := console.Claims(tokenTime)
			change(claims)
			_, err := verifier.Verify(ctx, console.Sign(claims))
			assert.NoError(t, err, name)
		}
	})
}

func TestVerifyRefusesWrongClaims(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)

	tests := map[string]struct {
		change func(claims map[string]any)
		reason string
	}{
		"wrong issuer":            {func(c map[string]any) { c["iss"] = "console2" }, "wrong issuer"},
		"no issuer":               {func(c map[string]any) { delete(c, "iss") }, "wrong issuer"},
		"wrong audience":          {func(c map[string]any) { c["aud"] = "billing" }, "wrong audience"},
		"wrong audiences":         {func(c map[string]any) { c["aud"] = []string{"billing", "maeve2"} }, "wrong audience"},
		"no audience":             {func(c map[string]any) { delete(c, "aud") }, "wrong audience"},
		"no subject":              {func(c map[string]any) { delete(c, "sub") }, "no subject"},
		"empty subject":           {func(c map[string]any) { c["sub"] = "" }, "no subject"},
		"no token id":             {func(c map[string]any) { delete(c, "jti") }, "no token id"},
		"no expiry":               {func(c map[string]any) { delete(c, "exp") }, "no expiry"},
		"no issue time":           {func(c map[string]any) { delete(c, "iat") }, "no issue time"},
		"expiry is not a number":  {func(c map[string]any) { c["exp"] = "tomorrow" }, "malformed claims"},
		"org is not a UUID":       {func(c map[string]any) { c["org"] = "acme" }, "org is not a UUID"},
		"org without hyphens":     {func(c map[string]any) { c["org"] = strings.ReplaceAll(testutil.ConsoleOrgId, "-", "") }, "org is not a UUID"},
		"org is not a string":     {func(c map[string]any) { c["org"] = 7 }, "org is not a UUID"},
		"no org":                  {func(c map[string]any) { delete(c, "org") }, "org is not a UUID"},
		"org scope all":           {func(c map[string]any) { c["org_scope"] = "all" }, "unknown org scope"},
		"no org scope":            {func(c map[string]any) { delete(c, "org_scope") }, "unknown org scope"},
		"perms is a string":       {func(c map[string]any) { c["perms"] = "cpms.station.read" }, "perms is not a list"},
		"perms holds a number":    {func(c map[string]any) { c["perms"] = []any{"cpms.station.read", 1} }, "perms is not a list of strings"},
		"act is a string":         {func(c map[string]any) { c["act"] = "user:staff-123" }, "act is not an object"},
		"act.sub is not a string": {func(c map[string]any) { c["act"] = map[string]any{"sub": 1} }, "act.sub is not a string"},
		"expired": {func(c map[string]any) {
			c["iat"] = tokenTime.Add(-2 * time.Minute).Unix()
			c["exp"] = tokenTime.Add(-time.Minute).Unix()
		}, "expired"},
		"expired for exactly the skew": {func(c map[string]any) {
			c["iat"] = tokenTime.Add(-time.Minute).Unix()
			c["exp"] = tokenTime.Add(-testClockSkew).Unix()
		}, "expired"},
		"expiry at the start of time": {func(c map[string]any) { c["exp"] = 0 }, "expired"},
		"issued in the future beyond the skew": {func(c map[string]any) {
			c["iat"] = tokenTime.Add(testClockSkew + time.Second).Unix()
			c["exp"] = tokenTime.Add(time.Minute).Unix()
		}, "issued in the future"},
		"not valid yet": {func(c map[string]any) {
			c["nbf"] = tokenTime.Add(testClockSkew + time.Second).Unix()
		}, "not valid yet"},
		"lifetime of an hour": {func(c map[string]any) {
			c["exp"] = tokenTime.Add(time.Hour).Unix()
		}, "lifetime is not within the accepted one"},
		"lifetime one second too long": {func(c map[string]any) {
			c["exp"] = tokenTime.Add(testMaxLifetime + time.Second).Unix()
		}, "lifetime is not within the accepted one"},
		"expires when it is issued": {func(c map[string]any) {
			c["exp"] = c["iat"]
		}, "lifetime is not within the accepted one"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			claims := console.Claims(tokenTime)
			tc.change(claims)

			p, err := verifier.Verify(context.Background(), console.Sign(claims))
			requireInvalid(t, p, err, tc.reason)
		})
	}

	// the claims that the cases start from are accepted
	_, err := verifier.Verify(context.Background(), console.Token(tokenTime))
	require.NoError(t, err)
}

func TestVerifyRefusesAnExpiredTokenWhenTheClockMoves(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, fakeClock := newTestVerifier(t, console)
	token := console.Token(tokenTime) // valid for 60 seconds

	fakeClock.Step(60*time.Second + testClockSkew - time.Second)
	_, err := verifier.Verify(context.Background(), token)
	require.NoError(t, err)

	fakeClock.Step(time.Second)
	p, err := verifier.Verify(context.Background(), token)
	requireInvalid(t, p, err, "expired")
}

func TestVerifyRefusesWhatIsNotAToken(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)
	ctx := context.Background()

	p, err := verifier.Verify(ctx, "")
	assert.Nil(t, p)
	assert.Equal(t, tenancy.ErrMissingToken, err)

	token := console.Token(tokenTime)
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	for name, tc := range map[string]struct {
		token  string
		reason string
	}{
		"not a JWS":               {"letmein", "not a compact JWS"},
		"two parts":               {parts[0] + "." + parts[1], "not a compact JWS"},
		"no signature":            {parts[0] + "." + parts[1] + ".", "not a compact JWS"},
		"four parts":              {token + "." + parts[2], "not a compact JWS"},
		"a space in the token":    {parts[0] + ". " + parts[1] + "." + parts[2], "not a compact JWS"},
		"padded base64":           {parts[0] + "=." + parts[1] + "." + parts[2], "not a compact JWS"},
		"JSON serialization":      {`{"payload":"` + parts[1] + `","protected":"` + parts[0] + `","signature":"` + parts[2] + `"}`, "not a compact JWS"},
		"header is not JSON":      {"bm90IGpzb24." + parts[1] + "." + parts[2], "malformed JWS"},
		"too long":                {token + strings.Repeat("A", 16<<10), "token is too long"},
		"signature cut short":     {token[:len(token)-4], "signature does not verify"},
		"payload of another call": {parts[0] + "." + strings.Split(console.Sign(map[string]any{"sub": "svc:root"}), ".")[1] + "." + parts[2], "signature does not verify"},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := verifier.Verify(ctx, tc.token)
			requireInvalid(t, p, err, tc.reason)
		})
	}
}

func TestVerifyPinsTheAlgorithm(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)
	ctx := context.Background()
	claims := console.Claims(tokenTime)

	hmacWithThePublicKey := func(signingInput []byte) []byte {
		mac := hmac.New(sha256.New, console.PublicKey())
		mac.Write(signingInput)
		return mac.Sum(nil)
	}
	_, otherKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		header map[string]any
		sign   func(signingInput []byte) []byte
		reason string
	}{
		"HS256 with the public key as the secret": {
			header: map[string]any{"alg": "HS256", "typ": "JWT", "kid": console.KeyId()},
			sign:   hmacWithThePublicKey,
			reason: "algorithm is not EdDSA",
		},
		"EdDSA in the header, HMAC with the public key as the signature": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": console.KeyId()},
			sign:   hmacWithThePublicKey,
			reason: "signature does not verify",
		},
		"none without a signature": {
			header: map[string]any{"alg": "none", "typ": "JWT", "kid": console.KeyId()},
			sign:   func([]byte) []byte { return nil },
			reason: "not a compact JWS",
		},
		"none with a signature": {
			header: map[string]any{"alg": "none", "typ": "JWT", "kid": console.KeyId()},
			sign:   func([]byte) []byte { return []byte("signature") },
			reason: "algorithm is not EdDSA",
		},
		"RS256": {
			header: map[string]any{"alg": "RS256", "typ": "JWT", "kid": console.KeyId()},
			sign:   func([]byte) []byte { return make([]byte, 256) },
			reason: "algorithm is not EdDSA",
		},
		"no algorithm": {
			header: map[string]any{"typ": "JWT", "kid": console.KeyId()},
			sign:   console.Signature,
			reason: "algorithm is not EdDSA",
		},
		"signed with a key that is not in the key set": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": console.KeyId()},
			sign:   func(signingInput []byte) []byte { return ed25519.Sign(otherKey, signingInput) },
			reason: "signature does not verify",
		},
		"a key id that the key set does not have, signed with the key of the console": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "key-0"},
			sign:   console.Signature,
			reason: "unknown key id",
		},
		"no key id": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT"},
			sign:   console.Signature,
			reason: "no key id",
		},
		"a critical header parameter": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": console.KeyId(), "crit": []string{"exp"}},
			sign:   console.Signature,
			reason: "unsupported header parameter",
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := verifier.Verify(ctx, testutil.SignJWS(t, tc.header, claims, tc.sign))
			requireInvalid(t, p, err, tc.reason)
		})
	}
}

func TestVerifyPinsTheKeyType(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecdhKey, err := ecKey.PublicKey.ECDH()
	require.NoError(t, err)
	point := ecdhKey.Bytes() // 0x04, X, Y
	require.Len(t, point, 65)
	signES256 := func(signingInput []byte) []byte {
		digest := sha256.Sum256(signingInput)
		r, s, err := ecdsa.Sign(rand.Reader, ecKey, digest[:])
		require.NoError(t, err)
		signature := make([]byte, 64)
		r.FillBytes(signature[:32])
		s.FillBytes(signature[32:])
		return signature
	}

	console := testutil.NewConsole(t)
	publicKey := console.PublicKey()
	// the signing key of the console: only the entry of the key in the key set is wrong
	signEdDSA := console.Signature
	ed25519JWK := func(keyId string, change func(key map[string]any)) map[string]any {
		key := testutil.Ed25519JWK(keyId, publicKey)
		change(key)
		return key
	}
	console.SetKeySet(
		map[string]any{
			"kty": "EC", "crv": "P-256", "kid": "ec-1", "alg": "ES256", "use": "sig",
			"x": base64.RawURLEncoding.EncodeToString(point[1:33]),
			"y": base64.RawURLEncoding.EncodeToString(point[33:]),
		},
		ed25519JWK("for-es256", func(key map[string]any) { key["alg"] = "ES256" }),
		ed25519JWK("for-encryption", func(key map[string]any) { key["use"] = "enc" }),
		ed25519JWK("x25519", func(key map[string]any) { key["crv"] = "X25519" }),
		ed25519JWK("short", func(key map[string]any) { key["x"] = base64.RawURLEncoding.EncodeToString(publicKey[:16]) }),
		ed25519JWK("good", func(map[string]any) {}),
	)
	verifier, _ := newTestVerifier(t, console)
	ctx := context.Background()
	claims := console.Claims(tokenTime)

	for name, tc := range map[string]struct {
		header map[string]any
		sign   func(signingInput []byte) []byte
		reason string
	}{
		"ES256 token for the ES256 key": {
			header: map[string]any{"alg": "ES256", "typ": "JWT", "kid": "ec-1"},
			sign:   signES256,
			reason: "algorithm is not EdDSA",
		},
		"EdDSA in the header for the ES256 key": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "ec-1"},
			sign:   signES256,
			reason: "key is not an OKP key",
		},
		"key that is marked for another algorithm": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "for-es256"},
			sign:   signEdDSA,
			reason: "key is not for EdDSA",
		},
		"key that is marked for encryption": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "for-encryption"},
			sign:   signEdDSA,
			reason: "key is not for signatures",
		},
		"key of another curve": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "x25519"},
			sign:   signEdDSA,
			reason: "key is not an Ed25519 key",
		},
		"key of the wrong length": {
			header: map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "short"},
			sign:   signEdDSA,
			reason: "key is not a valid Ed25519 public key",
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := verifier.Verify(ctx, testutil.SignJWS(t, tc.header, claims, tc.sign))
			requireInvalid(t, p, err, tc.reason)
		})
	}

	// the same token is accepted with the proper entry of the key
	_, err = verifier.Verify(ctx, testutil.SignJWS(t, map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "good"}, claims, signEdDSA))
	require.NoError(t, err)
}

func TestVerifyRefreshesTheKeySetOnceForAnUnknownKeyId(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, fakeClock := newTestVerifier(t, console)
	ctx := context.Background()
	require.Equal(t, 1, console.Requests(), "the key set is loaded when the verifier is created")

	_, err := verifier.Verify(ctx, console.Token(tokenTime))
	require.NoError(t, err)
	assert.Equal(t, 1, console.Requests(), "a known key needs no request")

	oldToken := console.Token(tokenTime)
	console.RotateKey()

	// the key of the token is not in the cached key set: it is loaded again, once
	p, err := verifier.Verify(ctx, console.Token(tokenTime))
	require.NoError(t, err)
	assert.Equal(t, "user:alice", p.Subject)
	assert.Equal(t, 2, console.Requests())

	// the key that was replaced is gone with the old key set
	p, err = verifier.Verify(ctx, oldToken)
	requireInvalid(t, p, err, "unknown key id")
	assert.Equal(t, 2, console.Requests(), "no second request within the refresh interval")

	unknownKey := func() string {
		return testutil.SignJWS(t, map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": "key-of-somebody-else"},
			console.Claims(fakeClock.Now()), func([]byte) []byte { return make([]byte, ed25519.SignatureSize) })
	}
	for i := 0; i < 20; i++ {
		p, err = verifier.Verify(ctx, unknownKey())
		requireInvalid(t, p, err, "unknown key id")
	}
	assert.Equal(t, 2, console.Requests(), "a flood of unknown key ids does not reach the console")

	fakeClock.Step(testMinRefreshInterval - time.Second)
	p, err = verifier.Verify(ctx, unknownKey())
	requireInvalid(t, p, err, "unknown key id")
	assert.Equal(t, 2, console.Requests())

	fakeClock.Step(time.Second)
	for i := 0; i < 3; i++ {
		p, err = verifier.Verify(ctx, unknownKey())
		requireInvalid(t, p, err, "unknown key id")
	}
	assert.Equal(t, 3, console.Requests(), "one more request after the refresh interval")

	// tokens of the known key were accepted all along
	_, err = verifier.Verify(ctx, console.Token(fakeClock.Now()))
	require.NoError(t, err)
	assert.Equal(t, 3, console.Requests())
}

func TestVerifyConcurrentCallersShareOneRefresh(t *testing.T) {
	console := testutil.NewConsole(t)
	verifier, _ := newTestVerifier(t, console)
	console.RotateKey()
	token := console.Token(tokenTime)

	const callers = 20
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			_, err := verifier.Verify(context.Background(), token)
			errs <- err
		}()
	}
	for i := 0; i < callers; i++ {
		// nobody is refused because another caller is loading the key set
		assert.NoError(t, <-errs)
	}
	assert.Equal(t, 2, console.Requests())
}

func TestVerifyWhileTheKeySetCannotBeLoaded(t *testing.T) {
	console := testutil.NewConsole(t)
	console.SetDown(true)

	// the console may start after the manager
	verifier, fakeClock := newTestVerifier(t, console)
	ctx := context.Background()
	require.Equal(t, 1, console.Requests())

	for i := 0; i < 20; i++ {
		p, err := verifier.Verify(ctx, console.Token(tokenTime))
		assert.Nil(t, p)
		require.Equal(t, tenancy.ErrKeysUnavailable, err)
	}
	assert.Equal(t, 2, console.Requests(), "requests while the console is down do not all reach it")

	console.SetDown(false)
	_, err := verifier.Verify(ctx, console.Token(tokenTime))
	require.Equal(t, tenancy.ErrKeysUnavailable, err, "not asked again before the retry interval is over")
	assert.Equal(t, 2, console.Requests())

	fakeClock.Step(testRetryInterval)
	p, err := verifier.Verify(ctx, console.Token(fakeClock.Now()))
	require.NoError(t, err)
	assert.Equal(t, "user:alice", p.Subject)
	assert.Equal(t, 3, console.Requests())

	// a token that is not one is refused as such, also while there are no keys
	console2 := testutil.NewConsole(t)
	console2.SetDown(true)
	verifier2, _ := newTestVerifier(t, console2)
	p, err = verifier2.Verify(ctx, "letmein")
	requireInvalid(t, p, err, "not a compact JWS")
	p, err = verifier2.Verify(ctx, "")
	assert.Nil(t, p)
	assert.Equal(t, tenancy.ErrMissingToken, err)
}

func TestVerifyWithAnEmptyKeySet(t *testing.T) {
	console := testutil.NewConsole(t)
	keyId, publicKey := console.KeyId(), console.PublicKey()
	console.SetKeySet()

	verifier, fakeClock := newTestVerifier(t, console)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := verifier.Verify(ctx, console.Token(tokenTime))
		require.Equal(t, tenancy.ErrKeysUnavailable, err)
	}
	assert.Equal(t, 2, console.Requests())

	console.SetKeySet(testutil.Ed25519JWK(keyId, publicKey))
	fakeClock.Step(testRetryInterval)
	_, err := verifier.Verify(ctx, console.Token(fakeClock.Now()))
	require.NoError(t, err)
}

func TestNewJWKSVerifierRefusesAnIncompleteConfiguration(t *testing.T) {
	valid := tenancy.JWKSVerifierConfig{
		JWKSURL:  "http://console.internal/jwks.json",
		Issuer:   testutil.ConsoleIssuer,
		Audience: testutil.ConsoleAudience,
	}

	for name, change := range map[string]func(cfg *tenancy.JWKSVerifierConfig){
		"no key set URL":       func(cfg *tenancy.JWKSVerifierConfig) { cfg.JWKSURL = "" },
		"relative key set URL": func(cfg *tenancy.JWKSVerifierConfig) { cfg.JWKSURL = "/jwks.json" },
		"key set in a file":    func(cfg *tenancy.JWKSVerifierConfig) { cfg.JWKSURL = "file:///etc/jwks.json" },
		"user name in the URL": func(cfg *tenancy.JWKSVerifierConfig) {
			cfg.JWKSURL = "http://manager@console.internal/jwks.json"
		},
		"no issuer":     func(cfg *tenancy.JWKSVerifierConfig) { cfg.Issuer = "" },
		"no audience":   func(cfg *tenancy.JWKSVerifierConfig) { cfg.Audience = "" },
		"negative skew": func(cfg *tenancy.JWKSVerifierConfig) { cfg.ClockSkew = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cfg := valid
			change(&cfg)
			verifier, err := tenancy.NewJWKSVerifier(ctx, cfg)
			require.Error(t, err)
			assert.Nil(t, verifier)
		})
	}
}

func TestDevVerifierAcceptsEveryRequest(t *testing.T) {
	verifier := tenancy.DevVerifier{Principal: tenancy.Principal{
		Subject:  "user:dev",
		OrgId:    testutil.ConsoleOrgId,
		OrgScope: tenancy.OrgScopeSubtree,
		Perms:    []string{tenancy.PermStationRead},
	}}

	for _, token := range []string{"", "letmein"} {
		p, err := verifier.Verify(context.Background(), token)
		require.NoError(t, err)
		assert.Equal(t, &verifier.Principal, p)
		assert.NotSame(t, &verifier.Principal, p)

		// what a handler does to its caller stays with the request
		p.Perms[0] = tenancy.PermTenantManage
		assert.Equal(t, []string{tenancy.PermStationRead}, verifier.Principal.Perms)
	}
}
