// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"

	"github.com/go-chi/render"
)

type ErrResponse struct {
	Err            error `json:"-"` // low-level runtime error
	HTTPStatusCode int   `json:"-"` // http response status code

	StatusText string `json:"status"`          // user-level status message
	AppCode    int64  `json:"code,omitempty"`  // application-specific error code
	ErrorText  string `json:"error,omitempty"` // application-level error message, for debugging
}

func (e *ErrResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}

func ErrInvalidRequest(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: http.StatusBadRequest,
		StatusText:     http.StatusText(http.StatusBadRequest),
		ErrorText:      err.Error(),
	}
}

func ErrInternalError(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: http.StatusInternalServerError,
		StatusText:     http.StatusText(http.StatusInternalServerError),
		ErrorText:      err.Error(),
	}
}

// ErrForbidden answers a caller that is known but may not do what it asked for.
func ErrForbidden(msg string) render.Renderer {
	return &ErrResponse{
		HTTPStatusCode: http.StatusForbidden,
		StatusText:     http.StatusText(http.StatusForbidden),
		ErrorText:      msg,
	}
}

// ErrLimitExceeded answers a request to create something that a hard limit of the
// entitlements of the tenant does not allow. The error starts with
// entitlement_limit_exceeded and names the limit.
func ErrLimitExceeded(err error) render.Renderer {
	return &ErrResponse{
		Err:            err,
		HTTPStatusCode: http.StatusForbidden,
		StatusText:     http.StatusText(http.StatusForbidden),
		ErrorText:      err.Error(),
	}
}

// ErrConflict answers a request that clashes with something that exists.
func ErrConflict(msg string) render.Renderer {
	return &ErrResponse{
		HTTPStatusCode: http.StatusConflict,
		StatusText:     http.StatusText(http.StatusConflict),
		ErrorText:      msg,
	}
}

// ErrUnauthorized answers a request without a verified caller.
var ErrUnauthorized = &ErrResponse{
	HTTPStatusCode: http.StatusUnauthorized,
	StatusText:     http.StatusText(http.StatusUnauthorized),
}

// errInternal answers a failure whose reason is not for the caller.
var errInternal = &ErrResponse{
	HTTPStatusCode: http.StatusInternalServerError,
	StatusText:     http.StatusText(http.StatusInternalServerError),
}

var ErrNotFound = &ErrResponse{
	HTTPStatusCode: http.StatusNotFound,
	StatusText:     http.StatusText(http.StatusNotFound),
}
