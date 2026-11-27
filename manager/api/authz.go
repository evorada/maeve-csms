// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// permissionExtension is the field of an operation in the API spec that names the
// permission that a caller needs for the operation.
const permissionExtension = "x-permission"

// stationParam is the path parameter that holds the station key of every operation on
// a charge station.
const stationParam = "csId"

// registerStationOperation is the one operation that works on a charge station that
// does not exist yet.
const registerStationOperation = http.MethodPost + " /cs/{" + stationParam + "}"

// BuildPermissionIndex maps every operation of the spec, as "METHOD /spec/path", to
// the permission in its x-permission field. It fails when an operation has no
// permission or one that is not in tenancy.AllPermissions.
func BuildPermissionIndex(swagger *openapi3.T) (map[string]string, error) {
	if swagger == nil || swagger.Paths == nil {
		return nil, errors.New("api spec has no paths")
	}

	index := make(map[string]string)
	var problems []string
	for path, item := range swagger.Paths.Map() {
		for method, operation := range item.Operations() {
			key := method + " " + path
			permission, ok := operation.Extensions[permissionExtension].(string)
			switch {
			case !ok || permission == "":
				problems = append(problems, fmt.Sprintf("%s has no %s", key, permissionExtension))
			case !slices.Contains(tenancy.AllPermissions, permission):
				problems = append(problems, fmt.Sprintf("%s has the unknown permission %q", key, permission))
			default:
				index[key] = permission
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("api spec: %s", strings.Join(problems, "; "))
	}
	return index, nil
}

// AuthzMiddleware lets a request through to its operation when the caller has the
// permission of the operation and, for an operation on a charge station, may reach the
// org that owns the charge station. It is meant for ChiServerOptions.Middlewares, so
// that it runs for every operation after the route is known. basePath is where the API
// is mounted ("/api/v0", or "" when it is mounted at the root).
//
// Everything that is not allowed is refused: a request without a caller (401), an
// operation that is not in the index or whose permission the caller does not have
// (403), and a charge station that the caller may not reach, which is answered exactly
// like a charge station that does not exist (404). The org of the charge station is
// read from the store, never from the request, and before anything else happens.
//
// The next handler finds the org of the charge station in its context
// (tenancy.StationOrgFromContext) and a scope cache (tenancy.WithScopeCache).
func AuthzMiddleware(scoper *tenancy.Scoper, index map[string]string, basePath string) MiddlewareFunc {
	if scoper == nil {
		panic("api: AuthzMiddleware needs a scoper")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			p, ok := tenancy.FromContext(ctx)
			if !ok {
				_ = render.Render(w, r, ErrUnauthorized)
				return
			}

			operation, ok := routeOperation(r, basePath)
			if !ok {
				_ = render.Render(w, r, ErrForbidden("operation not permitted"))
				return
			}
			permission, ok := index[operation]
			if !ok || permission == "" {
				_ = render.Render(w, r, ErrForbidden("operation not permitted"))
				return
			}
			if !p.Has(permission) {
				_ = render.Render(w, r, ErrForbidden("missing permission "+permission))
				return
			}

			ctx = tenancy.WithScopeCache(ctx)

			if strings.Contains(operation, "{"+stationParam+"}") {
				// the same value that the operation gets: the generated code unescapes
				// the path parameter in this way
				csId, err := url.PathUnescape(chi.URLParam(r, stationParam))
				if err != nil || csId == "" {
					_ = render.Render(w, r, ErrNotFound)
					return
				}

				orgId, exists, err := scoper.StationOrg(ctx, csId)
				if err != nil {
					authzFailed(w, r, err)
					return
				}
				register := operation == registerStationOperation
				if orgId == "" || (!exists && !register) {
					_ = render.Render(w, r, ErrNotFound)
					return
				}
				inScope, err := scoper.InScope(ctx, p, orgId)
				if err != nil {
					authzFailed(w, r, err)
					return
				}
				if !inScope {
					_ = render.Render(w, r, ErrNotFound)
					return
				}
				if register && !exists && scoper.Mode == tenancy.ModeMulti {
					// only a caller that may reach the tenant learns what is wrong
					// with the id
					_, stationId, _ := tenancy.SplitStationKey(csId)
					if err := tenancy.ValidateStationId(stationId); err != nil {
						_ = render.Render(w, r, ErrInvalidRequest(err))
						return
					}
				}

				ctx = tenancy.WithStationOrg(ctx, orgId)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// routeOperation returns the operation that the request was routed to, as
// "METHOD /spec/path". It reports false when the route is not below basePath.
func routeOperation(r *http.Request, basePath string) (string, bool) {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "", false
	}
	pattern := rctx.RoutePattern()
	if basePath != "" {
		var found bool
		pattern, found = strings.CutPrefix(pattern, basePath)
		if !found || !strings.HasPrefix(pattern, "/") {
			return "", false
		}
	}
	return r.Method + " " + pattern, true
}

// authzFailed answers a request that could not be authorised because a lookup failed.
// The reason goes to the log, not to the caller.
func authzFailed(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("authorising request", "method", r.Method, "path", r.URL.Path, "err", err)
	_ = render.Render(w, r, errInternal)
}
