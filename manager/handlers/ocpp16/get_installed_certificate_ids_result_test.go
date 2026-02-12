// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
)

func TestGetInstalledCertificateIdsResultHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.GetInstalledCertificateIdsResultHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name: "accepted with central system root certificates",
			request: &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeCentralSystemRootCertificate,
			},
			response: &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusAccepted,
				CertificateHashData: []types.CertificateHashDataType{
					{
						HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
						IssuerNameHash: "abc123",
						IssuerKeyHash:  "def456",
						SerialNumber:   "1234567890",
					},
				},
			},
		},
		{
			name: "accepted with manufacturer root certificates",
			request: &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeManufacturerRootCertificate,
			},
			response: &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusAccepted,
				CertificateHashData: []types.CertificateHashDataType{
					{
						HashAlgorithm:  types.HashAlgorithmEnumTypeSHA384,
						IssuerNameHash: "mfg_issuer",
						IssuerKeyHash:  "mfg_key",
						SerialNumber:   "MFG001",
					},
				},
			},
		},
		{
			name: "accepted with multiple certificates",
			request: &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeCentralSystemRootCertificate,
			},
			response: &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusAccepted,
				CertificateHashData: []types.CertificateHashDataType{
					{
						HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
						IssuerNameHash: "issuer1",
						IssuerKeyHash:  "key1",
						SerialNumber:   "serial1",
					},
					{
						HashAlgorithm:  types.HashAlgorithmEnumTypeSHA512,
						IssuerNameHash: "issuer2",
						IssuerKeyHash:  "key2",
						SerialNumber:   "serial2",
					},
				},
			},
		},
		{
			name: "not found for central system root",
			request: &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeCentralSystemRootCertificate,
			},
			response: &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusNotFound,
			},
		},
		{
			name: "not found for manufacturer root",
			request: &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeManufacturerRootCertificate,
			},
			response: &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusNotFound,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.HandleCallResult(ctx, chargeStationId, tt.request, tt.response, nil)
			require.NoError(t, err)
		})
	}
}

func TestGetInstalledCertificateIdsResultHandler_AllCertificateTypes(t *testing.T) {
	handler := ocpp16.GetInstalledCertificateIdsResultHandler{}
	ctx := context.Background()

	certTypes := []types.CertificateUseEnumType{
		types.CertificateUseEnumTypeCentralSystemRootCertificate,
		types.CertificateUseEnumTypeManufacturerRootCertificate,
	}

	for _, certType := range certTypes {
		t.Run(string(certType), func(t *testing.T) {
			request := &types.GetInstalledCertificateIdsJson{
				CertificateType: certType,
			}
			response := &types.GetInstalledCertificateIdsResponseJson{
				Status: types.GetInstalledCertificateIdsResponseJsonStatusAccepted,
				CertificateHashData: []types.CertificateHashDataType{
					{
						HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
						IssuerNameHash: "test_issuer",
						IssuerKeyHash:  "test_key",
						SerialNumber:   "test_serial",
					},
				},
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}

func TestGetInstalledCertificateIdsResultHandler_AllStatuses(t *testing.T) {
	handler := ocpp16.GetInstalledCertificateIdsResultHandler{}
	ctx := context.Background()

	statuses := []types.GetInstalledCertificateIdsResponseJsonStatus{
		types.GetInstalledCertificateIdsResponseJsonStatusAccepted,
		types.GetInstalledCertificateIdsResponseJsonStatusNotFound,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			request := &types.GetInstalledCertificateIdsJson{
				CertificateType: types.CertificateUseEnumTypeCentralSystemRootCertificate,
			}
			response := &types.GetInstalledCertificateIdsResponseJson{
				Status: status,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}
