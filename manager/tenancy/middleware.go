// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"errors"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

const bearerScheme = "Bearer"

// The answers to a request that is not authenticated, in the shape of the errors of
// the API. They do not say which check a token failed.
const (
	unauthenticatedBody = `{"status":"Unauthorized","error":"missing bearer token"}`
	refusedBody         = `{"status":"Unauthorized","error":"invalid token"}`
	unavailableBody     = `{"status":"Service Unavailable","error":"auth keys unavailable"}`
)

// Middleware verifies the token of "Authorization: Bearer <token>" with v and stores
// the caller in the context of the request (see FromContext). A request that v does not
// accept never reaches the next handler: it is answered with 401, or with 503 while the
// keys to verify tokens with cannot be loaded.
func Middleware(v Verifier) func(http.Handler) http.Handler {
	if v == nil {
		panic("tenancy: Middleware needs a verifier")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := v.Verify(r.Context(), bearerToken(r))
			if err == nil && p == nil {
				err = ErrInvalidToken
			}
			if err != nil {
				refuse(w, r, err)
				return
			}

			trace.SpanFromContext(r.Context()).SetAttributes(
				attribute.String("auth.sub", p.Subject),
				attribute.String("auth.act", p.Actor),
				attribute.String("auth.org", p.OrgId),
				attribute.String("auth.jti", p.Jti))

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// bearerToken returns the token of the Authorization header, "" when the request does
// not have exactly one Authorization header with the Bearer scheme.
func bearerToken(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return ""
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return ""
	}
	return strings.TrimLeft(token, " ")
}

// refuse answers a request that was not authenticated. Why a token was refused goes to
// the log, not to the caller; the token itself goes nowhere.
func refuse(w http.ResponseWriter, r *http.Request, err error) {
	status, body := http.StatusUnauthorized, refusedBody
	switch {
	case errors.Is(err, ErrKeysUnavailable):
		status, body = http.StatusServiceUnavailable, unavailableBody
	case errors.Is(err, ErrMissingToken):
		body = unauthenticatedBody
	}

	slog.Info("request not authenticated", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr, "reason", err.Error())

	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
