// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"k8s.io/utils/clock"
)

// actorOf returns the actor that the request reaches the next handler with, and the
// request itself.
func actorOf(req *http.Request) (string, *http.Request) {
	var actor string
	var seen *http.Request
	api.ActorMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		actor, seen = events.ActorFrom(r.Context()), r
	})).ServeHTTP(httptest.NewRecorder(), req)
	return actor, seen
}

func TestActorMiddlewareNamesTheCallerAsTheActor(t *testing.T) {
	for _, subject := range []string{"user:alice", "machine:ci", "svc:provisioner"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(tenancy.WithPrincipal(req.Context(), &tenancy.Principal{Subject: subject, Actor: "user:support"}))
		actor, _ := actorOf(req)
		assert.Equal(t, subject, actor)
	}
}

func TestActorMiddlewareLeavesARequestWithoutACallerAsItIs(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	actor, seen := actorOf(req)
	assert.Empty(t, actor)
	assert.Same(t, req, seen)

	// an actor that the context has already stays
	req = req.WithContext(events.WithActor(req.Context(), "svc:maeve"))
	actor, seen = actorOf(req)
	assert.Equal(t, "svc:maeve", actor)
	assert.Same(t, req, seen)
}

func TestNewServerNeedsAnEmitterOfDomainEvents(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	_, err := api.NewServer(engine, clock.RealClock{}, nil, nil)
	assert.Error(t, err)
}
