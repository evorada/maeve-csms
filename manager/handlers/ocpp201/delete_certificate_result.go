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
)

type DeleteCertificateResultHandler struct {
	Store store.CertificateStore
}

func (h DeleteCertificateResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.DeleteCertificateRequestJson)
	resp := response.(*types.DeleteCertificateResponseJson)

	span := trace.SpanFromContext(ctx)

	span.SetAttributes(
		attribute.String("delete_certificate.serial_number", req.CertificateHashData.SerialNumber),
		attribute.String("delete_certificate.status", string(resp.Status)))

	if resp.Status == types.DeleteCertificateStatusEnumTypeAccepted && h.Store != nil {
		if err := h.Store.DeleteCertificate(ctx, req.CertificateHashData.SerialNumber); err != nil {
			return fmt.Errorf("delete certificate %s from store: %w", req.CertificateHashData.SerialNumber, err)
		}
	}

	return nil
}
