// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/lestrrat-go/jwx/jws"
	"github.com/lestrrat-go/jwx/jwt"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// Verifier checks the token of a request and says who the caller is.
type Verifier interface {
	// Verify returns the caller that the token stands for. rawToken is the bearer token
	// of the request, "" when the request has none. The error is ErrMissingToken, wraps
	// ErrInvalidToken or is ErrKeysUnavailable.
	Verify(ctx context.Context, rawToken string) (*Principal, error)
}

var (
	// ErrMissingToken is returned for a request without a bearer token.
	ErrMissingToken = errors.New("missing bearer token")
	// ErrInvalidToken is wrapped by the error for a token that is refused. The wrapping
	// error names the check that failed: it is meant for the log, not for the caller.
	ErrInvalidToken = errors.New("invalid token")
	// ErrKeysUnavailable is returned while the keys that sign the tokens cannot be loaded.
	ErrKeysUnavailable = errors.New("signing keys unavailable")
)

const (
	defaultMinRefreshInterval = 5 * time.Minute
	defaultMaxLifetime        = 120 * time.Second

	// keysFetchTimeout bounds one request for the key set.
	keysFetchTimeout = 5 * time.Second
	// keysRetryInterval is how long the verifier waits after a request for the key set
	// that failed before it sends the next one.
	keysRetryInterval = 5 * time.Second
	// maxKeySetBytes is the longest key set that is read.
	maxKeySetBytes = 1 << 20
	// maxTokenLength is the longest token that is looked at.
	maxTokenLength = 16 << 10

	// the private claims of a platform token
	claimOrg      = "org"
	claimOrgScope = "org_scope"
	claimPerms    = "perms"
	claimActor    = "act"

	// canonicalUUIDLength is the length of a UUID in its hyphenated form.
	canonicalUUIDLength = 36
)

// JWKSVerifierConfig configures a JWKSVerifier.
type JWKSVerifierConfig struct {
	// JWKSURL is where the platform publishes the public keys that sign its tokens.
	JWKSURL string
	// Issuer is the only accepted "iss".
	Issuer string
	// Audience must be one of the "aud" values of a token.
	Audience string
	// MinRefreshInterval is the shortest time between two requests for the key set
	// that the platform answered. Zero is 5 minutes.
	MinRefreshInterval time.Duration
	// ClockSkew is the leeway for "exp", "iat" and "nbf".
	ClockSkew time.Duration
	// MaxLifetime is the longest accepted time between "iat" and "exp". Zero is 120
	// seconds.
	MaxLifetime time.Duration
	// HTTPClient fetches the key set. Nil is a client with a timeout of 5 seconds
	// that does not follow redirects.
	HTTPClient *http.Client
	// Clock is what the time claims are checked against. Nil is the real clock.
	Clock clock.PassiveClock
}

// JWKSVerifier verifies tokens that the platform signed with one of the Ed25519 keys of
// its key set. The algorithm (EdDSA), the key type, the issuer and the audience are
// fixed by the configuration and never taken from a token or from the key set.
type JWKSVerifier struct {
	cfg           JWKSVerifierConfig
	url           string
	keys          *jwk.AutoRefresh
	retryInterval time.Duration

	// loaded is set once a key set with at least one key was fetched: from then on the
	// key set is read from the cache without a request.
	loaded atomic.Bool
	// refreshing is held by the one caller that asks the platform for the key set.
	refreshing chan struct{}
	// nextRefresh is when the platform may be asked again. It is only touched while
	// refreshing is held.
	nextRefresh time.Time
}

