// SPDX-License-Identifier: Apache-2.0

package onboarding_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/commands"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/onboarding"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
)

var hexPassword = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestNewPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		password, hash, err := onboarding.NewPassword()
		require.NoError(t, err)
		assert.Regexp(t, hexPassword, password, "20 random bytes, hex encoded")
		assert.Equal(t, onboarding.PasswordHash(password), hash)
		assert.False(t, seen[password], "a new password each time")
		seen[password] = true
	}
	sum := sha256.Sum256([]byte("my password"))
	assert.Equal(t, base64.StdEncoding.EncodeToString(sum[:]), onboarding.PasswordHash("my password"),
		"the hash that the gateway compares")
}

func TestCredentialCommand(t *testing.T) {
	const password = "0123456789abcdef0123456789abcdef01234567"
	service := &commands.Service{SchemaFS: schemas.OcppSchemas}

	action, payload, paths, err := onboarding.CredentialCommand(transport.OcppVersion16, password)
	require.NoError(t, err)
	assert.Equal(t, "ChangeConfiguration", action)
	assert.JSONEq(t, `{"key":"AuthorizationKey","value":"`+password+`"}`, string(payload))
	assert.Equal(t, []string{"value"}, paths)
	assert.NoError(t, service.CheckPayload(transport.OcppVersion16, action, payload))
	redacted, _ := commands.RedactPayload(payload, paths)
	assert.NotContains(t, string(redacted), password)

	action, payload, paths, err = onboarding.CredentialCommand(transport.OcppVersion201, password)
	require.NoError(t, err)
	assert.Equal(t, "SetVariables", action)
	assert.JSONEq(t, `{"setVariableData":[{"component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"},`+
		`"attributeValue":"`+password+`"}]}`, string(payload))
	assert.Equal(t, []string{"setVariableData.0.attributeValue"}, paths)
	assert.NoError(t, service.CheckPayload(transport.OcppVersion201, action, payload))
	redacted, _ = commands.RedactPayload(payload, paths)
	assert.NotContains(t, string(redacted), password)

	_, _, _, err = onboarding.CredentialCommand("ocpp9", password)
	assert.Error(t, err)
}

func TestARotationStoresThePendingPasswordAndTheApproval(t *testing.T) {
	f := newFixture(t)
	cmd, call := f.rotate(testStation, transport.OcppVersion16)

	var sent struct{ Key, Value string }
	require.NoError(t, json.Unmarshal(call.RequestPayload, &sent))
	assert.Equal(t, "AuthorizationKey", sent.Key)
	assert.Regexp(t, hexPassword, sent.Value)
	assert.Equal(t, cmd.Id, call.MessageId)

	cs := f.station(testStation)
	assert.Equal(t, store.StationStatePending, cs.State)
	require.NotNil(t, cs.Onboarding)
	assert.Equal(t, store.OnboardingApproving, cs.Onboarding.Status)
	assert.Equal(t, &cmd.Id, cs.Onboarding.CommandId)
	assert.Equal(t, f.approval(), cs.Onboarding.Approval)
	assert.Equal(t, testStart, cs.Onboarding.FirstSeenAt, "the rest of the onboarding is kept")
	assert.Equal(t, onboarding.PasswordHash(sent.Value), f.auth(testStation).PendingBase64SHA256Password,
		"the station may connect with its new password")
	assert.Empty(t, f.auth(testStation).Base64SHA256Password)

	stored, err := f.engine.GetCommand(context.Background(), cmd.Id)
	require.NoError(t, err)
	assert.JSONEq(t, `{"key":"AuthorizationKey","value":"***"}`, string(stored.Payload), "the password is not kept once it is sent")
	assert.Equal(t, "user:alice", *stored.Actor)
}

