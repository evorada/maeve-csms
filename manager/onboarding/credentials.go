// SPDX-License-Identifier: Apache-2.0

package onboarding

import (
	"encoding/json"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// The command that sets the password of a charge station, by OCPP version.
const (
	ocpp16CredentialAction  = "ChangeConfiguration"
	ocpp16CredentialKey     = "AuthorizationKey"
	ocpp201CredentialAction = "SetVariables"
	ocpp201SecurityCtrlr    = "SecurityCtrlr"
	ocpp201BasicAuth        = "BasicAuthPassword"
)

// The results of a credential command that the station did not answer with a status.
const (
	ResultTimeout      = "timeout"
	ResultCancelled    = "cancelled"
	ResultNotCompleted = "not_completed"
)

// CredentialCommand returns the command that sets the password of a charge station of
// the OCPP version: its action, its payload, and the path of the password in the
// payload (store.Command.RedactPaths). OCPP 1.6: ChangeConfiguration of AuthorizationKey;
// OCPP 2.0.1: SetVariables of SecurityCtrlr.BasicAuthPassword.
func CredentialCommand(version transport.OcppVersion, password string) (action string, payload json.RawMessage, redactPaths []string, err error) {
	var request any
	switch version {
	case transport.OcppVersion16:
		action = ocpp16CredentialAction
		request = &ocpp16.ChangeConfigurationJson{Key: ocpp16CredentialKey, Value: password}
		redactPaths = []string{"value"}
	case transport.OcppVersion201:
		action = ocpp201CredentialAction
		request = &ocpp201.SetVariablesRequestJson{SetVariableData: []ocpp201.SetVariableDataType{{
			Component:      ocpp201.ComponentType{Name: ocpp201SecurityCtrlr},
			Variable:       ocpp201.VariableType{Name: ocpp201BasicAuth},
			AttributeValue: password,
		}}}
		redactPaths = []string{"setVariableData.0.attributeValue"}
	default:
		return "", nil, nil, fmt.Errorf("no credential command for OCPP version %q", version)
	}
	payload, err = json.Marshal(request)
	if err != nil {
		return "", nil, nil, fmt.Errorf("credential command: %w", err)
	}
	return action, payload, redactPaths, nil
}

// isCredentialCommand reports whether the command can be one that CredentialCommand
// builds: its action, with a secret in its payload.
func isCredentialCommand(cmd *store.Command) bool {
	return len(cmd.RedactPaths) > 0 &&
		(cmd.Action == ocpp16CredentialAction || cmd.Action == ocpp201CredentialAction)
}

// credentialOutcome says what came of a credential command that completed: accepted is
// true when the station took its password (OCPP 1.6: Accepted or RebootRequired, which
// the catalogue counts as accepted; OCPP 2.0.1: the attribute status Accepted or
// RebootRequired). Otherwise result is what the station answered (its status, or the
// error code of its CallError) or what became of the command (ResultTimeout,
// ResultCancelled), and detail describes it when there is more to say.
func credentialOutcome(cmd *store.Command) (accepted bool, result string, detail *string) {
	switch cmd.Status {
	case store.CommandStatusAccepted:
		if cmd.Action != ocpp201CredentialAction {
			return true, "", nil
		}
		var response ocpp201.SetVariablesResponseJson
		if err := json.Unmarshal(cmd.Result, &response); err != nil || len(response.SetVariableResult) == 0 {
			return false, "invalid_result", ptr("the station answered without the result of BasicAuthPassword")
		}
		status := response.SetVariableResult[0].AttributeStatus
		for _, r := range response.SetVariableResult {
			if r.Component.Name == ocpp201SecurityCtrlr && r.Variable.Name == ocpp201BasicAuth {
				status = r.AttributeStatus
			}
		}
		if status == ocpp201.SetVariableStatusEnumTypeAccepted || status == ocpp201.SetVariableStatusEnumTypeRebootRequired {
			return true, "", nil
		}
		return false, string(status), nil
	case store.CommandStatusRejected:
		var response struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(cmd.Result, &response); err != nil || response.Status == "" {
			return false, string(store.CommandStatusRejected), nil
		}
		return false, response.Status, nil
	case store.CommandStatusFailed:
		result = string(store.CommandStatusFailed)
		if cmd.ErrorCode != nil && *cmd.ErrorCode != "" {
			result = *cmd.ErrorCode
		}
		return false, result, cmd.ErrorDescription
	case store.CommandStatusTimeout:
		return false, ResultTimeout, cmd.ErrorDescription
	case store.CommandStatusCancelled:
		return false, ResultCancelled, nil
	}
	return false, string(cmd.Status), nil
}

func ptr[T any](v T) *T {
	return &v
}
