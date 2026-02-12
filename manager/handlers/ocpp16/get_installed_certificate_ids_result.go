// SPDX-License-Identifier: Apache-2.0

package ocpp16

import (
	"context"
	"log/slog"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetInstalledCertificateIdsResultHandler struct{}

func (h GetInstalledCertificateIdsResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetInstalledCertificateIdsJson)
	resp := response.(*types.GetInstalledCertificateIdsResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("get_installed_certificate_ids.certificate_type", string(req.CertificateType)),
		attribute.String("get_installed_certificate_ids.status", string(resp.Status)))

	if resp.CertificateHashData != nil {
		span.SetAttributes(attribute.Int("get_installed_certificate_ids.certificate_count", len(resp.CertificateHashData)))
	}

	if resp.Status == types.GetInstalledCertificateIdsResponseJsonStatusAccepted {
		slog.Info("get installed certificate ids accepted",
			"chargeStationId", chargeStationId,
			"certificateType", req.CertificateType,
			"certificateCount", len(resp.CertificateHashData))
	} else {
		slog.Warn("get installed certificate ids not found",
			"chargeStationId", chargeStationId,
			"certificateType", req.CertificateType,
			"status", resp.Status)
	}

	return nil
}