func TestAStationThatTakesItsPasswordIsApproved(t *testing.T) {
	for name, tc := range map[string]struct {
		station  string
		version  transport.OcppVersion
		response string
	}{
		"OCPP 1.6 Accepted":                   {testStation, transport.OcppVersion16, `{"status":"Accepted"}`},
		"OCPP 1.6 RebootRequired":             {testStation, transport.OcppVersion16, `{"status":"RebootRequired"}`},
		"OCPP 2.0.1 Accepted":                 {testStation21, transport.OcppVersion201, setVariablesResult("Accepted")},
		"OCPP 2.0.1 RebootRequired":           {testStation21, transport.OcppVersion201, setVariablesResult("RebootRequired")},
		"OCPP 2.0.1 with another result next": {testStation21, transport.OcppVersion201, `{"setVariableResult":[{"attributeStatus":"Rejected","component":{"name":"Other"},"variable":{"name":"X"}},` + setVariablesResultItem("Accepted") + `]}`},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			cmd, call := f.rotate(tc.station, tc.version)
			pending := f.auth(tc.station).PendingBase64SHA256Password
			f.events.Take(t)

			f.answer(call, tc.response)

			cs := f.station(tc.station)
			assert.Equal(t, store.StationStateActive, cs.State)
			assert.Nil(t, cs.Onboarding)
			assert.False(t, cs.SharedCredentials)
			assert.Equal(t, &f.location.Id, cs.LocationId)
			assert.Equal(t, ptr("Plaza 1"), cs.DisplayName)
			assert.Equal(t, &store.ChargeStationAuth{
				SecurityProfile: store.UnsecuredTransportWithBasicAuth, Base64SHA256Password: pending,
				InvalidUsernameAllowed: true, OrgId: testOrg,
			}, f.auth(tc.station), "the pending password is the password of the station")
			evses, err := f.engine.ListAssetEvsesForStation(context.Background(), tc.station)
			require.NoError(t, err)
			require.Len(t, evses, 1)
			assert.Len(t, evses[0].Connectors, 2)

			evs := f.events.Take(t)
			assert.Equal(t, []string{events.TypeCommandCompleted, events.TypeLocationUpdated, events.TypeStationApproved}, eventstest.Types(evs))
			assert.Equal(t, events.LocationUpdatedData{LocationId: f.location.Id, OcpiId: "LOC1", PartyRef: "cpo-a",
				Change: events.LocationChangeCreated, EvseUid: evses[0].Id, Publish: true}, eventstest.Data[events.LocationUpdatedData](t, evs[1]))
			approved := evs[2]
			assert.Equal(t, testOrg, approved.OrgId)
			assert.Equal(t, "user:alice", approved.Actor)
			assert.Equal(t, events.StationApprovedData{StationKey: tc.station, StationId: events.StationId(tc.station),
				LocationId: f.location.Id, EvseCount: 1, ConnectorCount: 2, PasswordMode: events.PasswordModeRotate,
				Actor: "user:alice"}, eventstest.Data[events.StationApprovedData](t, approved))

			// the station is asked to boot again, so that it is accepted at once
			sent := f.waitSent(2)
			assert.Equal(t, "TriggerMessage", sent[1].Action)
			assert.JSONEq(t, `{"requestedMessage":"BootNotification"}`, string(sent[1].RequestPayload))
			trigger, err := f.engine.GetCommand(context.Background(), sent[1].MessageId)
			require.NoError(t, err)
			assert.Equal(t, string(tc.version), trigger.OcppVersion)
			assert.Equal(t, cmd.Actor, trigger.Actor)
		})
	}
}

func setVariablesResultItem(status string) string {
	return `{"attributeStatus":"` + status + `","component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"}}`
}

func setVariablesResult(status string) string {
	return `{"setVariableResult":[` + setVariablesResultItem(status) + `]}`
}

