// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/entitlements"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// BasePath is where the control plane is mounted on the internal listener.
const BasePath = "/_control/v1"

// healthCheckTimeout is how long a check of GetHealth may take.
const healthCheckTimeout = 2 * time.Second

// HealthChecker is a storage engine that can say whether it works.
type HealthChecker interface {
	Health(ctx context.Context) error
}

// Server implements the operations of the control plane.
type Server struct {
	Tenants store.TenantStore
	// Resolver is told about every change of a tenant, so that it does not keep serving
	// what it cached. It may be nil.
	Resolver *tenancy.Resolver
	// Store is nil when the storage engine has no health check.
	Store HealthChecker
	// Mode is ModeSingle when empty.
	Mode tenancy.Mode
	// Clock times the health checks. Nil is the real clock.
	Clock clock.PassiveClock
	// Outbox keeps the published domain events that ListEvents replays. Nil answers 501.
	Outbox store.OutboxStore
	// Scoper says which orgs a caller may read the domain events of: the org of its token
	// and, with the org scope subtree, the orgs below it.
	Scoper *tenancy.Scoper
	// Stations keeps the charge stations and their assets that a transfer moves. Nil
	// answers a transfer with 501.
	Stations StationStore
	// Events stores the domain events of a transfer, in its store transaction. A transfer
	// needs it.
	Events events.Emitter
	// Entitlements checks the connector limit of the org that gets a charge station. Nil
	// limits nothing.
	Entitlements *entitlements.Enforcer
	// Commands cancels the commands that are queued for a charge station that is
	// transferred. Nil when the storage keeps no commands.
	Commands *commands.Service
}

var _ ServerInterface = (*Server)(nil)

// NewHandler returns the handler of the control plane, to be mounted at BasePath behind
// tenancy.Middleware. Every request to it, also to a path that it does not serve, goes
// through RequireControlPlane first.
func NewHandler(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(RequireControlPlane)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, codeNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed)
	})
	// the URN of a resource that a client sends with ":" or "/" that are not encoded does
	// not fit the route of the spec
	r.Post(resourcesPath+"*", s.resourceAction)
	return HandlerWithOptions(s, ChiServerOptions{BaseRouter: r, ErrorHandlerFunc: parameterError})
}

func (s *Server) GetManifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, BuildManifest())
}

func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	health := Health{
		Status: HealthStatusOk,
		Mode:   HealthModeSingle,
		Checks: map[string]HealthCheck{},
	}
	if s.Mode == tenancy.ModeMulti {
		health.Mode = HealthModeMulti
	}
	if s.Store != nil {
		check := s.checkStore(r.Context())
		health.Checks["store"] = check
		if check.Status != HealthStatusOk {
			health.Status = HealthStatusDown
		}
	}

	status := http.StatusOK
	if health.Status != HealthStatusOk {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, health)
}

// checkStore asks the storage engine whether it works. Why it does not goes to the log:
// the caller only learns whether the check ran out of time.
func (s *Server) checkStore(ctx context.Context) HealthCheck {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	start := s.now()
	err := s.Store.Health(ctx)
	check := HealthCheck{Status: HealthStatusOk, LatencyMs: s.now().Sub(start).Milliseconds()}
	if err != nil {
		slog.Error("control plane: store health check failed", "error", err)
		reason := "unavailable"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "timeout"
		}
		check.Status = HealthStatusDown
		check.Error = &reason
	}
	return check
}

func (s *Server) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}
