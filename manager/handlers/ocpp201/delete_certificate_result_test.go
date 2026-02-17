// SPDX-License-Identifier: Apache-2.0

package ocpp201_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp201"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/testutil"
)

type mockCertificateStore struct {
	deleteCalledWith string
	deleteErr        error
}

func (m *mockCertificateStore) SetCertificate(_ context.Context, _ string) error { return nil }
func (m *mockCertificateStore) LookupCertificate(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockCertificateStore) DeleteCertificate(_ context.Context, certificateHash string) error {
	m.deleteCalledWith = certificateHash
	return m.deleteErr
}

func TestDeleteCertificateResultHandler(t *testing.T) {
	certificateHashData := types.CertificateHashDataType{
		HashAlgorithm:  types.HashAlgorithmEnumTypeSHA256,
		IssuerKeyHash:  "ABC123",
		IssuerNameHash: "ABCDEF",
		SerialNumber:   "12345678",
	}

	t.Run("stores deletion on accepted status", func(t *testing.T) {
		store := &mockCertificateStore{}
		handler := ocpp201.DeleteCertificateResultHandler{Store: store}

		tracer, exporter := testutil.GetTracer()
		ctx := context.Background()

		func() {
			ctx, span := tracer.Start(ctx, "test")
			defer span.End()

			req := &types.DeleteCertificateRequestJson{CertificateHashData: certificateHashData}
			resp := &types.DeleteCertificateResponseJson{Status: types.DeleteCertificateStatusEnumTypeAccepted}

			err := handler.HandleCallResult(ctx, "cs001", req, resp, nil)
			require.NoError(t, err)
		}()

		require.Equal(t, "SHA256:ABCDEF:ABC123:12345678", store.deleteCalledWith)
		testutil.AssertSpan(t, &exporter.GetSpans()[0], "test", map[string]any{
			"delete_certificate.serial_number":    "12345678",
			"delete_certificate.status":           "Accepted",
			"delete_certificate.certificate_hash": "SHA256:ABCDEF:ABC123:12345678",
		})
	})

	t.Run("does not delete when request was not accepted", func(t *testing.T) {
		store := &mockCertificateStore{}
		handler := ocpp201.DeleteCertificateResultHandler{Store: store}

		err := handler.HandleCallResult(context.Background(), "cs001",
			&types.DeleteCertificateRequestJson{CertificateHashData: certificateHashData},
			&types.DeleteCertificateResponseJson{Status: types.DeleteCertificateStatusEnumTypeNotFound},
			nil)
		require.NoError(t, err)
		require.Empty(t, store.deleteCalledWith)
	})

	t.Run("returns store deletion error", func(t *testing.T) {
		store := &mockCertificateStore{deleteErr: errors.New("delete failed")}
		handler := ocpp201.DeleteCertificateResultHandler{Store: store}

		err := handler.HandleCallResult(context.Background(), "cs001",
			&types.DeleteCertificateRequestJson{CertificateHashData: certificateHashData},
			&types.DeleteCertificateResponseJson{Status: types.DeleteCertificateStatusEnumTypeAccepted},
			nil)
		require.EqualError(t, err, "delete certificate SHA256:ABCDEF:ABC123:12345678 from store: delete failed")
	})
}
