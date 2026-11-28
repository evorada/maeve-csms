// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// ActorMiddleware names the caller of a request (the subject of the principal that
// tenancy.Middleware verified) as the actor of the domain events that the request
// causes. It belongs after tenancy.Middleware. A request without a caller goes on
// unchanged.
func ActorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := tenancy.FromContext(r.Context()); ok && p.Subject != "" {
			r = r.WithContext(events.WithActor(r.Context(), p.Subject))
		}
		next.ServeHTTP(w, r)
	})
}
