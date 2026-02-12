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

func TestDeleteCertificateResultHandler_HandleCallResult(t *testing.T) {
	handler := ocpp16.DeleteCertificateResultHandler{}
	ctx := context.Background()
	chargeStationId := "cs001"

	tests := []struct {
		name     string
		request  ocpp.Request
		response ocpp.Response
	}{
		{
			name: "accepted with SHA256",
			request: &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
					IssuerNameHash: "abc123",
					IssuerKeyHash:  "def456",
					SerialNumber:   "1234567890",
				},
			},
			response: &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted with SHA384",
			request: &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA384,
					IssuerNameHash: "abc123",
					IssuerKeyHash:  "def456",
					SerialNumber:   "9876543210",
				},
			},
			response: &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusAccepted,
			},
		},
		{
			name: "accepted with SHA512",
			request: &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA512,
					IssuerNameHash: "abc123",
					IssuerKeyHash:  "def456",
					SerialNumber:   "5555555555",
				},
			},
			response: &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusAccepted,
			},
		},
		{
			name: "failed",
			request: &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
					IssuerNameHash: "abc123",
					IssuerKeyHash:  "def456",
					SerialNumber:   "1234567890",
				},
			},
			response: &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusFailed,
			},
		},
		{
			name: "not found",
			request: &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
					IssuerNameHash: "unknown",
					IssuerKeyHash:  "unknown",
					SerialNumber:   "0000000000",
				},
			},
			response: &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusNotFound,
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

func TestDeleteCertificateResultHandler_AllHashAlgorithms(t *testing.T) {
	handler := ocpp16.DeleteCertificateResultHandler{}
	ctx := context.Background()

	algorithms := []types.HashAlgorithmEnumType{
		types.HashAlgorithmEnumTypeSHA256,
		types.HashAlgorithmEnumTypeSHA384,
		types.HashAlgorithmEnumTypeSHA512,
	}

	for _, algo := range algorithms {
		t.Run(string(algo), func(t *testing.T) {
			request := &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  algo,
					IssuerNameHash: "test_issuer_name_hash",
					IssuerKeyHash:  "test_issuer_key_hash",
					SerialNumber:   "test_serial",
				},
			}
			response := &types.DeleteCertificateResponseJson{
				Status: types.DeleteCertificateResponseJsonStatusAccepted,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}

func TestDeleteCertificateResultHandler_AllStatuses(t *testing.T) {
	handler := ocpp16.DeleteCertificateResultHandler{}
	ctx := context.Background()

	statuses := []types.DeleteCertificateResponseJsonStatus{
		types.DeleteCertificateResponseJsonStatusAccepted,
		types.DeleteCertificateResponseJsonStatusFailed,
		types.DeleteCertificateResponseJsonStatusNotFound,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			request := &types.DeleteCertificateJson{
				CertificateHashData: types.CertificateHashDataType{
					HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
					IssuerNameHash: "test",
					IssuerKeyHash:  "test",
					SerialNumber:   "test",
				},
			}
			response := &types.DeleteCertificateResponseJson{
				Status: status,
			}

			err := handler.HandleCallResult(ctx, "cs001", request, response, nil)
			require.NoError(t, err)
		})
	}
}
