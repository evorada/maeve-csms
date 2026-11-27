// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"golang.org/x/exp/slog"
)

const (
	// maxTenantDocumentBytes is the largest body of a PUT of a tenant.
	maxTenantDocumentBytes = 1 << 20
	// maxHostnames is how many hostnames a tenant may have.
	maxHostnames = 20
	// maxPathLabels is how deep in the org tree a tenant may be.
	maxPathLabels = 64
	// maxPartyRecordIdLength is the longest id of a party of a tenant, in characters.
	maxPartyRecordIdLength = 64
)

// The names of the fields in the answers to requests that are not valid.
const (
	fieldBody   = "body"
	fieldOrgId  = "org_id"
	fieldPath   = "path"
	fieldParent = "parent_id"
	fieldConfig = "config"
)

var (
	pathPattern        = regexp.MustCompile(`^[0-9a-f]{32}(\.[0-9a-f]{32})*$`)
	countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)
	partyIdPattern     = regexp.MustCompile(`^[A-Z0-9]{3}$`)
)

// PutTenant creates the tenant or updates it. Nothing reaches the tenant store before
// the whole document was checked.
func (s *Server) PutTenant(w http.ResponseWriter, r *http.Request, orgId OrgId) {
	id, ok := canonicalOrgId(orgId)
	if !ok {
		writeInvalid(w, invalid(fieldOrgId, "must be a UUID"))
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTenantDocumentBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge)
			return
		}
		writeInvalid(w, invalid(fieldBody, "cannot be read"))
		return
	}
	var doc TenantDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		writeInvalid(w, jsonProblem("", err))
		return
	}
	tenant, problem := tenantFromDocument(id, &doc)
	if problem != nil {
		writeInvalid(w, problem)
		return
	}

	err = s.Tenants.PutTenant(r.Context(), tenant)
	// also after an error: the store may have written the tenant and failed to say so
	s.invalidate(id)
	if err != nil {
		s.storeError(w, "put tenant", id, err)
		return
	}
	s.writeTenantState(w, r, id)
}

// SuspendTenant stops the tenant from changing its data through the API. It does not
// touch its charge stations.
func (s *Server) SuspendTenant(w http.ResponseWriter, r *http.Request, orgId OrgId) {
	s.setTenantStatus(w, r, orgId, store.TenantStatusSuspended)
}

func (s *Server) ResumeTenant(w http.ResponseWriter, r *http.Request, orgId OrgId) {
	s.setTenantStatus(w, r, orgId, store.TenantStatusActive)
}

func (s *Server) setTenantStatus(w http.ResponseWriter, r *http.Request, orgId string, status store.TenantStatus) {
	id, ok := canonicalOrgId(orgId)
	if !ok {
		writeInvalid(w, invalid(fieldOrgId, "must be a UUID"))
		return
	}

	found, err := s.Tenants.SetTenantStatus(r.Context(), id, status)
	// also after an error: the store may have changed the status and failed to say so
	s.invalidate(id)
	if err != nil {
		s.storeError(w, "set tenant status", id, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, codeTenantNotFound)
		return
	}
	s.writeTenantState(w, r, id)
}

