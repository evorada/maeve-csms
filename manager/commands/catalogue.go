// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"slices"
	"sort"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/transport"
)

// ActionSpec describes one action that the CSMS can send to a charge station of one OCPP
// version.
type ActionSpec struct {
	Version        transport.OcppVersion
	Action         string
	RequestSchema  string // e.g. "ocpp16/Reset.json"
	ResponseSchema string // e.g. "ocpp16/ResetResponse.json"
	// AcceptedStatuses are the values of the top-level "status" of the response that
	// count as accepted; any other value is rejected. nil: the response has no status
	// and any valid CallResult counts as accepted.
	AcceptedStatuses []string
	// DefaultTTL is how long a command of the action may wait in the queue unless the
	// configuration or the caller says otherwise; 0 uses the configured default.
	DefaultTTL time.Duration
}

// catalogue holds the actions that can be sent, by OCPP version and action.
var catalogue = map[transport.OcppVersion]map[string]ActionSpec{
	transport.OcppVersion16: actionsOf(
		ocpp16Action("CancelReservation", 0, "Accepted"),
		ocpp16Action("CertificateSigned", 0, "Accepted"),
		ocpp16Action("ChangeAvailability", 0, "Accepted", "Scheduled"),
		ocpp16Action("ChangeConfiguration", 0, "Accepted", "RebootRequired"),
		ocpp16Action("ClearCache", 0, "Accepted"),
		ocpp16Action("ClearChargingProfile", 0, "Accepted"),
		ocpp16Action("DataTransfer", 0, "Accepted"),
		ocpp16Action("DeleteCertificate", 0, "Accepted"),
		ocpp16Action("ExtendedTriggerMessage", 0, "Accepted"),
		ocpp16Action("GetCompositeSchedule", 0, "Accepted"),
		ocpp16Action("GetConfiguration", 0),
		ocpp16Action("GetDiagnostics", 24*time.Hour),
		ocpp16Action("GetInstalledCertificateIds", 0, "Accepted", "NotFound"),
		ocpp16Action("GetLocalListVersion", 0),
		ocpp16Action("GetLog", 24*time.Hour, "Accepted", "AcceptedCanceled"),
		ocpp16Action("InstallCertificate", 0, "Accepted"),
		ocpp16Action("RemoteStartTransaction", 5*time.Minute, "Accepted"),
		ocpp16Action("RemoteStopTransaction", 0, "Accepted"),
		ocpp16Action("ReserveNow", 0, "Accepted"),
		ocpp16Action("Reset", 5*time.Minute, "Accepted"),
		ocpp16Action("SendLocalList", 0, "Accepted"),
		ocpp16Action("SetChargingProfile", 0, "Accepted"),
		ocpp16Action("SignedUpdateFirmware", 24*time.Hour, "Accepted", "AcceptedCanceled"),
		ocpp16Action("TriggerMessage", 0, "Accepted"),
		ocpp16Action("UnlockConnector", 5*time.Minute, "Unlocked"),
		ocpp16Action("UpdateFirmware", 24*time.Hour),
	),
}

// ocpp16Action returns the spec of an OCPP 1.6 action, whose schemas are named after it.
// Without accepted statuses the response has no status field.
func ocpp16Action(action string, ttl time.Duration, accepted ...string) ActionSpec {
	return ActionSpec{
		Version:          transport.OcppVersion16,
		Action:           action,
		RequestSchema:    "ocpp16/" + action + ".json",
		ResponseSchema:   "ocpp16/" + action + "Response.json",
		AcceptedStatuses: accepted,
		DefaultTTL:       ttl,
	}
}

func actionsOf(specs ...ActionSpec) map[string]ActionSpec {
	actions := make(map[string]ActionSpec, len(specs))
	for _, spec := range specs {
		actions[spec.Action] = spec
	}
	return actions
}

// Lookup returns the spec of an action of an OCPP version, and false when that action
// cannot be sent to stations of that version.
func Lookup(v transport.OcppVersion, action string) (ActionSpec, bool) {
	spec, ok := catalogue[v][action]
	if !ok {
		return ActionSpec{}, false
	}
	spec.AcceptedStatuses = slices.Clone(spec.AcceptedStatuses)
	return spec, true
}

// Actions returns the actions that can be sent to stations of an OCPP version, sorted.
func Actions(v transport.OcppVersion) []string {
	actions := make([]string, 0, len(catalogue[v]))
	for action := range catalogue[v] {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}

// VersionFromRuntime returns the OCPP version of a station from the version that its
// runtime details record ("1.6", "2.0.1"), and false for any other value.
func VersionFromRuntime(s string) (transport.OcppVersion, bool) {
	switch s {
	case "1.6":
		return transport.OcppVersion16, true
	case "2.0.1":
		return transport.OcppVersion201, true
	default:
		return "", false
	}
}
