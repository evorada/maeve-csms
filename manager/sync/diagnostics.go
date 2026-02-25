// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

func SyncDiagnostics(ctx context.Context,
	engine store.Engine,
	clock clock.PassiveClock,
	v16CallMaker,
	v201CallMaker handlers.CallMaker,
	runEvery,
	retryAfter time.Duration) {
	var previousDiagnosticsId string
	var previousLogId string
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down sync diagnostics")
			return
		case <-time.After(runEvery):
			previousDiagnosticsId = syncDiagnosticsRequests(ctx, engine, clock, v16CallMaker, previousDiagnosticsId, retryAfter)
			previousLogId = syncLogRequests(ctx, engine, clock, v16CallMaker, v201CallMaker, previousLogId, retryAfter)
		}
	}
}

func syncDiagnosticsRequests(ctx context.Context,
	engine store.Engine,
	clock clock.PassiveClock,
	v16CallMaker handlers.CallMaker,
	previousChargeStationId string,
	retryAfter time.Duration) string {

	requests, err := engine.ListDiagnosticsRequests(ctx, 50, previousChargeStationId)
	if err != nil {
		slog.Error("list diagnostics requests", slog.String("err", err.Error()))
		return previousChargeStationId
	}
	if len(requests) > 0 {
		previousChargeStationId = requests[len(requests)-1].ChargeStationId
	} else {
		previousChargeStationId = ""
	}

	for _, req := range requests {
		if req.Status != store.DiagnosticsRequestStatusPending {
			continue
		}
		if !clock.Now().After(req.SendAfter) {
			continue
		}

		csId := req.ChargeStationId
		details, err := engine.LookupChargeStationRuntimeDetails(ctx, csId)
		if err != nil {
			slog.Error("lookup charge station runtime details for diagnostics",
				slog.String("err", err.Error()),
				slog.String("chargeStationId", csId))
			continue
		}
		if details == nil {
			slog.Error("no runtime details for charge station",
				slog.String("chargeStationId", csId))
			continue
		}

		if details.OcppVersion != "1.6" {
			slog.Warn("diagnostics requests (GetDiagnostics) only supported for OCPP 1.6",
				slog.String("chargeStationId", csId),
				slog.String("ocppVersion", details.OcppVersion))
			if err := engine.DeleteDiagnosticsRequest(ctx, csId); err != nil {
				slog.Error("delete diagnostics request", slog.String("err", err.Error()))
			}
			continue
		}

		// Update send after for retry
		req.SendAfter = clock.Now().Add(retryAfter)
		if err := engine.SetDiagnosticsRequest(ctx, csId, req); err != nil {
			slog.Error("update diagnostics request send after", slog.String("err", err.Error()))
			continue
		}

		ocppReq := &ocpp16.GetDiagnosticsJson{
			Location:      req.Location,
			Retries:       req.Retries,
			RetryInterval: req.RetryInterval,
		}
		if req.StartTime != nil {
			startTime := req.StartTime.Format(time.RFC3339)
			ocppReq.StartTime = &startTime
		}
		if req.StopTime != nil {
			stopTime := req.StopTime.Format(time.RFC3339)
			ocppReq.StopTime = &stopTime
		}

		if err := v16CallMaker.Send(ctx, csId, ocppReq); err != nil {
			slog.Error("send get diagnostics request",
				slog.String("err", err.Error()),
				slog.String("chargeStationId", csId))
			continue
		}

		// Request sent successfully, mark as accepted and delete
		if err := engine.DeleteDiagnosticsRequest(ctx, csId); err != nil {
			slog.Error("delete diagnostics request after send",
				slog.String("err", err.Error()),
				slog.String("chargeStationId", csId))
		}

		slog.Info("sent get diagnostics request",
			slog.String("chargeStationId", csId),
			slog.String("location", req.Location))
	}

	return previousChargeStationId
}

