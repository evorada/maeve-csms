// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/go-chi/render"
)

func ValidationMiddleware(next http.Handler) http.Handler {
	swagger, err := GetSwagger()
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		matchedPath, pathParams := matchPath(r, swagger)
		if matchedPath != nil {
			pathItem := swagger.Paths.Find(*matchedPath)
			operation := pathItem.Operations()[r.Method]
			if operation != nil {
				requestValidationInput := &openapi3filter.RequestValidationInput{
					Request:    r,
					PathParams: pathParams,
					Route: &routers.Route{
						Spec:      swagger,
						Server:    nil,
						Path:      *matchedPath,
						PathItem:  pathItem,
						Method:    r.Method,
						Operation: operation,
					},
					// callers are authenticated before this middleware, by the token
					// verifier of the server: the security requirements of the spec are
					// not checked a second time here
					Options: &openapi3filter.Options{
						AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
					},
				}
				err := openapi3filter.ValidateRequest(r.Context(), requestValidationInput)
				if err != nil {
					_ = render.Render(w, r, ErrInvalidRequest(err))
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func matchPath(r *http.Request, swagger *openapi3.T) (*string, map[string]string) {
	pathParams := make(map[string]string)
	requestElements := strings.Split(r.RequestURI, "/")

	for _, path := range swagger.Paths.InMatchingOrder() {
		match := true
		pathElements := strings.Split(path, "/")
		if len(requestElements) == len(pathElements) {
			for i := 0; i < len(requestElements); i++ {
				pe := pathElements[i]
				if name, value, ok := matchParamSegment(pe, requestElements[i]); ok {
					pathParams[name] = value
					continue
				}
				if pe != requestElements[i] {
					match = false
					break
				}
			}

			if match {
				return &path, pathParams
			}
		}
	}

	return nil, nil
}

// matchParamSegment matches a segment of a request path to a segment of a spec path
// that is a path parameter: "{name}", which takes the whole segment, or "{name}:verb",
// which takes the segment up to a literal ":verb" at its end (as in
// "/commands/{commandId}:cancel"). It reports false when the spec segment is no
// parameter or the request segment does not end with the verb.
func matchParamSegment(specSegment, requestSegment string) (name, value string, ok bool) {
	if !strings.HasPrefix(specSegment, "{") {
		return "", "", false
	}
	name, verb, found := strings.Cut(specSegment[1:], "}")
	if !found || name == "" {
		return "", "", false
	}
	if verb == "" {
		return name, requestSegment, true
	}
	if !strings.HasPrefix(verb, ":") {
		return "", "", false
	}
	value, found = strings.CutSuffix(requestSegment, verb)
	if !found || value == "" {
		return "", "", false
	}
	return name, value, true
}
