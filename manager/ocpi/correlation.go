// SPDX-License-Identifier: Apache-2.0

package ocpi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ContextKey string

const (
	ContextKeyCorrelationId ContextKey = "correlation_id"
)

func CorrelationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.Header.Get("X-Correlation-Id")
		if id == "" {
			id = uuid.New().String()
		}
		ctx = context.WithValue(ctx, ContextKeyCorrelationId, id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Correlation-Id", id)
		next.ServeHTTP(w, r)
	})
}
