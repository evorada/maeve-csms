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

type DeleteCertificateResultHandler struct{}

func (h DeleteCertificateResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.DeleteCertificateJson)
	resp := response.(*types.DeleteCertificateResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("delete_certificate.hash_algorithm", string(req.CertificateHashData.HashAlgorithm)),
		attribute.String("delete_certificate.serial_number", req.CertificateHashData.SerialNumber),
		attribute.String("delete_certificate.status", string(resp.Status)))

	if resp.Status == types.DeleteCertificateResponseJsonStatusAccepted {
		slog.Info("delete certificate accepted",
			"chargeStationId", chargeStationId,
			"serialNumber", req.CertificateHashData.SerialNumber,
			"hashAlgorithm", req.CertificateHashData.HashAlgorithm)
	} else {
		slog.Warn("delete certificate not accepted",
			"chargeStationId", chargeStationId,
			"serialNumber", req.CertificateHashData.SerialNumber,
			"status", resp.Status)
	}

	return nil
}
