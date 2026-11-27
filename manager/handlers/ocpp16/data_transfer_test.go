// SPDX-License-Identifier: Apache-2.0

package ocpp16_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/events"
	"github.com/thoughtworks/maeve-csms/manager/handlers"
	handlers16 "github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpp"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/has2be"
	"github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp201"
	"github.com/thoughtworks/maeve-csms/manager/schemas"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	"github.com/thoughtworks/maeve-csms/manager/transport"
	"k8s.io/utils/clock"
)

func TestDataTransferHandlerRoutesCall(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {
				"GetCertificateStatus": {
					NewRequest:     func() ocpp.Request { return new(types.GetCertificateStatusRequestJson) },
					RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
					ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
					Handler: handlers.CallHandlerFunc(func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
						ocspResult := "ocsp-result"
						return &types.GetCertificateStatusResponseJson{
							Status:     types.GetCertificateStatusEnumTypeAccepted,
							OcspResult: &ocspResult,
						}, nil
					}),
				},
			},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	got, err := dth.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)

	expectedData := "{\"ocspResult\":\"ocsp-result\",\"status\":\"Accepted\"}"
	want := &ocpp16.DataTransferResponseJson{
		Data:   &expectedData,
		Status: ocpp16.DataTransferResponseJsonStatusAccepted,
	}

	assert.Equal(t, want, got)
}

func TestDataTransferHandlerWithUnknownVendorId(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		CallRoutes: map[string]map[string]handlers.CallRoute{},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	got, err := dth.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)

	want := &ocpp16.DataTransferResponseJson{
		Status: ocpp16.DataTransferResponseJsonStatusUnknownVendorId,
	}

	assert.Equal(t, want, got)
}

func TestDataTransferHandlerWithUnknownMessageId(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	got, err := dth.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)

	want := &ocpp16.DataTransferResponseJson{
		Status: ocpp16.DataTransferResponseJsonStatusUnknownMessageId,
	}

	assert.Equal(t, want, got)
}

func TestDataTransferHandlerWithEmptyResult(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {
				"GetCertificateStatus": {
					NewRequest:     func() ocpp.Request { return new(types.GetCertificateStatusRequestJson) },
					RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
					ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
					Handler: handlers.CallHandlerFunc(func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
						return nil, nil
					}),
				},
			},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	got, err := dth.HandleCall(context.Background(), "cs001", req)
	require.NoError(t, err)

	want := &ocpp16.DataTransferResponseJson{
		Status: ocpp16.DataTransferResponseJsonStatusAccepted,
	}

	assert.Equal(t, want, got)
}

func TestDataTransferHandlerWithErrorResult(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {
				"GetCertificateStatus": {
					NewRequest:     func() ocpp.Request { return new(types.GetCertificateStatusRequestJson) },
					RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
					ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
					Handler: handlers.CallHandlerFunc(func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
						return nil, errors.New("expected error")
					}),
				},
			},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	_, err := dth.HandleCall(context.Background(), "cs001", req)
	require.ErrorContains(t, err, "expected error")
}

func TestDataTransferHandlerCantUnmarshalRequest(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {
				"GetCertificateStatus": {
					NewRequest:     func() ocpp.Request { return new(noUnmarshalRequest) },
					RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
					ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
					Handler: handlers.CallHandlerFunc(func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
						ocspResult := "ocsp-result"
						return &types.GetCertificateStatusResponseJson{
							Status:     types.GetCertificateStatusEnumTypeAccepted,
							OcspResult: &ocspResult,
						}, nil
					}),
				},
			},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	_, err := dth.HandleCall(context.Background(), "cs001", req)
	require.ErrorContains(t, err, "unmarshalling org.openchargealliance.iso15118pnc:GetCertificateStatus data transfer data")
}

type ArrayResponse []string

func (*ArrayResponse) IsResponse() {}

func TestDataTransferHandlerCantMarshalResponse(t *testing.T) {
	dth := handlers16.DataTransferHandler{
		SchemaFS: schemas.OcppSchemas,
		CallRoutes: map[string]map[string]handlers.CallRoute{
			"org.openchargealliance.iso15118pnc": {
				"GetCertificateStatus": {
					NewRequest:     func() ocpp.Request { return new(types.GetCertificateStatusRequestJson) },
					RequestSchema:  "ocpp201/GetCertificateStatusRequest.json",
					ResponseSchema: "ocpp201/GetCertificateStatusResponse.json",
					Handler: handlers.CallHandlerFunc(func(ctx context.Context, chargeStationId string, request ocpp.Request) (ocpp.Response, error) {
						return new(noMarshalResponse), nil
					}),
				},
			},
		},
	}

	getCertificateStatusMessageId := "GetCertificateStatus"
	getCertificateStatusData := "{\"ocspRequestData\":{\"hashAlgorithm\":\"SHA256\",\"issuerKeyHash\":\"key-hash\",\"issuerNameHash\":\"name-hash\",\"responderURL\":\"http://ocsp-qa.example.com:8080\",\"serialNumber\":\"serial-number\"}}"
	req := &ocpp16.DataTransferJson{
		VendorId:  "org.openchargealliance.iso15118pnc",
		MessageId: &getCertificateStatusMessageId,
		Data:      &getCertificateStatusData,
	}

	_, err := dth.HandleCall(context.Background(), "cs001", req)
	require.ErrorContains(t, err, "marshalling org.openchargealliance.iso15118pnc:GetCertificateStatus data transfer data")
}

