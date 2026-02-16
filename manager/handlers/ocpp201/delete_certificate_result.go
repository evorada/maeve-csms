// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"fmt"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/exp/slog"
)

type DeleteCertificateResultHandler struct {
	Store store.CertificateStore
}

func (h DeleteCertificateResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.DeleteCertificateRequestJson)
	resp := response.(*types.DeleteCertificateResponseJson)

	span := trace.SpanFromContext(ctx)
	certificateHash := getCertificateHashDataKey(req.CertificateHashData)

	span.SetAttributes(
		attribute.String("delete_certificate.serial_number", req.CertificateHashData.SerialNumber),
		attribute.String("delete_certificate.status", string(resp.Status)),
		attribute.String("delete_certificate.certificate_hash", certificateHash))

	if resp.Status != types.DeleteCertificateStatusEnumTypeAccepted {
		return nil
	}

	if h.Store == nil {
		slog.Warn("certificate store is nil, skipping certificate deletion", "charge_station_id", chargeStationId, "certificate_hash", certificateHash)
		return nil
	}

	if err := h.Store.DeleteCertificate(ctx, certificateHash); err != nil {
		return fmt.Errorf("delete certificate %s from store: %w", certificateHash, err)
	}

	return nil
}

func getCertificateHashDataKey(hashData types.CertificateHashDataType) string {
	return fmt.Sprintf("%s:%s:%s:%s", hashData.HashAlgorithm, hashData.IssuerNameHash, hashData.IssuerKeyHash, hashData.SerialNumber)
}
