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
	codeInvalidConfig    = "invalid_config"
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
	// code is the error of the answer; codeInvalidField when empty
	code string
	// details are all the fields that are not valid, when there can be more than one
	details []ErrorDetail
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

// writeProblem answers a request that cannot be done with a fixed code, the field of the
// request that it is about (none when empty) and why, for the caller.
func writeProblem(w http.ResponseWriter, status int, code, field, message string) {
	answer := Error{Status: http.StatusText(status), Error: &code}
	if field != "" {
		answer.Field = &field
	}
	if message != "" {
		answer.Message = &message
	}
	writeJSON(w, status, answer)
}

func writeInvalid(w http.ResponseWriter, problem *invalidField) {
	code := codeInvalidField
	if problem.code != "" {
		code = problem.code
	}
	answer := Error{
		Status:  http.StatusText(http.StatusBadRequest),
		Error:   &code,
		Field:   &problem.field,
		Message: &problem.message,
	}
	if len(problem.details) > 0 {
		answer.Details = &problem.details
	}
	writeJSON(w, http.StatusBadRequest, answer)
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