type noUnmarshalRequest struct{}

func (*noUnmarshalRequest) IsRequest() {}

func (*noUnmarshalRequest) UnmarshalJSON(data []byte) error {
	return errors.New("expected to fail")
}

type noUnmarshalResponse struct{}

func (*noUnmarshalResponse) IsResponse() {}

func (*noUnmarshalResponse) UnmarshalJSON(data []byte) error {
	return errors.New("expected to fail")
}

type noMarshalResponse struct{}

func (*noMarshalResponse) IsResponse() {}

func (*noMarshalResponse) MarshalJSON() ([]byte, error) {
	return nil, errors.New("expected to fail")
}

// recordingCertProvider records the CSRs it is asked to sign and returns a certificate.
type recordingCertProvider struct {
	csrs []string
}

func (r *recordingCertProvider) ProvideCertificate(_ context.Context, _ services.CertificateType, pemEncodedCSR string, _ string) (string, error) {
	r.csrs = append(r.csrs, pemEncodedCSR)
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("signed-certificate"),
	})), nil
}

func newPemEncodedCsr(t *testing.T, commonName string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}, key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: der,
	}))
}

// TestDataTransferRoutesHas2beSignCertificate sends a has2be SignCertificate request
// (ISO 15118 over an OCPP 1.6 DataTransfer) through the router: the wrapped payload must
// pass schema validation and reach the SignCertificate handler.
func TestDataTransferRoutesHas2beSignCertificate(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	emitter := &FakeEmitter{}
	certProvider := &recordingCertProvider{}

	router := handlers16.NewRouter(emitter,
		clock.RealClock{},
		engine,
		events.NopEmitter{},
		&tenancy.Resolver{Stations: engine},
		nil,
		nil,
		certProvider,
		nil,
		time.Minute,
		schemas.OcppSchemas, nil, nil)

	csr := newPemEncodedCsr(t, "cs001")
	typeOfCertificate := has2be.CertificateSigningUseEnumTypeV2GCertificate
	signCertificateData, err := json.Marshal(&has2be.SignCertificateRequestJson{
		Csr:               csr,
		TypeOfCertificate: &typeOfCertificate,
	})
	require.NoError(t, err)

	messageId := "SignCertificate"
	data := string(signCertificateData)
	requestPayload, err := json.Marshal(&ocpp16.DataTransferJson{
		VendorId:  "iso15118",
		MessageId: &messageId,
		Data:      &data,
	})
	require.NoError(t, err)

	router.Handle(context.Background(), "cs001", &transport.Message{
		MessageType:    transport.MessageTypeCall,
		Action:         "DataTransfer",
		MessageId:      "19223201",
		RequestPayload: requestPayload,
	})

	// the charge station receives a call result, not a call error
	require.NotNil(t, emitter.got)
	require.Equal(t, transport.MessageTypeCallResult, emitter.got.MessageType,
		"error %s: %s", emitter.got.ErrorCode, emitter.got.ErrorDescription)
	assert.Equal(t, "DataTransfer", emitter.got.Action)
	assert.Equal(t, "19223201", emitter.got.MessageId)

	var response ocpp16.DataTransferResponseJson
	require.NoError(t, json.Unmarshal(emitter.got.ResponsePayload, &response))
	assert.Equal(t, ocpp16.DataTransferResponseJsonStatusAccepted, response.Status)
	require.NotNil(t, response.Data)
	assert.JSONEq(t, `{"status":"Accepted"}`, *response.Data)

	// the SignCertificate handler was reached: the CSR was signed and the certificate
	// is waiting to be installed on the charge station
	assert.Equal(t, []string{csr}, certProvider.csrs)

	certs, err := engine.LookupChargeStationInstallCertificates(context.Background(), "cs001")
	require.NoError(t, err)
	require.NotNil(t, certs)
	require.Len(t, certs.Certificates, 1)
	assert.Equal(t, store.CertificateTypeEVCC, certs.Certificates[0].CertificateType)
	assert.Equal(t, store.CertificateInstallationPending, certs.Certificates[0].CertificateInstallationStatus)
}