// NewJWKSVerifier creates a verifier that loads the key set from cfg.JWKSURL and keeps
// it up to date until ctx is done. A key set that cannot be loaded is not an error: the
// platform may start after the manager, and until then every token is refused with
// ErrKeysUnavailable.
func NewJWKSVerifier(ctx context.Context, cfg JWKSVerifierConfig) (*JWKSVerifier, error) {
	keysURL, err := url.Parse(cfg.JWKSURL)
	// no credentials in the URL: it is written to the log when the key set cannot be loaded
	if err != nil || (keysURL.Scheme != "http" && keysURL.Scheme != "https") || keysURL.Host == "" || keysURL.User != nil {
		return nil, errors.New("jwks url must be an absolute http or https URL without credentials")
	}
	if cfg.Issuer == "" {
		return nil, errors.New("issuer must be set")
	}
	if cfg.Audience == "" {
		return nil, errors.New("audience must be set")
	}
	if cfg.MinRefreshInterval < 0 || cfg.ClockSkew < 0 || cfg.MaxLifetime < 0 {
		return nil, errors.New("min refresh interval, clock skew and max lifetime must not be negative")
	}
	if cfg.MinRefreshInterval == 0 {
		cfg.MinRefreshInterval = defaultMinRefreshInterval
	}
	if cfg.MaxLifetime == 0 {
		cfg.MaxLifetime = defaultMaxLifetime
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Timeout: keysFetchTimeout,
			// the key set is pinned to its URL
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if cfg.Clock == nil {
		cfg.Clock = clock.RealClock{}
	}

	v := &JWKSVerifier{
		cfg:           cfg,
		url:           cfg.JWKSURL,
		keys:          jwk.NewAutoRefresh(ctx),
		retryInterval: min(keysRetryInterval, cfg.MinRefreshInterval),
		refreshing:    make(chan struct{}, 1),
	}
	v.keys.Configure(v.url,
		jwk.WithMinRefreshInterval(cfg.MinRefreshInterval),
		jwk.WithHTTPClient(limitedHTTPClient{client: cfg.HTTPClient}))

	fetchCtx, cancel := context.WithTimeout(ctx, keysFetchTimeout)
	defer cancel()
	set, err := v.keys.Refresh(fetchCtx, v.url)
	if err == nil && set.Len() == 0 {
		err = errors.New("the key set is empty")
	}
	if err != nil {
		slog.Warn("could not load the signing keys of the platform: API requests are refused until they are loaded",
			"url", v.url, "err", err.Error())
	} else {
		v.loaded.Store(true)
	}

	return v, nil
}

// Verify checks a token of the platform and returns the caller that it stands for.
// Every token that is not proven to be valid is refused.
func (v *JWKSVerifier) Verify(ctx context.Context, rawToken string) (*Principal, error) {
	if rawToken == "" {
		return nil, ErrMissingToken
	}

	kid, err := checkTokenHeader(rawToken)
	if err != nil {
		return nil, err
	}

	key, err := v.publicKey(ctx, kid)
	if err != nil {
		return nil, err
	}

	// the algorithm is the one of this verifier, whatever the header of the token says
	payload, err := jws.Verify([]byte(rawToken), jwa.EdDSA, key)
	if err != nil {
		return nil, invalidToken("signature does not verify")
	}

	return v.principal(payload)
}

// checkTokenHeader checks that the token is a compact JWS with one EdDSA signature and
// returns the id of the key that signed it.
func checkTokenHeader(rawToken string) (string, error) {
	if len(rawToken) > maxTokenLength {
		return "", invalidToken("token is too long")
	}
	if !isCompactJWS(rawToken) {
		return "", invalidToken("not a compact JWS")
	}

	msg, err := jws.Parse([]byte(rawToken))
	if err != nil {
		return "", invalidToken("malformed JWS")
	}
	signatures := msg.Signatures()
	if len(signatures) != 1 {
		return "", invalidToken("not exactly one signature")
	}
	header := signatures[0].ProtectedHeaders()
	if header.Algorithm() != jwa.EdDSA {
		return "", invalidToken("algorithm is not EdDSA")
	}
	// an extension that this verifier does not understand must not be ignored
	if _, hasB64 := header.Get("b64"); hasB64 || len(header.Critical()) > 0 {
		return "", invalidToken("unsupported header parameter")
	}
	kid := header.KeyID()
	if kid == "" {
		return "", invalidToken("no key id")
	}
	return kid, nil
}

// isCompactJWS reports whether the token is made of exactly three parts of base64url
// characters, none of them empty: a token without a signature is not one.
func isCompactJWS(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			c := part[i]
			isBase64URL := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
			if !isBase64URL {
				return false
			}
		}
	}
	return true
}

