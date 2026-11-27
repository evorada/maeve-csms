// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"net/http"

	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// RequireControlPlane only lets the services of the platform that may manage tenants
// through: the caller that tenancy.Middleware verified must be a service (subject
// "svc:…") and must have the permission tenancy.PermTenantManage. The org of the caller
// is not looked at: the control plane works on every tenant. Any other caller is
// answered with 403, and a request that was not authenticated with 401.
func RequireControlPlane(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := tenancy.FromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, codeUnauthenticated)
			return
		}
		if !p.IsService() || !p.Has(tenancy.PermTenantManage) {
			slog.Warn("control plane request refused", "method", r.Method, "path", r.URL.Path,
				"sub", p.Subject, "act", p.Actor, "org", p.OrgId, "jti", p.Jti)
			writeError(w, http.StatusForbidden, codeForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
