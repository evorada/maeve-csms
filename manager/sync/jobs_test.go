// SPDX-License-Identifier: Apache-2.0

package sync_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	handlers16 "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	handlers201 "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/sync"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/trace/noop"
	clocktesting "k8s.io/utils/clock/testing"
)

// recordingEmitter keeps what is sent to the charge stations. The tests call it from one
// goroutine at a time.
type recordingEmitter struct {
	sent []sentCall
}

// sentCall is a call to a charge station without its message id.
type sentCall struct {
	OcppVersion     transport.OcppVersion
	ChargeStationId string
	Action          string
	Request         string
}

func (e *recordingEmitter) Emit(_ context.Context, ocppVersion transport.OcppVersion, chargeStationId string, message *transport.Message) error {
	e.sent = append(e.sent, sentCall{
		OcppVersion:     ocppVersion,
		ChargeStationId: chargeStationId,
		Action:          message.Action,
		Request:         string(message.RequestPayload),
	})
	return nil
}

// take returns what was sent since the last call, sorted.
func (e *recordingEmitter) take() []sentCall {
	sent := e.sent
	e.sent = nil
	sort.Slice(sent, func(i, j int) bool {
		a, b := sent[i], sent[j]
		if a.ChargeStationId != b.ChargeStationId {
			return a.ChargeStationId < b.ChargeStationId
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		return a.Request < b.Request
	})
	return sent
}

func syncJob(t *testing.T, jobList []jobs.Job, name string) jobs.Job {
	t.Helper()
	for _, job := range jobList {
		if job.Name == name {
			return job
		}
	}
	t.Fatalf("no job %s", name)
	return jobs.Job{}
}

func TestJobsAreTheThreeSyncLoops(t *testing.T) {
	engine := inmemory.NewStore(clocktesting.NewFakeClock(time.Now()))
	jobList := sync.Jobs(engine, clocktesting.NewFakePassiveClock(time.Now()), noop.NewTracerProvider().Tracer(""),
		&recordingEmitter{}, 90*time.Second, 5*time.Minute)

	require.NoError(t, jobs.Validate(jobList))
	var names []string
	for _, job := range jobList {
		names = append(names, job.Name)
		assert.Equal(t, 90*time.Second, job.Interval, job.Name)
	}
	assert.Equal(t, []string{"sync-settings", "sync-certificates", "sync-triggers"}, names)
	assert.Equal(t, []string{sync.SettingsJobName, sync.CertificatesJobName, sync.TriggersJobName}, names)
}

// pendingWork puts a pending setting, certificate and trigger message of each kind into
// the store: for an OCPP 1.6 station, an OCPP 2.0.1 station and a station that has not
// booted yet (no runtime details).
func pendingWork(t *testing.T, engine store.Engine) {
	t.Helper()
	ctx := context.Background()
	for id, version := range map[string]string{"cs16": "1.6", "cs201": "2.0.1"} {
		require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, id, &store.ChargeStationRuntimeDetails{OcppVersion: version}))
	}
	for id, name := range map[string]string{"cs16": "HeartbeatInterval", "cs201": "OCPPCommCtrlr/HeartbeatInterval", "new": "HeartbeatInterval"} {
		require.NoError(t, engine.UpdateChargeStationSettings(ctx, id, &store.ChargeStationSettings{
			Settings: map[string]*store.ChargeStationSetting{name: {Value: "60", Status: store.ChargeStationSettingStatusPending}},
		}))
	}
	for _, id := range []string{"cs16", "cs201", "new"} {
		require.NoError(t, engine.UpdateChargeStationInstallCertificates(ctx, id, &store.ChargeStationInstallCertificates{
			Certificates: []*store.ChargeStationInstallCertificate{
				{CertificateType: store.CertificateTypeV2G, CertificateId: "v2g-" + id, CertificateData: "v2g root of " + id,
					CertificateInstallationStatus: store.CertificateInstallationPending},
				{CertificateType: store.CertificateTypeChargeStation, CertificateId: "cs-" + id, CertificateData: "chain of " + id,
					CertificateInstallationStatus: store.CertificateInstallationPending},
			},
		}))
	}
	connectorId := 1
	for id, trigger := range map[string]store.TriggerMessage{
		"cs16":     store.TriggerMessageStatusNotification,
		"cs16-pnc": store.TriggerMessageSignV2GCertificate,
		"cs201":    store.TriggerMessageStatusNotification,
		"new":      store.TriggerMessageBootNotification,
	} {
		if id == "cs16-pnc" {
			require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, id, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
		}
		require.NoError(t, engine.SetChargeStationTriggerMessage(ctx, id, &store.ChargeStationTriggerMessage{
			ChargeStationId: id,
			TriggerMessage:  trigger,
			ConnectorId:     &connectorId,
			TriggerStatus:   store.TriggerStatusPending,
		}))
	}
}

