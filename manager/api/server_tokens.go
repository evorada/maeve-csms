// SPDX-License-Identifier: Apache-2.0

package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/render"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
)

func (s *Server) SetToken(w http.ResponseWriter, r *http.Request) {
	req := new(Token)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	normContractId, err := ocpp.NormalizeEmaid(req.ContractId)
	if err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	orgId, ok := s.targetOrg(w, r, req.OrgId)
	if !ok {
		return
	}

	err = s.store.SetToken(r.Context(), &store.Token{
		CountryCode:  req.CountryCode,
		PartyId:      req.PartyId,
		Type:         string(req.Type),
		Uid:          req.Uid,
		ContractId:   normContractId,
		VisualNumber: req.VisualNumber,
		Issuer:       req.Issuer,
		GroupId:      req.GroupId,
		Valid:        req.Valid,
		LanguageCode: req.LanguageCode,
		CacheMode:    string(req.CacheMode),
		LastUpdated:  s.clock.Now().Format(time.RFC3339),
		ExpiresAt:    req.ExpiresAt,
		OrgId:        orgId,
	})
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) ListTokens(w http.ResponseWriter, r *http.Request, params ListTokensParams) {
	offset := 0
	limit := 20

	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit > 100 {
		limit = 100
	}

	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	orgIds, err := s.scoper.OrgIds(r.Context(), p)
	if err != nil {
		renderScopeError(w, r, err)
		return
	}
	if params.OrgId != nil {
		// narrow the list to one of the orgs that the caller may reach
		requested := params.OrgId.String()
		if slices.Contains(orgIds, requested) {
			orgIds = []string{requested}
		} else {
			orgIds = nil
		}
	}

	var tokens []*store.Token
	if len(orgIds) > 0 {
		tokens, err = s.store.ListTokensForOrgs(r.Context(), orgIds, offset, limit)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	}

	var resp = make([]render.Renderer, len(tokens))
	for i, tok := range tokens {
		resp[i], err = newToken(tok)
		if err != nil {
			_ = render.Render(w, r, ErrInternalError(err))
			return
		}
	}
	_ = render.RenderList(w, r, resp)
}

func (s *Server) LookupToken(w http.ResponseWriter, r *http.Request, tokenUid string, params LookupTokenParams) {
	p, ok := s.caller(w, r)
	if !ok {
		return
	}
	orgId := p.OrgId
	if params.OrgId != nil {
		orgId = params.OrgId.String()
	}
	// a token of an org that the caller may not reach is answered like a token that
	// does not exist
	inScope, err := s.scoper.InScope(r.Context(), p, orgId)
	if err != nil {
		renderScopeError(w, r, err)
		return
	}
	if !inScope {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	tok, err := s.store.LookupTokenForOrg(r.Context(), strings.ToLower(orgId), tokenUid)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	if tok == nil {
		_ = render.Render(w, r, ErrNotFound)
		return
	}

	resp, err := newToken(tok)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	_ = render.Render(w, r, resp)
}

// DeleteToken deletes the token with the UID of the org of the caller, or of the
// requested org. Deleting a token that does not exist succeeds as well.
func (s *Server) DeleteToken(w http.ResponseWriter, r *http.Request, tokenUid string, params DeleteTokenParams) {
	// the org must be one that the caller may write to: out of its scope is 404, a
	// suspended tenant 403
	orgId, ok := s.targetOrg(w, r, params.OrgId)
	if !ok {
		return
	}
	if err := s.store.DeleteToken(r.Context(), orgId, tokenUid); err != nil {
		if errors.Is(err, store.ErrNotImplemented) {
			_ = render.Render(w, r, errTokenDeleteNotAvailable)
			return
		}
		slog.Error("deleting a token", "org", orgId, "err", err)
		_ = render.Render(w, r, errInternal)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// errTokenDeleteNotAvailable answers the deletion of a token by a manager whose storage
// engine cannot delete the token of one org.
var errTokenDeleteNotAvailable = &ErrResponse{
	HTTPStatusCode: http.StatusNotImplemented,
	StatusText:     http.StatusText(http.StatusNotImplemented),
	ErrorText:      "the storage engine of the manager cannot delete the token of one org",
}

func newToken(tok *store.Token) (*Token, error) {
	lastUpdated, err := time.Parse(time.RFC3339, tok.LastUpdated)
	if err != nil {
		return nil, err
	}

	// the org is not known to every storage backend
	var orgId *openapi_types.UUID
	if tok.OrgId != "" {
		id, err := uuid.Parse(tok.OrgId)
		if err != nil {
			return nil, fmt.Errorf("token org id: %w", err)
		}
		orgId = &id
	}

	return &Token{
		CountryCode:  tok.CountryCode,
		PartyId:      tok.PartyId,
		Type:         TokenType(tok.Type),
		Uid:          tok.Uid,
		ContractId:   tok.ContractId,
		VisualNumber: tok.VisualNumber,
		Issuer:       tok.Issuer,
		GroupId:      tok.GroupId,
		Valid:        tok.Valid,
		LanguageCode: tok.LanguageCode,
		CacheMode:    TokenCacheMode(tok.CacheMode),
		LastUpdated:  &lastUpdated,
		ExpiresAt:    tok.ExpiresAt,
		OrgId:        orgId,
	}, nil
}

// Render implementations

func (t Token) Bind(r *http.Request) error {
	return nil
}

func (t Token) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