// publicKey returns the Ed25519 key of the platform with the given id.
func (v *JWKSVerifier) publicKey(ctx context.Context, kid string) (ed25519.PublicKey, error) {
	set := v.cachedKeySet(ctx)
	if set == nil || set.Len() == 0 {
		set = v.refreshKeySet(ctx)
	}
	if set == nil || set.Len() == 0 {
		return nil, ErrKeysUnavailable
	}

	key, found := set.LookupKeyID(kid)
	if !found {
		// the platform may have rotated its keys since the key set was loaded
		if set = v.refreshKeySet(ctx); set != nil {
			key, found = set.LookupKeyID(kid)
		}
	}
	if !found {
		return nil, invalidToken("unknown key id")
	}

	if key.KeyType() != jwa.OKP {
		return nil, invalidToken("key is not an OKP key")
	}
	okpKey, ok := key.(jwk.OKPPublicKey)
	if !ok || okpKey.Crv() != jwa.Ed25519 {
		return nil, invalidToken("key is not an Ed25519 key")
	}
	if alg := key.Algorithm(); alg != "" && alg != jwa.EdDSA.String() {
		return nil, invalidToken("key is not for EdDSA")
	}
	if use := key.KeyUsage(); use != "" && use != string(jwk.ForSignature) {
		return nil, invalidToken("key is not for signatures")
	}
	// fails for a private key: the key set must only hold public keys
	var publicKey ed25519.PublicKey
	if err := key.Raw(&publicKey); err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, invalidToken("key is not a valid Ed25519 public key")
	}
	return publicKey, nil
}

// cachedKeySet returns the key set that was loaded last, nil when none was loaded. It
// never sends a request.
func (v *JWKSVerifier) cachedKeySet(ctx context.Context) jwk.Set {
	if !v.loaded.Load() {
		return nil
	}
	// answered from the cache once a key set was loaded
	set, err := v.keys.Fetch(ctx, v.url)
	if err != nil {
		return nil
	}
	return set
}

// refreshKeySet asks the platform for its key set, unless it was asked a short while
// ago: MinRefreshInterval after an answer, the retry interval after a failure. So the
// number of requests does not depend on the number of tokens with an unknown key id or
// on the number of requests while the platform is down. A caller waits for a request
// that is under way. It returns the key set that is cached afterwards, nil when there
// is none.
func (v *JWKSVerifier) refreshKeySet(ctx context.Context) jwk.Set {
	select {
	case v.refreshing <- struct{}{}:
		defer func() { <-v.refreshing }()
	case <-ctx.Done():
		return v.cachedKeySet(ctx)
	}

	now := v.cfg.Clock.Now()
	if now.Before(v.nextRefresh) {
		return v.cachedKeySet(ctx)
	}

	// not bound to the request that asks: when its caller goes away, the callers that
	// wait for this answer still need it
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keysFetchTimeout)
	defer cancel()
	set, err := v.keys.Refresh(fetchCtx, v.url)
	if err == nil && set.Len() == 0 {
		err = errors.New("the key set is empty")
	}
	if err != nil {
		v.nextRefresh = now.Add(v.retryInterval)
		slog.Warn("could not load the signing keys of the platform", "url", v.url, "err", err.Error())
		return v.cachedKeySet(ctx)
	}

	v.loaded.Store(true)
	v.nextRefresh = now.Add(v.cfg.MinRefreshInterval)
	return set
}

