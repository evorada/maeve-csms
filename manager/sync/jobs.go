// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/jobs"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/utils/clock"
)

const (
	// SettingsJobName is the job that sends the pending settings to the charge stations.
	SettingsJobName = "sync-settings"
	// CertificatesJobName is the job that sends the pending certificates.
	CertificatesJobName = "sync-certificates"
	// TriggersJobName is the job that sends the pending trigger messages.
	TriggersJobName = "sync-triggers"
)

// Jobs returns the sync loops as jobs named sync-settings, sync-certificates and
// sync-triggers. Each job keeps its cursor in its closure (it restarts from the
// beginning when the lease moves to another replica).
func Jobs(engine store.Engine, clk clock.PassiveClock, tracer trace.Tracer, emitter transport.Emitter, interval, retryAfter time.Duration) []jobs.Job {
	v16CallMaker := ocpp16.NewCallMaker(emitter)
	dataTransferCallMaker := ocpp16.NewDataTransferCallMaker(emitter)
	v201CallMaker := ocpp201.NewCallMaker(emitter)

	// a runner runs a job once at a time, so each cursor is only used by one run at a time
	var settingsCursor, certificatesCursor, triggersCursor string
	return []jobs.Job{
		{
			Name:     SettingsJobName,
			Interval: interval,
			Run: func(ctx context.Context) error {
				settingsCursor = SyncSettingsOnce(ctx, engine, clk, v16CallMaker, v201CallMaker, settingsCursor, retryAfter)
				return nil
			},
		},
		{
			Name:     CertificatesJobName,
			Interval: interval,
			Run: func(ctx context.Context) error {
				// OCPP 1.6 stations get the certificates of ISO 15118 in a DataTransfer
				certificatesCursor = SyncCertificatesOnce(ctx, engine, clk, dataTransferCallMaker, v201CallMaker, certificatesCursor, retryAfter)
				return nil
			},
		},
		{
			Name:     TriggersJobName,
			Interval: interval,
			Run: func(ctx context.Context) error {
				triggersCursor = SyncTriggersOnce(ctx, tracer, engine, clk, v16CallMaker, dataTransferCallMaker, v201CallMaker, triggersCursor, retryAfter)
				return nil
			},
		},
	}
}
