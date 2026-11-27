// SPDX-License-Identifier: Apache-2.0

package commands_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

var ocpp16Actions = []string{
	"CancelReservation", "CertificateSigned", "ChangeAvailability", "ChangeConfiguration", "ClearCache",
	"ClearChargingProfile", "DataTransfer", "DeleteCertificate", "ExtendedTriggerMessage", "GetCompositeSchedule",
	"GetConfiguration", "GetDiagnostics", "GetInstalledCertificateIds", "GetLocalListVersion", "GetLog",
	"InstallCertificate", "RemoteStartTransaction", "RemoteStopTransaction", "ReserveNow", "Reset",
	"SendLocalList", "SetChargingProfile", "SignedUpdateFirmware", "TriggerMessage", "UnlockConnector",
	"UpdateFirmware",
}

func TestCatalogueHasTheOcpp16Actions(t *testing.T) {
	assert.Equal(t, ocpp16Actions, commands.Actions(transport.OcppVersion16))
	// the 2.0.1 actions are not in the catalogue yet
	assert.Empty(t, commands.Actions(transport.OcppVersion201))
}

func TestCatalogueSchemasExist(t *testing.T) {
	for _, action := range commands.Actions(transport.OcppVersion16) {
		spec, ok := commands.Lookup(transport.OcppVersion16, action)
		require.True(t, ok, action)
		assert.Equal(t, transport.OcppVersion16, spec.Version, action)
		assert.Equal(t, action, spec.Action)
		assert.Equal(t, "ocpp16/"+action+".json", spec.RequestSchema)
		assert.Equal(t, "ocpp16/"+action+"Response.json", spec.ResponseSchema)
		for _, file := range []string{spec.RequestSchema, spec.ResponseSchema} {
			_, err := schemas.Compile(schemas.OcppSchemas, file)
			assert.NoError(t, err, file)
		}
	}
}

func TestCatalogueStatusesAndDefaultTTLs(t *testing.T) {
	for action, want := range map[string]struct {
		accepted []string
		ttl      time.Duration
	}{
		"ChangeAvailability":         {accepted: []string{"Accepted", "Scheduled"}},
		"ChangeConfiguration":        {accepted: []string{"Accepted", "RebootRequired"}},
		"GetConfiguration":           {},
		"GetDiagnostics":             {ttl: 24 * time.Hour},
		"GetInstalledCertificateIds": {accepted: []string{"Accepted", "NotFound"}},
		"GetLocalListVersion":        {},
		"GetLog":                     {accepted: []string{"Accepted", "AcceptedCanceled"}, ttl: 24 * time.Hour},
		"RemoteStartTransaction":     {accepted: []string{"Accepted"}, ttl: 5 * time.Minute},
		"Reset":                      {accepted: []string{"Accepted"}, ttl: 5 * time.Minute},
		"SignedUpdateFirmware":       {accepted: []string{"Accepted", "AcceptedCanceled"}, ttl: 24 * time.Hour},
		"UnlockConnector":            {accepted: []string{"Unlocked"}, ttl: 5 * time.Minute},
		"UpdateFirmware":             {ttl: 24 * time.Hour},
		"TriggerMessage":             {accepted: []string{"Accepted"}},
	} {
		spec, ok := commands.Lookup(transport.OcppVersion16, action)
		require.True(t, ok, action)
		assert.Equal(t, want.accepted, spec.AcceptedStatuses, action)
		assert.Equal(t, want.ttl, spec.DefaultTTL, action)
	}
}

func TestLookupOfAnUnknownAction(t *testing.T) {
	for _, tc := range []struct {
		version transport.OcppVersion
		action  string
	}{
		{transport.OcppVersion16, "BootNotification"}, // sent by stations, not by the CSMS
		{transport.OcppVersion16, "Unknown"},
		{transport.OcppVersion201, "Reset"},
		{"ocpp9", "Reset"},
	} {
		_, ok := commands.Lookup(tc.version, tc.action)
		assert.False(t, ok, "%s %s", tc.version, tc.action)
	}
}

func TestLookupReturnsACopy(t *testing.T) {
	spec, _ := commands.Lookup(transport.OcppVersion16, "ChangeAvailability")
	spec.AcceptedStatuses[0] = "Rejected"

	again, _ := commands.Lookup(transport.OcppVersion16, "ChangeAvailability")
	assert.Equal(t, []string{"Accepted", "Scheduled"}, again.AcceptedStatuses)
}

func TestVersionFromRuntime(t *testing.T) {
	v, ok := commands.VersionFromRuntime("1.6")
	assert.True(t, ok)
	assert.Equal(t, transport.OcppVersion16, v)

	v, ok = commands.VersionFromRuntime("2.0.1")
	assert.True(t, ok)
	assert.Equal(t, transport.OcppVersion201, v)

	for _, s := range []string{"", "ocpp1.6", "1.5", "2.0"} {
		_, ok := commands.VersionFromRuntime(s)
		assert.False(t, ok, s)
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		action   string
		response string
		want     store.CommandStatus
	}{
		{"UnlockConnector", `{"status":"Unlocked"}`, store.CommandStatusAccepted},
		{"UnlockConnector", `{"status":"UnlockFailed"}`, store.CommandStatusRejected},
		{"UnlockConnector", `{"status":"NotSupported"}`, store.CommandStatusRejected},
		{"ChangeAvailability", `{"status":"Scheduled"}`, store.CommandStatusAccepted},
		{"ChangeAvailability", `{"status":"Rejected"}`, store.CommandStatusRejected},
		{"Reset", `{"status":"Accepted"}`, store.CommandStatusAccepted},
		{"Reset", `{"status":"Rejected"}`, store.CommandStatusRejected},
		{"GetConfiguration", `{"configurationKey":[]}`, store.CommandStatusAccepted},
		{"GetLocalListVersion", `{"listVersion":3}`, store.CommandStatusAccepted},
		{"UpdateFirmware", `{}`, store.CommandStatusAccepted},
		{"GetInstalledCertificateIds", `{"status":"NotFound"}`, store.CommandStatusAccepted},
		{"DeleteCertificate", `{"status":"NotFound"}`, store.CommandStatusRejected},
		{"ChangeConfiguration", `{"status":"RebootRequired"}`, store.CommandStatusAccepted},
		{"ChangeConfiguration", `{"status":"NotSupported"}`, store.CommandStatusRejected},
		// an action with statuses whose response has none is not accepted
		{"Reset", `{}`, store.CommandStatusRejected},
		{"Reset", `not json`, store.CommandStatusRejected},
	} {
		spec, ok := commands.Lookup(transport.OcppVersion16, tc.action)
		require.True(t, ok, tc.action)
		assert.Equal(t, tc.want, commands.Classify(spec, json.RawMessage(tc.response)), "%s %s", tc.action, tc.response)
	}
}