// The jobs send the same calls as the sync loops did when they ran as goroutines, with the
// same call makers: OCPP 1.6 certificates and the ISO 15118 trigger messages of OCPP 1.6
// stations in a DataTransfer.
func TestSyncJobsSendWhatTheLoopsSent(t *testing.T) {
	now := time.Now()

	// the loops, with the call makers that started them
	loopEngine := inmemory.NewStore(clocktesting.NewFakeClock(now))
	pendingWork(t, loopEngine)
	loopEmitter := &recordingEmitter{}
	v16, dataTransfer, v201 := handlers16.NewCallMaker(loopEmitter), handlers16.NewDataTransferCallMaker(loopEmitter), handlers201.NewCallMaker(loopEmitter)
	clk := clocktesting.NewFakePassiveClock(now)
	tracer := noop.NewTracerProvider().Tracer("")
	loops := map[string]func(ctx context.Context){
		sync.SettingsJobName: func(ctx context.Context) {
			sync.SyncSettings(ctx, loopEngine, clk, v16, v201, 10*time.Millisecond, time.Hour)
		},
		sync.CertificatesJobName: func(ctx context.Context) {
			sync.SyncCertificates(ctx, loopEngine, clk, dataTransfer, v201, 10*time.Millisecond, time.Hour)
		},
		sync.TriggersJobName: func(ctx context.Context) {
			sync.SyncTriggers(ctx, tracer, loopEngine, clk, v16, dataTransfer, v201, 10*time.Millisecond, time.Hour)
		},
	}

	// the jobs, on a store with the same content
	jobEngine := inmemory.NewStore(clocktesting.NewFakeClock(now))
	pendingWork(t, jobEngine)
	jobEmitter := &recordingEmitter{}
	jobList := sync.Jobs(jobEngine, clk, tracer, jobEmitter, time.Minute, time.Hour)

	for _, name := range []string{sync.SettingsJobName, sync.CertificatesJobName, sync.TriggersJobName} {
		t.Run(name, func(t *testing.T) {
			// several cycles of the loop: what was sent is not due again within the hour
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			loops[name](ctx)
			cancel()
			want := loopEmitter.take()
			require.NotEmpty(t, want)

			require.NoError(t, syncJob(t, jobList, name).Run(context.Background()))
			assert.Equal(t, want, jobEmitter.take())
		})
	}

	// what the jobs sent, by station
	t.Run("calls", func(t *testing.T) {
		engine := inmemory.NewStore(clocktesting.NewFakeClock(now))
		pendingWork(t, engine)
		emitter := &recordingEmitter{}
		jobList := sync.Jobs(engine, clk, tracer, emitter, time.Minute, time.Hour)
		var got []string
		for _, job := range jobList {
			require.NoError(t, job.Run(context.Background()))
			for _, call := range emitter.take() {
				got = append(got, fmt.Sprintf("%s %s %s", job.Name, call.ChargeStationId, describeCall(t, call)))
			}
		}
		assert.Equal(t, []string{
			"sync-settings cs16 ocpp1.6 ChangeConfiguration",
			"sync-settings cs201 ocpp2.0.1 SetVariables",
			"sync-certificates cs16 ocpp1.6 DataTransfer CertificateSigned",
			"sync-certificates cs16 ocpp1.6 DataTransfer InstallCertificate",
			"sync-certificates cs201 ocpp2.0.1 CertificateSigned",
			"sync-certificates cs201 ocpp2.0.1 InstallCertificate",
			"sync-triggers cs16 ocpp1.6 TriggerMessage",
			"sync-triggers cs16-pnc ocpp1.6 DataTransfer TriggerMessage",
			"sync-triggers cs201 ocpp2.0.1 TriggerMessage",
		}, got, "a station without runtime details gets nothing")
	})
}

