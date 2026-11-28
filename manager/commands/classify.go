// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"slices"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Classify returns the status of a command whose station answered with response, a
// CallResult payload that matches the response schema of the action: accepted when the
// action has no accepted statuses or the top-level "status" of the response is one of
// them, rejected otherwise.
func Classify(spec ActionSpec, response json.RawMessage) store.CommandStatus {
	if spec.AcceptedStatuses == nil {
		return store.CommandStatusAccepted
	}
	var body struct {
		Status *string `json:"status"`
	}
	if err := json.Unmarshal(response, &body); err != nil || body.Status == nil {
		return store.CommandStatusRejected
	}
	if slices.Contains(spec.AcceptedStatuses, *body.Status) {
		return store.CommandStatusAccepted
	}
	return store.CommandStatusRejected
}
