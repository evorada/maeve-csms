// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

// fieldCursor is the query parameter of GetInventory with the cursor of a page, as named
// in a 400 answer.
const fieldCursor = "cursor"

// GetInventory returns how many resources of a type the org of a tenant owns itself and a
// page of them, with their URNs. The caller must be able to read charge stations and its
// token must reach the org: the org of the token or, with the org scope subtree, a tenant
// below it. An org that the token does not reach is answered like a tenant that does not
// exist.
func (s *Server) GetInventory(w http.ResponseWriter, r *http.Request, orgId OrgId, params GetInventoryParams) {
	// the guard in front of the handler only lets a verified caller through
	p, ok := tenancy.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, codeUnauthenticated)
		return
	}
	if !p.Has(tenancy.PermStationRead) {
		slog.Warn("control plane: inventory refused without "+tenancy.PermStationRead, "sub", p.Subject, "jti", p.Jti)
		writeError(w, http.StatusForbidden, codeForbidden)
		return
	}
	id, ok := canonicalOrgId(orgId)
	if !ok {
		writeInvalid(w, invalid(fieldOrgId, "must be a UUID"))
		return
	}
	resourceType := string(params.Type)
	if !slices.Contains(store.InventoryTypes, resourceType) {
		writeInvalid(w, invalid(fieldType, "must be one of "+strings.Join(store.InventoryTypes, ", ")))
		return
	}
	page := store.PageRequest{Limit: store.DefaultInventoryPageLimit}
	if params.Limit != nil {
		page.Limit = *params.Limit
		if page.Limit < 1 || page.Limit > store.MaxInventoryPageLimit {
			writeInvalid(w, invalid(fieldLimit, "must be between 1 and "+strconv.Itoa(store.MaxInventoryPageLimit)))
			return
		}
	}
	if params.Cursor != nil {
		if _, err := store.DecodeKeyCursor(*params.Cursor); err != nil {
			writeInvalid(w, invalid(fieldCursor, "must be a cursor that a page of the inventory returned"))
			return
		}
		page.Cursor = *params.Cursor
	}
	includeInactive := params.IncludeInactive != nil && *params.IncludeInactive

	if s.Inventory == nil || s.Tenants == nil {
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
		return
	}
	if s.Scoper == nil {
		slog.Error("control plane: no scoper for the inventory")
		writeError(w, http.StatusInternalServerError, codeInternal)
		return
	}
	ctx := r.Context()
	reachable, err := s.Scoper.InScope(ctx, p, id)
	if err != nil {
		s.readError(w, "find the orgs of the caller", err)
		return
	}
	if !reachable {
		writeError(w, http.StatusNotFound, codeTenantNotFound)
		return
	}
	tenant, err := s.Tenants.GetTenant(ctx, id)
	if err != nil {
		s.storeError(w, "get tenant", id, err)
		return
	}
	if tenant == nil {
		writeError(w, http.StatusNotFound, codeTenantNotFound)
		return
	}

	asOf := s.now().UTC()
	count, err := s.Inventory.CountInventory(ctx, id, resourceType, includeInactive)
	if err != nil {
		s.readError(w, "count the inventory", err)
		return
	}
	items, nextCursor, err := s.Inventory.ListInventory(ctx, id, resourceType, includeInactive, page)
	if err != nil {
		s.readError(w, "list the inventory", err)
		return
	}

	answer := Inventory{
		OrgId: id,
		Type:  InventoryType(resourceType),
		AsOf:  asOf,
		Count: count,
		Items: make([]InventoryItem, len(items)),
	}
	for i, item := range items {
		attrs := item.Attrs
		if attrs == nil {
			attrs = map[string]string{}
		}
		answer.Items[i] = InventoryItem{
			Id:    item.Id,
			Urn:   URN{Product: events.Product, OrgId: id, Type: resourceType, Id: item.Id}.String(),
			Attrs: attrs,
		}
	}
	if nextCursor != "" {
		answer.NextCursor = &nextCursor
	}
	writeJSON(w, http.StatusOK, answer)
}
