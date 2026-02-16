// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

type testCertificateStore struct {
	deletedHashes []string
}

func (s *testCertificateStore) SetCertificate(ctx context.Context, pemCertificate string) error {
	return nil
}

func (s *testCertificateStore) LookupCertificate(ctx context.Context, certificateHash string) (string, error) {
	return "", nil
}

func (s *testCertificateStore) DeleteCertificate(ctx context.Context, certificateHash string) error {
	s.deletedHashes = append(s.deletedHashes, certificateHash)
	return nil
}

func TestDeleteCertificateResultHandler(t *testing.T) {
	certStore := &testCertificateStore{}
	handler := ocpp201.DeleteCertificateResultHandler{Store: certStore}

	tracer, exporter := testutil.GetTracer()

	ctx := context.Background()

	func() {
		ctx, span := tracer.Start(ctx, `test`)
		defer span.End()

		req := &types.DeleteCertificateRequestJson{
			CertificateHashData: types.CertificateHashDataType{
				HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
				IssuerKeyHash:  "ABC123",
				IssuerNameHash: "ABCDEF",
				SerialNumber:   "12345678",
			},
		}
		resp := &types.DeleteCertificateResponseJson{
			Status: types.DeleteCertificateStatusEnumTypeAccepted,
		}

		err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
		require.NoError(t, err)
	}()

	require.Equal(t, []string{"12345678"}, certStore.deletedHashes)
	testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
		"delete_certificate.serial_number": "12345678",
		"delete_certificate.status":        "Accepted",
	})
}

func TestDeleteCertificateResultHandler_NoDeleteOnRejected(t *testing.T) {
	certStore := &testCertificateStore{}
	handler := ocpp201.DeleteCertificateResultHandler{Store: certStore}

	req := &types.DeleteCertificateRequestJson{
		CertificateHashData: types.CertificateHashDataType{SerialNumber: "12345678"},
	}
	resp := &types.DeleteCertificateResponseJson{Status: types.DeleteCertificateStatusEnumTypeFailed}

	err := handler.HandleCallResult(context.Background(), "cs001", req, resp, nil)
	require.NoError(t, err)
	require.Empty(t, certStore.deletedHashes)
}
