// SPDX-License-Identifier: Apache-2.0

package ocpp201

import (
	"context"
	"strings"

	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type GetInstalledCertificateIdsResultHandler struct {
	Store store.ChargeStationInstallCertificatesStore
}

func (h GetInstalledCertificateIdsResultHandler) HandleCallResult(ctx context.Context, chargeStationId string, request ocpp.Request, response ocpp.Response, state any) error {
	req := request.(*types.GetInstalledCertificateIdsRequestJson)
	resp := response.(*types.GetInstalledCertificateIdsResponseJson)

	span := trace.SpanFromContext(ctx)

	var certTypes []string
	if req.CertificateType != nil {
		for _, ct := range req.CertificateType {
			certTypes = append(certTypes, string(ct))
		}
	}

	span.SetAttributes(
		attribute.String("get_installed_certificate.types", strings.Join(certTypes, ",")),
		attribute.String("get_installed_certificate.status", string(resp.Status)),
		attribute.Int("get_installed_certificate.returned_count", len(resp.CertificateHashDataChain)),
	)

	if resp.Status != types.GetInstalledCertificateStatusEnumTypeAccepted || h.Store == nil {
		return nil
	}

	certificates := make([]*store.ChargeStationInstallCertificate, 0, len(resp.CertificateHashDataChain))
	for _, chain := range resp.CertificateHashDataChain {
		storeType := mapCertificateType(chain.CertificateType)
		if storeType == "" {
			continue
		}

		certificates = append(certificates, &store.ChargeStationInstallCertificate{
			CertificateType:               storeType,
			CertificateId:                 chain.CertificateHashData.SerialNumber,
			CertificateData:               "",
			CertificateInstallationStatus: store.CertificateInstallationAccepted,
		})
	}

	if len(certificates) == 0 {
		return nil
	}

	return h.Store.UpdateChargeStationInstallCertificates(ctx, chargeStationId, &store.ChargeStationInstallCertificates{
		Certificates: certificates,
	})
}

func mapCertificateType(certificateType types.GetCertificateIdUseEnumType) store.CertificateType {
	switch certificateType {
	case types.GetCertificateIdUseEnumTypeMORootCertificate:
		return store.CertificateTypeMO
	case types.GetCertificateIdUseEnumTypeManufacturerRootCertificate:
		return store.CertificateTypeMF
	case types.GetCertificateIdUseEnumTypeCSMSRootCertificate:
		return store.CertificateTypeCSMS
	case types.GetCertificateIdUseEnumTypeV2GRootCertificate, types.GetCertificateIdUseEnumTypeV2GCertificateChain:
		return store.CertificateTypeV2G
	default:
		return ""
	}
}