// assertApprovalFailed checks that the approval of the station stopped at
// credentials_failed with the reason, and that one station.approval_failed event says so.
func (f *fixture) assertApprovalFailed(stationKey, commandId, pending, result string, detail *string) {
	f.t.Helper()
	cs := f.station(stationKey)
	assert.Equal(f.t, store.StationStatePending, cs.State, "the station stays pending")
	require.NotNil(f.t, cs.Onboarding)
	assert.Equal(f.t, store.OnboardingCredentialsFailed, cs.Onboarding.Status)
	reason := result
	if detail != nil {
		reason += ": " + *detail
	}
	assert.Equal(f.t, &reason, cs.Onboarding.Error)
	assert.Equal(f.t, &commandId, cs.Onboarding.CommandId)
	assert.Equal(f.t, f.approval(), cs.Onboarding.Approval, "the approval is kept")
	assert.Equal(f.t, pending, f.auth(stationKey).PendingBase64SHA256Password,
		"the pending password is kept: a station that took it can still connect")
	assert.Empty(f.t, f.auth(stationKey).Base64SHA256Password)

	var failed []*store.OutboxEvent
	for _, ev := range f.events.Take(f.t) {
		if ev.Type == events.TypeStationApprovalFailed {
			failed = append(failed, ev)
		}
	}
	require.Len(f.t, failed, 1)
	assert.Equal(f.t, "user:alice", failed[0].Actor)
	assert.Equal(f.t, events.StationApprovalFailedData{StationKey: stationKey, StationId: events.StationId(stationKey),
		CommandId: commandId, Result: result, Error: detail}, eventstest.Data[events.StationApprovalFailedData](f.t, failed[0]))
}

func TestAStationThatDoesNotTakeItsPasswordStaysPending(t *testing.T) {
	ctx := context.Background()

	for name, tc := range map[string]struct {
		station  string
		version  transport.OcppVersion
		response string
		result   string
	}{
		"OCPP 1.6 NotSupported":         {testStation, transport.OcppVersion16, `{"status":"NotSupported"}`, "NotSupported"},
		"OCPP 1.6 Rejected":             {testStation, transport.OcppVersion16, `{"status":"Rejected"}`, "Rejected"},
		"OCPP 2.0.1 Rejected":           {testStation21, transport.OcppVersion201, setVariablesResult("Rejected"), "Rejected"},
		"OCPP 2.0.1 UnknownVariable":    {testStation21, transport.OcppVersion201, setVariablesResult("UnknownVariable"), "UnknownVariable"},
		"OCPP 1.6 answer out of schema": {testStation, transport.OcppVersion16, `{"status":"Maybe"}`, "FormatViolation"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			cmd, call := f.rotate(tc.station, tc.version)
			pending := f.auth(tc.station).PendingBase64SHA256Password
			f.events.Take(t)

			f.answer(call, tc.response)

			var detail *string
			if tc.result == "FormatViolation" {
				stored, err := f.engine.GetCommand(ctx, cmd.Id)
				require.NoError(t, err)
				detail = stored.ErrorDescription
			}
			f.assertApprovalFailed(tc.station, cmd.Id, pending, tc.result, detail)
			assert.Len(t, f.calls.sent(), 1, "nothing more is sent")
		})
	}

	t.Run("a CallError", func(t *testing.T) {
		f := newFixture(t)
		cmd, call := f.rotate(testStation, transport.OcppVersion16)
		pending := f.auth(testStation).PendingBase64SHA256Password
		f.events.Take(t)

		require.True(t, f.dispatcher.ObserveCallError(ctx, testStation, &transport.Message{
			MessageType: transport.MessageTypeCallError, Action: call.Action, MessageId: call.MessageId,
			ErrorCode: transport.ErrorNotSupported, ErrorDescription: "AuthorizationKey is read-only",
		}))
		f.assertApprovalFailed(testStation, cmd.Id, pending, "NotSupported", ptr("AuthorizationKey is read-only"))
	})

	t.Run("no answer in time, and the answer that comes late", func(t *testing.T) {
		f := newFixture(t)
		cmd, call := f.rotate(testStation, transport.OcppVersion16)
		pending := f.auth(testStation).PendingBase64SHA256Password
		f.events.Take(t)

		f.clock.Step(31 * time.Second)
		require.NoError(t, f.dispatcher.Sweep(ctx))
		f.assertApprovalFailed(testStation, cmd.Id, pending, onboarding.ResultTimeout, ptr("no answer within 30s"))

		// the station did take it: the approval is completed all the same
		f.answer(call, `{"status":"Accepted"}`)
		assert.Equal(t, store.StationStateActive, f.station(testStation).State)
		assert.Equal(t, pending, f.auth(testStation).Base64SHA256Password)
	})

	t.Run("cancelled before it was sent", func(t *testing.T) {
		f := newFixture(t)
		var rotation *onboarding.Rotation
		require.NoError(t, f.events.Tx.RunInTx(ctx, func(ctx context.Context) error {
			var err error
			rotation, err = f.approvals.StartRotation(ctx, f.station(testStation), f.approval(), transport.OcppVersion16, ptr("user:alice"), nil)
			return err
		}))
		cmd := *rotation.Command
		cmd.ExpiresAt = testStart.Add(time.Hour)
		f.dispatcher.Connectivity = disconnected{}
		_, err := f.service.Enqueue(ctx, &cmd)
		require.NoError(t, err)
		pending := f.auth(testStation).PendingBase64SHA256Password
		f.events.Take(t)

		_, err = f.service.Cancel(ctx, cmd.Id)
		require.NoError(t, err)
		f.assertApprovalFailed(testStation, cmd.Id, pending, onboarding.ResultCancelled, nil)
	})

	t.Run("a second failure of the command changes nothing", func(t *testing.T) {
		f := newFixture(t)
		cmd, call := f.rotate(testStation, transport.OcppVersion16)
		f.clock.Step(31 * time.Second)
		require.NoError(t, f.dispatcher.Sweep(ctx))
		f.events.Take(t)
		before := f.station(testStation)

		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, &transport.Message{
			MessageType: transport.MessageTypeCallResult, Action: call.Action, MessageId: cmd.Id,
			RequestPayload: call.RequestPayload, ResponsePayload: json.RawMessage(`{"status":"Rejected"}`),
		}))
		assert.Equal(t, before.Onboarding, f.station(testStation).Onboarding)
		assert.NotContains(t, eventstest.Types(f.events.Take(t)), events.TypeStationApprovalFailed)
	})
}