// principal checks the claims of a token whose signature was verified and returns the
// caller that they describe.
func (v *JWKSVerifier) principal(payload []byte) (*Principal, error) {
	token := jwt.New()
	if err := json.Unmarshal(payload, token); err != nil {
		return nil, invalidToken("malformed claims")
	}

	if token.Issuer() != v.cfg.Issuer {
		return nil, invalidToken("wrong issuer")
	}
	if !slices.Contains(token.Audience(), v.cfg.Audience) {
		return nil, invalidToken("wrong audience")
	}
	if token.Subject() == "" {
		return nil, invalidToken("no subject")
	}
	if token.JwtID() == "" {
		return nil, invalidToken("no token id")
	}

	if _, ok := token.Get(jwt.ExpirationKey); !ok {
		return nil, invalidToken("no expiry")
	}
	if _, ok := token.Get(jwt.IssuedAtKey); !ok {
		return nil, invalidToken("no issue time")
	}
	now := v.cfg.Clock.Now()
	expiresAt, issuedAt := token.Expiration(), token.IssuedAt()
	if !now.Before(expiresAt.Add(v.cfg.ClockSkew)) {
		return nil, invalidToken("expired")
	}
	if issuedAt.After(now.Add(v.cfg.ClockSkew)) {
		return nil, invalidToken("issued in the future")
	}
	if _, ok := token.Get(jwt.NotBeforeKey); ok && token.NotBefore().After(now.Add(v.cfg.ClockSkew)) {
		return nil, invalidToken("not valid yet")
	}
	if lifetime := expiresAt.Sub(issuedAt); lifetime <= 0 || lifetime > v.cfg.MaxLifetime {
		return nil, invalidToken("lifetime is not within the accepted one")
	}

	p := &Principal{
		Subject: token.Subject(),
		Jti:     token.JwtID(),
	}

	org, ok := stringClaim(token, claimOrg)
	orgId, err := uuid.Parse(org)
	if !ok || err != nil || len(org) != canonicalUUIDLength {
		return nil, invalidToken("org is not a UUID")
	}
	p.OrgId = orgId.String()

	scope, _ := stringClaim(token, claimOrgScope)
	switch OrgScope(scope) {
	case OrgScopeSelf, OrgScopeSubtree:
		p.OrgScope = OrgScope(scope)
	default:
		return nil, invalidToken("unknown org scope")
	}

	if perms, ok := token.Get(claimPerms); ok {
		list, isList := perms.([]interface{})
		if !isList {
			return nil, invalidToken("perms is not a list")
		}
		p.Perms = make([]string, 0, len(list))
		for _, item := range list {
			perm, isString := item.(string)
			if !isString {
				return nil, invalidToken("perms is not a list of strings")
			}
			p.Perms = append(p.Perms, perm)
		}
	}

	if actor, ok := token.Get(claimActor); ok {
		fields, isObject := actor.(map[string]interface{})
		if !isObject {
			return nil, invalidToken("act is not an object")
		}
		if sub, hasSub := fields["sub"]; hasSub {
			p.Actor, ok = sub.(string)
			if !ok {
				return nil, invalidToken("act.sub is not a string")
			}
		}
	}

	return p, nil
}

func stringClaim(token jwt.Token, name string) (string, bool) {
	value, ok := token.Get(name)
	if !ok {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

func invalidToken(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidToken, reason)
}

// limitedHTTPClient stops reading a response after maxKeySetBytes.
type limitedHTTPClient struct {
	client *http.Client
}

func (c limitedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body = struct {
		io.Reader
		io.Closer
	}{io.LimitReader(res.Body, maxKeySetBytes), res.Body}
	return res, nil
}

// DevVerifier is for local runs without a platform: it accepts every request, with any
// token or with none, as the caller in Principal. It must only be used when the
// configuration asks for it ([api.auth] mode = "dev") and the ENVIRONMENT environment
// variable is "dev".
type DevVerifier struct {
	Principal Principal
}

// Verify returns a copy of the fixed caller.
func (v DevVerifier) Verify(context.Context, string) (*Principal, error) {
	p := v.Principal
	p.Perms = slices.Clone(p.Perms)
	return &p, nil
}
