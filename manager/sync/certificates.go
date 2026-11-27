// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/handlers"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"golang.org/x/exp/slog"
	"k8s.io/utils/clock"
)

// SyncCertificates runs SyncCertificatesOnce every runEvery until ctx is cancelled.
func SyncCertificates(ctx context.Context, engine store.Engine, clock clock.PassiveClock, v16CallMaker, v201CallMaker handlers.CallMaker, runEvery, retryAfter time.Duration) {
	var previousChargeStationId string
	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down sync certificates")
			return
		case <-time.After(runEvery):
			previousChargeStationId = SyncCertificatesOnce(ctx, engine, clock, v16CallMaker, v201CallMaker, previousChargeStationId, retryAfter)
		}
	}
}

// SyncCertificatesOnce sends the pending certificates of the next page of charge stations
// after the charge station after, and returns where the next pass starts (as
// SyncSettingsOnce does). A certificate that is sent is not sent again before retryAfter.
func SyncCertificatesOnce(ctx context.Context, engine store.Engine, clock clock.PassiveClock, v16CallMaker, v201CallMaker handlers.CallMaker, after string, retryAfter time.Duration) (next string) {
	slog.Info("checking for pending charge station certificates changes")
	certificateInstallations, err := engine.ListChargeStationInstallCertificates(ctx, 50, after)
	if err != nil {
		slog.Error("list charge station certificates", slog.String("err", err.Error()))
		return after
	}
	if len(certificateInstallations) > 0 {
		next = certificateInstallations[len(certificateInstallations)-1].ChargeStationId
	}
	pendingCertificateInstallation := filterPendingCertificatesInstallations(certificateInstallations)
	for _, pendingCertificateInstallation := range pendingCertificateInstallation {
		details, err := engine.LookupChargeStationRuntimeDetails(ctx, pendingCertificateInstallation.ChargeStationId)
		if err != nil {
			slog.Error("lookup charge station runtime details", slog.String("err", err.Error()),
				slog.String("chargeStationId", pendingCertificateInstallation.ChargeStationId))
			continue
		}
		// a charge station that has not booted yet: its version is not known
		if details == nil {
			continue
		}
		var callMaker handlers.CallMaker
		if details.OcppVersion == "1.6" {
			callMaker = v16CallMaker
		} else {
			callMaker = v201CallMaker
		}

		csId := pendingCertificateInstallation.ChargeStationId
		for _, certificate := range pendingCertificateInstallation.Certificates {
			if certificate.CertificateInstallationStatus != store.CertificateInstallationAccepted && clock.Now().After(certificate.SendAfter) {
				slog.Info("updating charge station certificates", slog.String("chargeStationId", csId),
					slog.String("certificate", certificate.CertificateId),
					slog.String("OcppVersion", details.OcppVersion))
				certificate.SendAfter = clock.Now().Add(retryAfter)
				err = engine.UpdateChargeStationInstallCertificates(ctx, csId, &store.ChargeStationInstallCertificates{
					Certificates: []*store.ChargeStationInstallCertificate{
						certificate,
					},
				})
				if err != nil {
					slog.Error("update charge station certificates", slog.String("err", err.Error()))
					continue
				}

				if certificate.CertificateType == store.CertificateTypeChargeStation ||
					certificate.CertificateType == store.CertificateTypeEVCC {
					var certType ocpp201.CertificateSigningUseEnumType
					if certificate.CertificateType == store.CertificateTypeChargeStation {
						certType = ocpp201.CertificateSigningUseEnumTypeChargingStationCertificate
					} else {
						certType = ocpp201.CertificateSigningUseEnumTypeV2GCertificate
					}
					req := &ocpp201.CertificateSignedRequestJson{
						CertificateChain: certificate.CertificateData,
						CertificateType:  &certType,
					}
					err = callMaker.Send(ctx, csId, req)
					if err != nil {
						slog.Error("send certificate signed request", slog.String("err", err.Error()),
							slog.String("chargeStationId", csId), slog.String("certificate", certificate.CertificateId))
					}
				} else {
					var certType ocpp201.InstallCertificateUseEnumType
					switch certificate.CertificateType {
					case store.CertificateTypeCSMS:
						certType = ocpp201.InstallCertificateUseEnumTypeCSMSRootCertificate
					case store.CertificateTypeV2G:
						certType = ocpp201.InstallCertificateUseEnumTypeV2GRootCertificate
					case store.CertificateTypeMO:
						certType = ocpp201.InstallCertificateUseEnumTypeMORootCertificate
					case store.CertificateTypeMF:
						certType = ocpp201.InstallCertificateUseEnumTypeManufacturerRootCertificate
					}
					req := &ocpp201.InstallCertificateRequestJson{
						CertificateType: certType,
						Certificate:     certificate.CertificateData,
					}
					err = callMaker.Send(ctx, csId, req)
					if err != nil {
						slog.Error("send install certificate request", slog.String("err", err.Error()),
							slog.String("chargeStationId", csId), slog.String("certificate", certificate.CertificateId))
					}
				}
			}
		}
	}
	return next
}

func filterPendingCertificatesInstallations(certificateInstallations []*store.ChargeStationInstallCertificates) []*store.ChargeStationInstallCertificates {
	var pendingCertificateInstallations []*store.ChargeStationInstallCertificates
	for _, certificateInstallation := range certificateInstallations {
		for _, certificate := range certificateInstallation.Certificates {
			if certificate.CertificateInstallationStatus != store.CertificateInstallationAccepted {
				pendingCertificateInstallations = append(pendingCertificateInstallations, certificateInstallation)
				break
			}
		}
	}
	return pendingCertificateInstallations
}
