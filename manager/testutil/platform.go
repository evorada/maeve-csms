// SPDX-License-Identifier: Apache-2.0

package testutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const (
	// PlatformIssuer is the "iss" of the tokens of a Platform.
	PlatformIssuer = "platform"
	// PlatformAudience is the "aud" of the tokens of a Platform.
	PlatformAudience = "maeve"
	// PlatformOrgId is the org of the caller in Platform.Claims.
	PlatformOrgId = "0193b2c4-0000-7000-8000-0000000000c1"
)

// Platform stands in for the platform in tests: it serves a key set over HTTP and signs
// tokens with its Ed25519 key, without the library that the manager verifies them with.
type Platform struct {
	t      testing.TB
	server *httptest.Server

	mu       sync.Mutex
	keyCount int
	keyId    string
	key      ed25519.PrivateKey
	keySet   []map[string]any
	down     bool
	requests int
}

// NewPlatform starts a platform with one signing key. It is stopped when the test ends.
func NewPlatform(t testing.TB) *Platform {
	t.Helper()

	c := &Platform{t: t}
	c.RotateKey()
	c.server = httptest.NewServer(http.HandlerFunc(c.serveKeySet))
	t.Cleanup(c.server.Close)
	return c
}

func (c *Platform) serveKeySet(w http.ResponseWriter, _ *http.Request) {
	c.mu.Lock()
	c.requests++
	down := c.down
	body, err := json.Marshal(map[string]any{"keys": c.keySet})
	c.mu.Unlock()

	if down || err != nil {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// JWKSURL is where the platform serves its key set.
func (c *Platform) JWKSURL() string {
	return c.server.URL + "/jwks.json"
}

// RotateKey replaces the signing key: the key set only holds the new key afterwards.
func (c *Platform) RotateKey() {
	c.t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		c.t.Fatalf("generating a key: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.keyCount++
	c.keyId = fmt.Sprintf("key-%d", c.keyCount)
	c.key = privateKey
	c.keySet = []map[string]any{Ed25519JWK(c.keyId, publicKey)}
}

// KeyId is the id of the signing key.
func (c *Platform) KeyId() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keyId
}

// PublicKey is the public half of the signing key.
func (c *Platform) PublicKey() ed25519.PublicKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.key.Public().(ed25519.PublicKey)
}

// SetKeySet replaces the key set that is served, without changing the signing key.
func (c *Platform) SetKeySet(keys ...map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keySet = append([]map[string]any{}, keys...)
}

// SetDown makes the platform answer requests for the key set with an error, or not.
func (c *Platform) SetDown(down bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down = down
}

// Requests is the number of requests for the key set so far.
func (c *Platform) Requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

// Claims returns the claims of a token that the platform mints at the given time for a
// person of the org PlatformOrgId: valid for 60 seconds. A test changes what it needs.
func (c *Platform) Claims(now time.Time) map[string]any {
	return map[string]any{
		"iss":       PlatformIssuer,
		"aud":       PlatformAudience,
		"sub":       "user:alice",
		"jti":       "token-1",
		"iat":       now.Unix(),
		"exp":       now.Add(60 * time.Second).Unix(),
		"org":       PlatformOrgId,
		"org_scope": "subtree",
		"perms":     []string{"cpms.station.read", "cpms.token.read"},
	}
}

// Sign returns a token with the given claims, signed with the signing key.
func (c *Platform) Sign(claims map[string]any) string {
	c.t.Helper()

	header := map[string]any{"alg": "EdDSA", "typ": "JWT", "kid": c.KeyId()}
	return SignJWS(c.t, header, claims, c.Signature)
}

// Signature returns the signature of the signing key over the signing input of a JWS.
func (c *Platform) Signature(signingInput []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ed25519.Sign(c.key, signingInput)
}

// Token returns a valid token for the given time: Claims signed with the signing key.
func (c *Platform) Token(now time.Time) string {
	c.t.Helper()
	return c.Sign(c.Claims(now))
}

// SignJWS returns the compact serialization of a JWS with the given protected header
// and payload. sign gets the signing input and returns the signature.
func SignJWS(t testing.TB, header, payload map[string]any, sign func(signingInput []byte) []byte) string {
	t.Helper()

	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("encoding the header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encoding the payload: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sign([]byte(signingInput)))
}

// Ed25519JWK returns the public key as a member of a key set.
func Ed25519JWK(keyId string, publicKey ed25519.PublicKey) map[string]any {
	return map[string]any{
		"kty": "OKP",
		"crv": "Ed25519",
		"kid": keyId,
		"alg": "EdDSA",
		"use": "sig",
		"x":   base64.RawURLEncoding.EncodeToString(publicKey),
	}
}
