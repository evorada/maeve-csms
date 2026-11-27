// SPDX-License-Identifier: Apache-2.0

package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/render"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// GetLocalListVersion answers the version of the local authorization list of the charge
// station that the manager knows. With refresh, an OCPP 1.6 charge station is asked
// for the version instead: a GetLocalListVersion command is queued and answered (202),
// and the version is the result of the command. Refreshing sends a command, so it needs
// the permission of the commands as well.
func (s *Server) GetLocalListVersion(w http.ResponseWriter, r *http.Request, csId string, params GetLocalListVersionParams) {
	if params.Refresh != nil && *params.Refresh &&
		s.answerThroughQueue(w, r, csId, http.StatusAccepted, "GetLocalListVersion", &ocpp16.GetLocalListVersionJson{}) {
		return
	}

	version, err := s.store.GetLocalListVersion(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	resp := &LocalListVersionResponse{
		ListVersion: int32(version),
	}

	_ = render.Render(w, r, resp)
}

func (s *Server) GetLocalAuthorizationList(w http.ResponseWriter, r *http.Request, csId string) {
	entries, err := s.store.GetLocalAuthList(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	resp := &LocalAuthorizationListResponse{
		ListVersion:            0, // Will be set below
		LocalAuthorizationList: make([]LocalAuthorizationEntry, len(entries)),
	}

	// Get the current version
	version, err := s.store.GetLocalListVersion(r.Context(), csId)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}
	resp.ListVersion = int32(version)

	// Convert store entries to API entries
	for i, entry := range entries {
		apiEntry := LocalAuthorizationEntry{
			IdTag: entry.IdTag,
			IdTagInfo: IdTagInfo{
				Status: IdTagInfoStatus(entry.IdTagInfo.Status),
			},
		}

		if entry.IdTagInfo.ExpiryDate != nil {
			expiryDate, err := time.Parse(time.RFC3339, *entry.IdTagInfo.ExpiryDate)
			if err == nil {
				apiEntry.IdTagInfo.ExpiryDate = &expiryDate
			}
		}

		if entry.IdTagInfo.ParentIdTag != nil {
			apiEntry.IdTagInfo.ParentIdTag = entry.IdTagInfo.ParentIdTag
		}

		resp.LocalAuthorizationList[i] = apiEntry
	}

	_ = render.Render(w, r, resp)
}

// UpdateLocalAuthorizationList changes the local authorization list of the charge
// station. For an OCPP 1.6 charge station it queues a SendLocalList command and answers
// it: the result handler of SendLocalList keeps the list and its version when the charge
// station accepts it. The caller needs the permission of the operation
// (cpms.token.write), as for other charge stations, which get the list in the store at
// once.
func (s *Server) UpdateLocalAuthorizationList(w http.ResponseWriter, r *http.Request, csId string) {
	req := new(UpdateLocalListRequest)
	if err := render.Bind(r, req); err != nil {
		_ = render.Render(w, r, ErrInvalidRequest(err))
		return
	}

	// Validate update type
	if req.UpdateType != Full &&
		req.UpdateType != Differential {
		_ = render.Render(w, r, ErrInvalidRequest(fmt.Errorf("invalid update type: %s", req.UpdateType)))
		return
	}

	if s.answerThroughQueueWithPermission(w, r, csId, tenancy.PermTokenWrite, http.StatusAccepted, "SendLocalList", sendLocalListOf(req)) {
		return
	}

	// Convert API entries to store entries
	var entries []*store.LocalAuthListEntry
	if req.LocalAuthorizationList != nil {
		entries = make([]*store.LocalAuthListEntry, len(*req.LocalAuthorizationList))
		for i, apiEntry := range *req.LocalAuthorizationList {
			storeEntry := &store.LocalAuthListEntry{
				IdTag: apiEntry.IdTag,
				IdTagInfo: &store.IdTagInfo{
					Status: string(apiEntry.IdTagInfo.Status),
				},
			}

			if apiEntry.IdTagInfo.ExpiryDate != nil {
				expiryDate := apiEntry.IdTagInfo.ExpiryDate.Format(time.RFC3339)
				storeEntry.IdTagInfo.ExpiryDate = &expiryDate
			}

			if apiEntry.IdTagInfo.ParentIdTag != nil {
				storeEntry.IdTagInfo.ParentIdTag = apiEntry.IdTagInfo.ParentIdTag
			}

			entries[i] = storeEntry
		}
	}

	// Update the local authorization list
	err := s.store.UpdateLocalAuthList(r.Context(), csId, int(req.ListVersion), string(req.UpdateType), entries)
	if err != nil {
		_ = render.Render(w, r, ErrInternalError(err))
		return
	}

	// Return 202 Accepted - in a real implementation, this would trigger an OCPP SendLocalList call
	w.WriteHeader(http.StatusAccepted)
}

// sendLocalListOf is the SendLocalList request of OCPP 1.6 that makes the change of the
// local authorization list.
func sendLocalListOf(req *UpdateLocalListRequest) *ocpp16.SendLocalListJson {
	payload := &ocpp16.SendLocalListJson{
		ListVersion: int(req.ListVersion),
		UpdateType:  ocpp16.SendLocalListJsonUpdateType(req.UpdateType),
	}
	if req.LocalAuthorizationList != nil {
		for _, entry := range *req.LocalAuthorizationList {
			payload.LocalAuthorizationList = append(payload.LocalAuthorizationList, ocpp16.SendLocalListJsonLocalAuthorizationListEntry{
				IdTag: entry.IdTag,
				IdTagInfo: &ocpp16.SendLocalListJsonIdTagInfo{
					Status:      ocpp16.SendLocalListJsonIdTagInfoStatus(entry.IdTagInfo.Status),
					ExpiryDate:  optionalOcppDateTime(entry.IdTagInfo.ExpiryDate),
					ParentIdTag: entry.IdTagInfo.ParentIdTag,
				},
			})
		}
	}
	return payload
}

// Render implementations

func (u UpdateLocalListRequest) Bind(r *http.Request) error {
	return nil
}

// Render implementations

func (l LocalListVersionResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

// Render implementations

func (l LocalAuthorizationListResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}
