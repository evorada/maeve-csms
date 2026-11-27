// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/controlplane"
	"github.com/thoughtworks/maeve-csms/manager/server"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// A charge station that a person registers through the API is replayed to the platform
// through the control plane, as the event of the person, and only to the org that owns
// the charge station.
func TestTheEventsOfTheApiAreReplayedThroughTheControlPlane(t *testing.T) {
	ctx := context.Background()
	engine := inmemory.NewStore(clock.RealClock{})
	for orgId, orgKey := range map[string]string{orgA: "aaaa", orgB: "bbbb"} {
		require.NoError(t, engine.PutTenant(ctx, &store.Tenant{OrgId: orgId, Path: store.OrgPathLabel(orgId), Slug: "tenant-" + orgKey, OrgKey: orgKey}))
	}
	// one resolver and one scoper behind both listeners, as in the manager
	resolver := &tenancy.Resolver{Mode: tenancy.ModeMulti, Tenants: engine, Stations: engine, DefaultOrgId: store.LegacyOrgId}
	platform, verifier := newPlatformVerifier(t)
	internal := server.NewInternalHandler(server.InternalDeps{Resolver: resolver, Engine: engine, Verifier: verifier, Scoper: scoperOf(resolver)})
	apiHandler := server.NewApiHandler(server.ApiDeps{
		Engine:   engine,
		Verifier: verifier,
		Scoper:   scoperOf(resolver),
		Events:   eventsOf(engine, resolver),
	})

	// alice is a person of tenant A
	rr := request(apiHandler, http.MethodPost, "/api/v0/cs/aaaa.NEW", tokenOf(platform, orgA, tenancy.PermStationWrite), `{"securityProfile":1}`)
	require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
	// the relay publishes it
	published, err := engine.RelayOutboxEvents(ctx, 10, func(_ context.Context, evs []*store.OutboxEvent) (int, error) {
		return len(evs), nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, published)

	replay := func(orgId string) []map[string]any {
		t.Helper()
		claims := platform.Claims(time.Now())
		claims["sub"] = "svc:events"
		claims["org"] = orgId
		claims["org_scope"] = "subtree"
		claims["perms"] = []string{tenancy.PermTenantManage}
		rr := get(internal, controlplane.BasePath+"/events?type=station.registered", platform.Sign(claims))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var page struct {
			Events []map[string]any `json:"events"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
		return page.Events
	}

	evs := replay(orgA)
	require.Len(t, evs, 1)
	assert.Equal(t, "cpms.station.registered", evs[0]["type"])
	assert.Equal(t, orgA, evs[0]["orgid"])
	assert.Equal(t, "urn:cpms:"+orgA+":station/aaaa.NEW", evs[0]["subject"])
	// the subject of the token of the caller of the API
	assert.Equal(t, "user:alice", evs[0]["actor"])
	assert.Equal(t, map[string]any{"station_key": "aaaa.NEW", "station_id": "NEW", "security_profile": float64(1)}, evs[0]["data"])

	assert.Empty(t, replay(orgB))
}

func TestApiHandlerNeedsAnEmitterOfDomainEvents(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	assert.PanicsWithValue(t, "server: the api handler needs an emitter of domain events", func() {
		server.NewApiHandler(server.ApiDeps{Engine: engine, Verifier: devVerifier, Scoper: scoperOf(&tenancy.Resolver{Stations: engine})})
	})
}

func TestInternalHandlerNeedsAScoper(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	assert.PanicsWithValue(t, "server: the internal handler needs a scoper", func() {
		server.NewInternalHandler(server.InternalDeps{Resolver: &tenancy.Resolver{Stations: engine}, Engine: engine, Verifier: devVerifier})
	})
}
