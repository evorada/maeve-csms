// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"

	"golang.org/x/exp/slog"
)

// The values of Error.Error: what went wrong, for the program that called.
const (
	codeUnauthenticated  = "unauthenticated"
	codeForbidden        = "forbidden"
	codeInvalidField     = "invalid_field"
	codeTooLarge         = "too_large"
	codeNotFound         = "not_found"
	codeMethodNotAllowed = "method_not_allowed"
	codeTenantNotFound   = "tenant_not_found"
	codeImmutableField   = "immutable_field"
	codeConflict         = "conflict"
	codeNotImplemented   = "not_implemented"
	codeInternal         = "internal_error"
)

// invalidField says which field of a request is not valid and why. The message is for
// the caller: it must only repeat what the caller sent and the rule that it broke.
type invalidField struct {
	field   string
	message string
}

func invalid(field, message string) *invalidField {
	return &invalidField{field: field, message: message}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		slog.Error("control plane: encoding a response", "error", err)
		status, payload = http.StatusInternalServerError, []byte(`{"status":"Internal Server Error","error":"`+codeInternal+`"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

// writeError answers with the text of the status and a fixed code, never with the text
// of an error of the manager.
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, Error{Status: http.StatusText(status), Error: &code})
}

func writeInvalid(w http.ResponseWriter, problem *invalidField) {
	code := codeInvalidField
	writeJSON(w, http.StatusBadRequest, Error{
		Status:  http.StatusText(http.StatusBadRequest),
		Error:   &code,
		Field:   &problem.field,
		Message: &problem.message,
	})
}

// parameterError answers a request whose parameters the generated router could not
// read.
func parameterError(w http.ResponseWriter, _ *http.Request, err error) {
	var name string
	var (
		format   *InvalidParamFormatError
		tooMany  *TooManyValuesForParamError
		required *RequiredParamError
		header   *RequiredHeaderError
	)
	switch {
	case errors.As(err, &format):
		name = format.ParamName
	case errors.As(err, &tooMany):
		name = tooMany.ParamName
	case errors.As(err, &required):
		name = required.ParamName
	case errors.As(err, &header):
		name = header.ParamName
	}
	if name == "orgId" {
		name = fieldOrgId
	}
	writeInvalid(w, invalid(name, "cannot be read"))
}
