// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// The query parameters of ListEvents, as named in a 400 answer.
const (
	fieldAfter = "after"
	fieldLimit = "limit"
	fieldType  = "type"
)

// ListEvents replays the published domain events of the orgs that the caller may reach,
// after a cursor (the published sequence of the last event that the caller has).
func (s *Server) ListEvents(w http.ResponseWriter, r *http.Request, params ListEventsParams) {
	after := int64(0)
	if params.After != nil {
		var err error
		after, err = strconv.ParseInt(*params.After, 10, 64)
		if err != nil || after < 0 {
			writeInvalid(w, invalid(fieldAfter, "must be a cursor that a page of events returned"))
			return
		}
	}

	limit := store.DefaultOutboxEventListLimit
	if params.Limit != nil {
		limit = *params.Limit
		if limit < 1 || limit > store.MaxOutboxEventListLimit {
			writeInvalid(w, invalid(fieldLimit, "must be between 1 and "+strconv.Itoa(store.MaxOutboxEventListLimit)))
			return
		}
	}

	var types []string
	if params.Type != nil {
		for _, name := range *params.Type {
			eventType, ok := shortEventType(name)
			if !ok {
				writeInvalid(w, invalid(fieldType, "must be a type of the manifest"))
				return
			}
			types = append(types, eventType)
		}
	}

	if s.Outbox == nil {
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
		return
	}
	// the guard in front of the handler only lets a verified caller through
	p, ok := tenancy.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthenticated)
		return
	}
	if s.Scoper == nil {
		slog.Error("control plane: no scoper for the events")
		writeError(w, http.StatusInternalServerError, codeInternal)
		return
	}
	orgIds, err := s.Scoper.OrgIds(r.Context(), p)
	if err != nil {
		s.readError(w, "find the orgs of the caller", err)
		return
	}

	page := EventPage{Events: []CloudEvent{}, NextCursor: strconv.FormatInt(after, 10)}
	// an empty list of orgs would select the events of every org
	if len(orgIds) == 0 {
		writeJSON(w, http.StatusOK, page)
		return
	}

	listed, err := s.Outbox.ListOutboxEvents(r.Context(), store.OutboxEventFilter{
		AfterPublishedSeq: after,
		OrgIds:            orgIds,
		Types:             types,
		Limit:             limit,
	})
	if err != nil {
		s.readError(w, "list the events", err)
		return
	}
	for _, ev := range listed {
		page.Events = append(page.Events, events.ToCloudEvent(ev, events.Product))
	}
	if len(listed) > 0 {
		page.NextCursor = strconv.FormatInt(listed[len(listed)-1].PublishedSeq, 10)
	}
	writeJSON(w, http.StatusOK, page)
}

// shortEventType returns the type of the catalogue that a type parameter names, with or
// without the product: "session.completed" and "cpms.session.completed" are both
// "session.completed".
func shortEventType(name string) (string, bool) {
	eventType := strings.TrimPrefix(name, events.Product+".")
	return eventType, slices.Contains(events.AllTypes, eventType)
}

// readError answers a read (of events, of an inventory) that the store could not serve.
// What the store said only goes to the log.
func (s *Server) readError(w http.ResponseWriter, action string, err error) {
	if errors.Is(err, store.ErrNotImplemented) {
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
		return
	}
	slog.Error("control plane: "+action, "error", err)
	writeError(w, http.StatusInternalServerError, codeInternal)
}