// describeCall gives the version and action of a call, and the message id of a DataTransfer.
func describeCall(t *testing.T, call sentCall) string {
	t.Helper()
	s := string(call.OcppVersion) + " " + call.Action
	if call.Action == "DataTransfer" {
		var req struct {
			MessageId string `json:"messageId"`
		}
		require.NoError(t, json.Unmarshal([]byte(call.Request), &req))
		s += " " + req.MessageId
	}
	return s
}

func TestSyncJobsKeepTheirCursorBetweenRuns(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	engine := inmemory.NewStore(clocktesting.NewFakeClock(now))
	// one page is 50 stations
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("cs%03d", i)
		require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, id, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
		require.NoError(t, engine.UpdateChargeStationSettings(ctx, id, &store.ChargeStationSettings{
			Settings: map[string]*store.ChargeStationSetting{"HeartbeatInterval": {Value: "60", Status: store.ChargeStationSettingStatusPending}},
		}))
		require.NoError(t, engine.UpdateChargeStationInstallCertificates(ctx, id, &store.ChargeStationInstallCertificates{
			Certificates: []*store.ChargeStationInstallCertificate{{CertificateType: store.CertificateTypeV2G, CertificateId: "v2g",
				CertificateData: "root", CertificateInstallationStatus: store.CertificateInstallationPending}},
		}))
		require.NoError(t, engine.SetChargeStationTriggerMessage(ctx, id, &store.ChargeStationTriggerMessage{
			ChargeStationId: id, TriggerMessage: store.TriggerMessageHeartbeat, TriggerStatus: store.TriggerStatusPending,
		}))
	}
	clk := clocktesting.NewFakePassiveClock(now)
	emitter := &recordingEmitter{}
	jobList := sync.Jobs(engine, clk, noop.NewTracerProvider().Tracer(""), emitter, time.Minute, time.Hour)

	stations := func(calls []sentCall) []string {
		var ids []string
		for _, call := range calls {
			ids = append(ids, call.ChargeStationId)
		}
		return ids
	}
	// pendingAgain gives the first station new pending work of the job
	pendingAgain := map[string]func(){
		sync.SettingsJobName: func() {
			require.NoError(t, engine.UpdateChargeStationSettings(ctx, "cs000", &store.ChargeStationSettings{
				Settings: map[string]*store.ChargeStationSetting{"MeterValueSampleInterval": {Value: "30", Status: store.ChargeStationSettingStatusPending}},
			}))
		},
		sync.CertificatesJobName: func() {
			require.NoError(t, engine.UpdateChargeStationInstallCertificates(ctx, "cs000", &store.ChargeStationInstallCertificates{
				Certificates: []*store.ChargeStationInstallCertificate{{CertificateType: store.CertificateTypeMO, CertificateId: "mo",
					CertificateData: "root", CertificateInstallationStatus: store.CertificateInstallationPending}},
			}))
		},
		sync.TriggersJobName: func() {
			require.NoError(t, engine.SetChargeStationTriggerMessage(ctx, "cs000", &store.ChargeStationTriggerMessage{
				ChargeStationId: "cs000", TriggerMessage: store.TriggerMessageStatusNotification, TriggerStatus: store.TriggerStatusPending,
			}))
		},
	}
	for _, job := range jobList {
		t.Run(job.Name, func(t *testing.T) {
			// the first page, then the rest
			require.NoError(t, job.Run(ctx))
			first := stations(emitter.take())
			require.Len(t, first, 50)
			assert.Equal(t, "cs000", first[0])
			assert.Equal(t, "cs049", first[49])

			require.NoError(t, job.Run(ctx))
			assert.Equal(t, []string{"cs050", "cs051", "cs052", "cs053", "cs054", "cs055", "cs056", "cs057", "cs058", "cs059"},
				stations(emitter.take()))

			// after the last page the next run starts from the beginning again (what was
			// sent is not due again before the retry time)
			require.NoError(t, job.Run(ctx))
			assert.Empty(t, emitter.take())
			pendingAgain[job.Name]()
			require.NoError(t, job.Run(ctx))
			assert.Equal(t, []string{"cs000"}, stations(emitter.take()))
		})
	}
}