// disconnected says that no station is connected.
type disconnected struct{}

func (disconnected) IsConnected(context.Context, string) (bool, error) { return false, nil }

func TestAnswersThatNoApprovalWaitsForChangeNothing(t *testing.T) {
	ctx := context.Background()

	t.Run("the station was approved by hand meanwhile", func(t *testing.T) {
		f := newFixture(t)
		_, call := f.rotate(testStation, transport.OcppVersion16)
		manual := &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, Base64SHA256Password: "MANUAL"}
		require.NoError(t, f.events.Tx.RunInTx(ctx, func(ctx context.Context) error {
			return f.approvals.Complete(ctx, f.station(testStation), f.approval(), manual, false, events.PasswordModeManual)
		}))
		f.events.Take(t)

		f.answer(call, `{"status":"Accepted"}`)
		assert.Equal(t, "MANUAL", f.auth(testStation).Base64SHA256Password, "the password set by hand stays")
		assert.Equal(t, []string{events.TypeCommandCompleted}, eventstest.Types(f.events.Take(t)))
		assert.Len(t, f.calls.sent(), 1, "no boot is asked for")
	})

	t.Run("the station was rejected meanwhile", func(t *testing.T) {
		f := newFixture(t)
		_, call := f.rotate(testStation, transport.OcppVersion16)
		require.NoError(t, f.engine.RejectChargeStation(ctx, testStation, "not ours"))
		f.events.Take(t)

		f.answer(call, `{"status":"Accepted"}`)
		assert.Equal(t, store.StationStateDecommissioned, f.station(testStation).State)
		assert.Empty(t, f.auth(testStation).Base64SHA256Password)
		assert.Equal(t, []string{events.TypeCommandCompleted}, eventstest.Types(f.events.Take(t)))
	})

	t.Run("the station was approved again with another command", func(t *testing.T) {
		f := newFixture(t)
		first, firstCall := f.rotate(testStation, transport.OcppVersion16)
		f.answer(firstCall, `{"status":"NotSupported"}`)
		second, _ := f.rotate(testStation, transport.OcppVersion16)
		f.events.Take(t)

		// the first command is answered again, late: it is not the approval any more
		require.True(t, f.dispatcher.ObserveCallResult(ctx, testStation, &transport.Message{
			MessageType: transport.MessageTypeCallResult, Action: firstCall.Action, MessageId: first.Id,
			ResponsePayload: json.RawMessage(`{"status":"Accepted"}`),
		}))
		cs := f.station(testStation)
		assert.Equal(t, store.StationStatePending, cs.State)
		assert.Equal(t, store.OnboardingApproving, cs.Onboarding.Status)
		assert.Equal(t, &second.Id, cs.Onboarding.CommandId)
	})

	t.Run("a command that sets no password", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.rotate(testStation, transport.OcppVersion16)
		before := f.station(testStation)
		reset, err := f.service.Enqueue(ctx, &store.Command{OrgId: testOrg, StationKey: testStation21, Action: "TriggerMessage",
			Payload: json.RawMessage(`{"requestedMessage":"BootNotification"}`)})
		require.NoError(t, err)
		f.answer(&transport.Message{Action: reset.Action, MessageId: reset.Id, RequestPayload: reset.Payload}, `{"status":"Rejected"}`)
		assert.Equal(t, before, f.station(testStation))
	})
}

func TestAnApprovalThatCannotBeCompletedFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	cmd, call := f.rotate(testStation, transport.OcppVersion16)
	pending := f.auth(testStation).PendingBase64SHA256Password
	// the EVSE id of the approval is taken meanwhile
	require.NoError(t, f.engine.SetChargeStationAuth(ctx, "aaaa.OTHER", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: testOrg}))
	require.NoError(t, f.engine.CreateAssetEvse(ctx, &store.AssetEvse{OrgId: testOrg, LocationId: f.location.Id, StationKey: "aaaa.OTHER",
		OcppEvseId: 1, EvseId: ptr("ES*CPO*ECP777*1")}))
	f.events.Take(t)

	f.answer(call, `{"status":"Accepted"}`)

	f.assertApprovalFailed(testStation, cmd.Id, pending, onboarding.ResultNotCompleted,
		ptr("an EVSE of the approval has an evse_id or an OCPP EVSE id that is taken"))
	evses, err := f.engine.ListAssetEvsesForStation(ctx, testStation)
	require.NoError(t, err)
	assert.Empty(t, evses)
}

func TestUndoRotation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	before := f.station(testStation).Onboarding
	var rotation *onboarding.Rotation
	require.NoError(t, f.events.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		rotation, err = f.approvals.StartRotation(ctx, f.station(testStation), f.approval(), transport.OcppVersion16, nil, nil)
		return err
	}))
	require.NotEmpty(t, f.auth(testStation).PendingBase64SHA256Password)

	require.NoError(t, f.approvals.UndoRotation(ctx, rotation))
	assert.Equal(t, before, f.station(testStation).Onboarding)
	assert.Empty(t, f.auth(testStation).PendingBase64SHA256Password)

	// a station whose onboarding changed since is left alone
	require.NoError(t, f.events.Tx.RunInTx(ctx, func(ctx context.Context) error {
		var err error
		_, err = f.approvals.StartRotation(ctx, f.station(testStation), f.approval(), transport.OcppVersion16, nil, nil)
		return err
	}))
	changed := f.station(testStation).Onboarding
	require.NoError(t, f.approvals.UndoRotation(ctx, rotation))
	assert.Equal(t, changed, f.station(testStation).Onboarding)
}