// GetTenantEntitlements returns the entitlement document that is stored for the tenant.
// It is read from the tenant store, not from a cache: the platform compares it with what
// it sent.
func (s *Server) GetTenantEntitlements(w http.ResponseWriter, r *http.Request, orgId OrgId, params GetTenantEntitlementsParams) {
	id, ok := canonicalOrgId(orgId)
	if !ok {
		writeInvalid(w, invalid(fieldOrgId, "must be a UUID"))
		return
	}

	tenant, err := s.Tenants.GetTenant(r.Context(), id)
	if err != nil {
		s.storeError(w, "get tenant", id, err)
		return
	}
	if tenant == nil {
		writeError(w, http.StatusNotFound, codeTenantNotFound)
		return
	}

	etag := `"` + strconv.FormatInt(tenant.EntitlementsVersion, 10) + `"`
	w.Header().Set("ETag", etag)
	if params.IfNoneMatch != nil && etagMatches(*params.IfNoneMatch, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	document := tenant.Entitlements
	if len(document) == 0 {
		document = json.RawMessage(`{}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(document)
}

// etagMatches reports whether one of the entity tags of an If-None-Match header is the
// entity tag of what is stored. Weak and strong tags are the same here.
func etagMatches(candidates []string, etag string) bool {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func (s *Server) invalidate(orgId string) {
	if s.Resolver != nil {
		s.Resolver.Invalidate(orgId)
	}
}

func (s *Server) writeTenantState(w http.ResponseWriter, r *http.Request, orgId string) {
	tenant, err := s.Tenants.GetTenant(r.Context(), orgId)
	if err != nil {
		s.storeError(w, "get tenant", orgId, err)
		return
	}
	if tenant == nil {
		writeError(w, http.StatusNotFound, codeTenantNotFound)
		return
	}
	writeJSON(w, http.StatusOK, TenantState{
		OrgId:               tenant.OrgId,
		Status:              TenantStateStatus(tenant.Status),
		EntitlementsVersion: tenant.EntitlementsVersion,
		ConfigVersion:       tenant.ConfigVersion,
		UpdatedAt:           tenant.UpdatedAt,
	})
}

// storeError answers a request that the tenant store refused or failed. What the store
// said only goes to the log.
func (s *Server) storeError(w http.ResponseWriter, action, orgId string, err error) {
	switch {
	case errors.Is(err, store.ErrTenantImmutable):
		writeError(w, http.StatusConflict, codeImmutableField)
	case errors.Is(err, store.ErrTenantConflict):
		writeError(w, http.StatusConflict, codeConflict)
	case errors.Is(err, store.ErrNotImplemented):
		writeError(w, http.StatusNotImplemented, codeNotImplemented)
	default:
		slog.Error("control plane: "+action, "org", orgId, "error", err)
		writeError(w, http.StatusInternalServerError, codeInternal)
	}
}

// canonicalOrgId returns the lowercase form of an org id. Only the form of 36
// characters with hyphens is an org id, in any case.
func canonicalOrgId(orgId string) (string, bool) {
	if len(orgId) != 36 {
		return "", false
	}
	id, err := uuid.Parse(orgId)
	if err != nil {
		return "", false
	}
	return id.String(), true
}

// tenantFromDocument checks the document that the platform sent for the org and returns
// the tenant to store, or the first field that is not valid.
func tenantFromDocument(orgId string, doc *TenantDocument) (*store.Tenant, *invalidField) {
	if !pathPattern.MatchString(doc.Path) {
		return nil, invalid(fieldPath, "must be the labels of the orgs from the root to the tenant, separated by dots")
	}
	labels := strings.Split(doc.Path, ".")
	if len(labels) > maxPathLabels {
		return nil, invalid(fieldPath, fmt.Sprintf("must not have more than %d labels", maxPathLabels))
	}
	if labels[len(labels)-1] != store.OrgPathLabel(orgId) {
		return nil, invalid(fieldPath, "the last label must be the org of the tenant")
	}
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		if seen[label] {
			return nil, invalid(fieldPath, "must not name an org twice")
		}
		seen[label] = true
	}

	var parentId *string
	if doc.ParentId != nil {
		parent, ok := canonicalOrgId(*doc.ParentId)
		if !ok {
			return nil, invalid(fieldParent, "must be a UUID")
		}
		if len(labels) < 2 || labels[len(labels)-2] != store.OrgPathLabel(parent) {
			return nil, invalid(fieldParent, "must be the org of the label before the last one of the path")
		}
		parentId = &parent
	} else if len(labels) > 1 {
		return nil, invalid(fieldParent, "is required when the path has more than one label")
	}

	if doc.Hostnames == nil {
		return nil, invalid(tenancy.IdentityFieldHostnames, "is required")
	}
	if len(doc.Hostnames) > maxHostnames {
		return nil, invalid(tenancy.IdentityFieldHostnames, fmt.Sprintf("must not have more than %d entries", maxHostnames))
	}
	if err := tenancy.ValidateTenantIdentity(doc.Slug, doc.OrgKey, doc.Hostnames); err != nil {
		var identityError *tenancy.IdentityError
		if errors.As(err, &identityError) {
			return nil, invalid(identityError.Field, identityError.Error())
		}
		return nil, invalid(fieldBody, "the slug, the org key or a hostname is not valid")
	}

	parties := []store.TenantParty{}
	if doc.Parties != nil {
		for i, party := range *doc.Parties {
			field := fmt.Sprintf("parties[%d].", i)
			switch {
			case !validPartyRecordId(party.Id):
				return nil, invalid(field+"id", fmt.Sprintf("must be 1 to %d characters, without control characters", maxPartyRecordIdLength))
			case party.Role != TenantPartyRoleCPO && party.Role != TenantPartyRoleEMSP:
				return nil, invalid(field+"role", "must be CPO or EMSP")
			case !countryCodePattern.MatchString(party.CountryCode):
				return nil, invalid(field+"country_code", "must match "+countryCodePattern.String())
			case !partyIdPattern.MatchString(party.PartyId):
				return nil, invalid(field+"party_id", "must match "+partyIdPattern.String())
			}
			parties = append(parties, store.TenantParty{
				Id:          party.Id,
				Role:        string(party.Role),
				CountryCode: party.CountryCode,
				PartyId:     party.PartyId,
			})
		}
	}

	entitlementsVersion, entitlements, problem := versionedDocument("entitlements", doc.Entitlements)
	if problem != nil {
		return nil, problem
	}
	if entitlements != nil {
		// an entitlement document that names an org was made for that org
		var owner struct {
			OrgId *string `json:"org_id"`
		}
		if err := json.Unmarshal(entitlements, &owner); err != nil {
			return nil, jsonProblem("entitlements", err)
		}
		if owner.OrgId != nil {
			if id, ok := canonicalOrgId(*owner.OrgId); !ok || id != orgId {
				return nil, invalid("entitlements.org_id", "must be the org of the tenant")
			}
		}
	}
	configVersion, config, problem := versionedDocument(fieldConfig, doc.Config)
	if problem != nil {
		return nil, problem
	}
	if config != nil {
		// also a config that is older than the stored one, which the store will ignore
		if err := tenancy.ValidateConfig(config); err != nil {
			return nil, invalidConfig(err)
		}
	}

	return &store.Tenant{
		OrgId:               orgId,
		ParentId:            parentId,
		Path:                doc.Path,
		Slug:                doc.Slug,
		OrgKey:              doc.OrgKey,
		Hostnames:           doc.Hostnames,
		Parties:             parties,
		EntitlementsVersion: entitlementsVersion,
		Entitlements:        entitlements,
		ConfigVersion:       configVersion,
		Config:              config,
	}, nil
}

// invalidConfig turns what the schema of the tenant config refused into the answer:
// every setting that is wrong, below "config".
func invalidConfig(err error) *invalidField {
	problem := &invalidField{field: fieldConfig, message: "does not fit the schema of the tenant config", code: codeInvalidConfig}
	var configError *tenancy.ConfigError
	if !errors.As(err, &configError) || len(configError.Problems) == 0 {
		return problem
	}
	for _, p := range configError.Problems {
		field := fieldConfig
		if p.Field != "" {
			field += "." + p.Field
		}
		problem.details = append(problem.details, ErrorDetail{Field: field, Message: p.Message})
	}
	problem.field, problem.message = problem.details[0].Field, problem.details[0].Message
	return problem
}

// versionedDocument checks a document that is stored as the platform sent it: it must be
// a JSON object with a version that is not negative. A document that was not sent is
// the empty document at version 0.
func versionedDocument(field string, raw *json.RawMessage) (int64, json.RawMessage, *invalidField) {
	if raw == nil {
		return 0, nil, nil
	}
	var header struct {
		Version *int64 `json:"version"`
	}
	if err := json.Unmarshal(*raw, &header); err != nil {
		return 0, nil, jsonProblem(field, err)
	}
	if header.Version == nil {
		return 0, nil, invalid(field+".version", "is required")
	}
	if *header.Version < 0 {
		return 0, nil, invalid(field+".version", "must not be negative")
	}
	if containsNUL(*raw) {
		return 0, nil, invalid(field, "must not contain the character U+0000")
	}
	return *header.Version, *raw, nil
}

// containsNUL reports whether a string or a key of a valid JSON document has the
// character U+0000, which the PostgreSQL storage cannot keep.
func containsNUL(document []byte) bool {
	if !bytes.Contains(document, []byte(`\u0000`)) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if text, ok := token.(string); ok && strings.ContainsRune(text, 0) {
			return true
		}
	}
}

func validPartyRecordId(id string) bool {
	if id == "" || utf8.RuneCountInString(id) > maxPartyRecordIdLength {
		return false
	}
	return !strings.ContainsFunc(id, unicode.IsControl)
}

// jsonProblem turns an error of the JSON decoder into the field that is not valid.
// field is where the decoded document is in the request, "" for the body itself.
func jsonProblem(field string, err error) *invalidField {
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		name := typeError.Field
		switch {
		case field != "" && name != "":
			name = field + "." + name
		case field != "":
			name = field
		case name == "":
			name = fieldBody
		}
		return invalid(name, "has the wrong JSON type")
	}
	if field == "" {
		field = fieldBody
	}
	return invalid(field, "must be a JSON document")
}
