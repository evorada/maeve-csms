// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

type testInstalledCertificateStore struct {
	lastChargeStationId string
	lastPayload         *store.ChargeStationInstallCertificates
}

func (s *testInstalledCertificateStore) UpdateChargeStationInstallCertificates(_ context.Context, chargeStationId string, certificates *store.ChargeStationInstallCertificates) error {
	s.lastChargeStationId = chargeStationId
	s.lastPayload = certificates
	return nil
}

func (s *testInstalledCertificateStore) LookupChargeStationInstallCertificates(_ context.Context, _ string) (*store.ChargeStationInstallCertificates, error) {
	return nil, nil
}

func (s *testInstalledCertificateStore) ListChargeStationInstallCertificates(_ context.Context, _ int, _ string) ([]*store.ChargeStationInstallCertificates, error) {
	return nil, nil
}

func TestGetInstalledCertificateIdsResultHandler(t *testing.T) {
	certStore := &testInstalledCertificateStore{}
	handler := ocpp201.GetInstalledCertificateIdsResultHandler{Store: certStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.GetInstalledCertificateIdsRequestJson{
			CertificateType: []types.GetCertificateIdUseEnumType{
				types.GetCertificateIdUseEnumTypeCSMSRootCertificate,
				types.GetCertificateIdUseEnumTypeMORootCertificate,
			},
		}
		resp := &types.GetInstalledCertificateIdsResponseJson{
			Status: types.GetInstalledCertificateStatusEnumTypeAccepted,
			CertificateHashDataChain: []types.CertificateHashDataChainType{
				{
					CertificateHashData: types.CertificateHashDataType{SerialNumber: "12345678"},
					CertificateType:     types.GetCertificateIdUseEnumTypeCSMSRootCertificate,
				},
				{
					CertificateHashData: types.CertificateHashDataType{SerialNumber: "87654321"},
					CertificateType:     types.GetCertificateIdUseEnumTypeV2GCertificateChain,
				},
			},
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	require.Equal(t, "cs001", certStore.lastChargeStationId)
	require.NotNil(t, certStore.lastPayload)
	require.Len(t, certStore.lastPayload.Certificates, 2)
	require.Equal(t, "12345678", certStore.lastPayload.Certificates[0].CertificateId)
	require.Equal(t, store.CertificateTypeCSMS, certStore.lastPayload.Certificates[0].CertificateType)
	require.Equal(t, store.CertificateInstallationAccepted, certStore.lastPayload.Certificates[0].CertificateInstallationStatus)
	require.Equal(t, store.CertificateTypeV2G, certStore.lastPayload.Certificates[1].CertificateType)

	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"get_installed_certificate.types":          "CSMSRootCertificate,MORootCertificate",
		"get_installed_certificate.status":         "Accepted",
		"get_installed_certificate.returned_count": int64(2),
	})
}

func TestGetInstalledCertificateIdsResultHandler_DoesNotPersistWhenNotAccepted(t *testing.T) {
	certStore := &testInstalledCertificateStore{}
	handler := ocpp201.GetInstalledCertificateIdsResultHandler{Store: certStore}

	req := &types.GetInstalledCertificateIdsRequestJson{}
	resp := &types.GetInstalledCertificateIdsResponseJson{Status: types.GetInstalledCertificateStatusEnumTypeNotFound}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)
	require.Nil(t, certStore.lastPayload)
}