func syncLogRequests(ctx context.Context,
	engine store.Engine,
	clock clock.PassiveClock,
	v16CallMaker,
	v201CallMaker handlers.CallMaker,
	previousChargeStationId string,
	retryAfter time.Duration) string {

	requests, err := engine.ListLogRequests(ctx, 50, previousChargeStationId)
	if err != nil {
		slog.Error("list log requests", slog.String("err", err.Error()))
		return previousChargeStationId
	}
	if len(requests) > 0 {
		previousChargeStationId = requests[len(requests)-1].ChargeStationId
	} else {
		previousChargeStationId = ""
	}

	for _, req := range requests {
		if req.Status != store.LogRequestStatusPending {
			continue
		}
		if !clock.Now().After(req.SendAfter) {
			continue
		}

		csId := req.ChargeStationId
		details, err := engine.LookupChargeStationRuntimeDetails(ctx, csId)
		if err != nil {
			slog.Error("lookup charge station runtime details for log request",
				slog.String("err", err.Error()),
				slog.String("chargeStationId", csId))
			continue
		}
		if details == nil {
			slog.Error("no runtime details for charge station",
				slog.String("chargeStationId", csId))
			continue
		}

		// Update send after for retry
		req.SendAfter = clock.Now().Add(retryAfter)
		if err := engine.SetLogRequest(ctx, csId, req); err != nil {
			slog.Error("update log request send after", slog.String("err", err.Error()))
			continue
		}

		var sendErr error
		switch details.OcppVersion {
		case "1.6":
			ocppReq := &ocpp16.GetLogJson{
				LogType:   ocpp16.LogEnumType(req.LogType),
				RequestId: req.RequestId,
				Log: ocpp16.LogParametersType{
					RemoteLocation: req.RemoteLocation,
				},
				Retries:       req.Retries,
				RetryInterval: req.RetryInterval,
			}
			if req.OldestTimestamp != nil {
				oldestTimestamp := req.OldestTimestamp.Format(time.RFC3339)
				ocppReq.Log.OldestTimestamp = &oldestTimestamp
			}
			if req.LatestTimestamp != nil {
				latestTimestamp := req.LatestTimestamp.Format(time.RFC3339)
				ocppReq.Log.LatestTimestamp = &latestTimestamp
			}
			sendErr = v16CallMaker.Send(ctx, csId, ocppReq)
		case "2.0.1":
			ocppReq := &ocpp201.GetLogRequestJson{
				LogType:   ocpp201.LogEnumType(req.LogType),
				RequestId: req.RequestId,
				Log: ocpp201.LogParametersType{
					RemoteLocation: req.RemoteLocation,
				},
				Retries:       req.Retries,
				RetryInterval: req.RetryInterval,
			}
			if req.OldestTimestamp != nil {
				oldestTimestamp := req.OldestTimestamp.Format(time.RFC3339)
				ocppReq.Log.OldestTimestamp = &oldestTimestamp
			}
			if req.LatestTimestamp != nil {
				latestTimestamp := req.LatestTimestamp.Format(time.RFC3339)
				ocppReq.Log.LatestTimestamp = &latestTimestamp
			}
			sendErr = v201CallMaker.Send(ctx, csId, ocppReq)
		default:
			slog.Warn("unsupported OCPP version for log request",
				slog.String("chargeStationId", csId),
				slog.String("ocppVersion", details.OcppVersion))
			if err := engine.DeleteLogRequest(ctx, csId); err != nil {
				slog.Error("delete log request", slog.String("err", err.Error()))
			}
			continue
		}

		if sendErr != nil {
			slog.Error("send get log request",
				slog.String("err", sendErr.Error()),
				slog.String("chargeStationId", csId))
			continue
		}

		// Request sent successfully, delete from pending queue
		if err := engine.DeleteLogRequest(ctx, csId); err != nil {
			slog.Error("delete log request after send",
				slog.String("err", err.Error()),
				slog.String("chargeStationId", csId))
		}

		slog.Info("sent get log request",
			slog.String("chargeStationId", csId),
			slog.String("logType", req.LogType),
			slog.String("remoteLocation", req.RemoteLocation))
	}

	return previousChargeStationId
}

