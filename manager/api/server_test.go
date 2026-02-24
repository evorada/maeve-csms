// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
	clockTest "k8s.io/utils/clock/testing"
)

func TestRegisterChargeStation(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001", strings.NewReader(`{"securityProfile":0}`))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(b))
}

func TestLookupChargeStationAuth(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	err := engine.SetChargeStationAuth(context.Background(), "cs001", &store.ChargeStationAuth{
		SecurityProfile: 1,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/auth", strings.NewReader("{}"))
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	invalidUsernameAllowed := false
	want := &api.ChargeStationAuth{
		SecurityProfile:        1,
		InvalidUsernameAllowed: &invalidUsernameAllowed,
	}

	got := new(api.ChargeStationAuth)
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Equal(t, want, got)
}

func TestLookupChargeStationAuthThatDoesNotExist(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/cs/unknown/auth", strings.NewReader("{}"))
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Result().StatusCode)
}

func TestSetToken(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	token := api.Token{
		CacheMode:   "ALWAYS",
		ContractId:  "GB-TWK-012345678-V",
		CountryCode: "GB",
		Issuer:      "Thoughtworks",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "012345678",
		Valid:       true,
	}
	tokenPayload, err := json.Marshal(token)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/token", bytes.NewReader(tokenPayload))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(b))

	want := &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "012345678",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
	}

	got, err := engine.LookupToken(context.Background(), "012345678")

	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`, got.LastUpdated)
	got.LastUpdated = ""

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestLookupToken(t *testing.T) {
	server, r, engine, c := setupServer(t)
	now := c.Now()
	defer server.Close()

	err := engine.SetToken(context.Background(), &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "012345678",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
		LastUpdated: now.Format(time.RFC3339),
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/token/012345678", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	decoder := json.NewDecoder(rr.Result().Body)
	var got api.Token
	err = decoder.Decode(&got)
	require.NoError(t, err)

	lastUpdatedStr := now.Format(time.RFC3339)
	lastUpdated, err := time.Parse(time.RFC3339, lastUpdatedStr)
	require.NoError(t, err)
	want := api.Token{
		CacheMode:   "ALWAYS",
		ContractId:  "GBTWK012345678V",
		CountryCode: "GB",
		Issuer:      "Thoughtworks",
		LastUpdated: &lastUpdated,
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "012345678",
		Valid:       true,
	}

	assert.Equal(t, want, got)
}

func TestListTokens(t *testing.T) {
	ctx := context.Background()
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	tokens := make([]*store.Token, 20)
	for i := 0; i < 20; i++ {
		tokens[i] = &store.Token{
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         fmt.Sprintf("123456%02d", i),
			ContractId:  "GBTWK012345678V",
			Issuer:      "TWK",
			Valid:       true,
			CacheMode:   store.CacheModeAllowed,
		}
	}

	for _, token := range tokens {
		err := engine.SetToken(ctx, token)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	decoder := json.NewDecoder(rr.Result().Body)
	var got []api.Token
	err := decoder.Decode(&got)
	require.NoError(t, err)

	t.Logf("got: %+v", got)
}

func TestSetCertificate(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	cert := generateCertificate(t)
	pemCert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
	encodedPemCert := strings.Replace(string(pemCert), "\n", "\\n", -1)

	req := httptest.NewRequest(http.MethodPost, "/certificate", strings.NewReader(fmt.Sprintf(`{"certificate":"%s"}`, encodedPemCert)))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(b))

	b64Hash := getCertificateHash(cert)

	got, err := engine.LookupCertificate(context.Background(), b64Hash)
	require.NoError(t, err)

	assert.Equal(t, string(pemCert), got)
}

func TestDeleteCertificate(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	cert := generateCertificate(t)
	pemCert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	err := engine.SetCertificate(context.Background(), string(pemCert))
	require.NoError(t, err)

	b64Hash := getCertificateHash(cert)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/certificate/%s", b64Hash), nil)
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(b))

	got, err := engine.LookupCertificate(context.Background(), b64Hash)
	require.NoError(t, err)

	assert.Equal(t, "", got)
}

func TestLookupCertificate(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	cert := generateCertificate(t)
	pemCert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	err := engine.SetCertificate(context.Background(), string(pemCert))
	require.NoError(t, err)

	b64Hash := getCertificateHash(cert)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/certificate/%s", b64Hash), nil)
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.JSONEq(t, fmt.Sprintf(`{"certificate":"%s"}`, strings.Replace(string(pemCert), "\n", "\\n", -1)), string(b))
}

func TestRegisterLocation(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/location/loc001", strings.NewReader(`{
  "name": "Gent Zuid",
  "address": "F.Rooseveltlaan 3A",
  "city": "Gent",
  "party_id": "TWK",
  "postal_code": "9000",
  "country": "BEL",
  "country_code": "BEL",
  "coordinates": {
    "latitude": "51.047599",
    "longitude": "3.729944"
  },
  "parking_type": "ON_STREET"
}`))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(b))

	want := &store.Location{
		Address: "F.Rooseveltlaan 3A",
		City:    "Gent",
		Coordinates: store.GeoLocation{
			Latitude:  "51.047599",
			Longitude: "3.729944",
		},
		Country:     "BEL",
		Evses:       &[]store.Evse{},
		Id:          "loc001",
		Name:        "Gent Zuid",
		ParkingType: "ON_STREET",
		PostalCode:  "9000",
	}
	got, err := engine.LookupLocation(context.Background(), "loc001")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func setupServer(t *testing.T) (*httptest.Server, *chi.Mux, store.Engine, clock.PassiveClock) {
	now := time.Now().UTC()
	c := clockTest.NewFakePassiveClock(now)
	engine := inmemory.NewStore(c)
	ocpiApi := ocpi.NewOCPI(engine, nil, "GB", "TWK")

	srv, err := api.NewServer(engine, c, ocpiApi)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Use(api.ValidationMiddleware)
	r.Mount("/", api.Handler(srv))
	server := httptest.NewServer(r)

	return server, r, engine, c
}

func generateCertificate(t *testing.T) *x509.Certificate {
	keyPair, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Thoughtworks"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &keyPair.PublicKey, keyPair)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(derBytes)
	require.NoError(t, err)

	return cert
}

func getCertificateHash(cert *x509.Certificate) string {
	hash := sha256.Sum256(cert.Raw)
	b64Hash := base64.RawURLEncoding.EncodeToString(hash[:])
	return b64Hash
}

func TestCreateReservation(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	expiryTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"reservationId": 12345,
		"connectorId": 1,
		"idTag": "USER001",
		"expiryDate": "%s"
	}`, expiryTime)

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001/reservation", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)
}

func TestCreateReservationWithParentIdTag(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	expiryTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	body := fmt.Sprintf(`{
		"reservationId": 12346,
		"connectorId": 2,
		"idTag": "USER002",
		"parentIdTag": "PARENT001",
		"expiryDate": "%s"
	}`, expiryTime)

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001/reservation", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)
}

func TestCancelReservation(t *testing.T) {
	server, r, engine, c := setupServer(t)
	defer server.Close()

	// First create a reservation
	ctx := context.Background()
	parentTag := "PARENT001"
	err := engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   12345,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "USER001",
		ParentIdTag:     &parentTag,
		ExpiryDate:      c.Now().Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       c.Now(),
	})
	require.NoError(t, err)

	// Now cancel it
	req := httptest.NewRequest(http.MethodDelete, "/cs/cs001/reservation/12345", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)

	// Verify it was cancelled
	reservation, err := engine.GetReservation(ctx, 12345)
	require.NoError(t, err)
	assert.Equal(t, store.ReservationStatusCancelled, reservation.Status)
}

func TestCancelReservationNotFound(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodDelete, "/cs/cs001/reservation/99999", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Result().StatusCode)
}

func TestCancelReservationWrongChargeStation(t *testing.T) {
	server, r, engine, c := setupServer(t)
	defer server.Close()

	// Create a reservation for cs001
	ctx := context.Background()
	err := engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   12345,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "USER001",
		ExpiryDate:      c.Now().Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       c.Now(),
	})
	require.NoError(t, err)

	// Try to cancel it from cs002
	req := httptest.NewRequest(http.MethodDelete, "/cs/cs002/reservation/12345", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Result().StatusCode)
}

func TestListReservations(t *testing.T) {
	server, r, engine, c := setupServer(t)
	defer server.Close()

	// Create some reservations
	ctx := context.Background()
	err := engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   12345,
		ChargeStationId: "cs001",
		ConnectorId:     1,
		IdTag:           "USER001",
		ExpiryDate:      c.Now().Add(1 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       c.Now(),
	})
	require.NoError(t, err)

	err = engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   12346,
		ChargeStationId: "cs001",
		ConnectorId:     2,
		IdTag:           "USER002",
		ExpiryDate:      c.Now().Add(2 * time.Hour),
		Status:          store.ReservationStatusAccepted,
		CreatedAt:       c.Now(),
	})
	require.NoError(t, err)

	// Create a cancelled reservation (should not appear in active list)
	err = engine.CreateReservation(ctx, &store.Reservation{
		ReservationId:   12347,
		ChargeStationId: "cs001",
		ConnectorId:     3,
		IdTag:           "USER003",
		ExpiryDate:      c.Now().Add(1 * time.Hour),
		Status:          store.ReservationStatusCancelled,
		CreatedAt:       c.Now(),
	})
	require.NoError(t, err)

	// List active reservations
	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/reservations?status=active", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)

	var response api.ReservationList
	err = json.NewDecoder(rr.Result().Body).Decode(&response)
	require.NoError(t, err)

	// Should only have 2 active reservations
	assert.Len(t, response.Reservations, 2)
	assert.Equal(t, int32(12345), response.Reservations[0].ReservationId)
	assert.Equal(t, int32(12346), response.Reservations[1].ReservationId)
}

func TestListReservationsEmpty(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/reservations", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)

	var response api.ReservationList
	err := json.NewDecoder(rr.Result().Body).Decode(&response)
	require.NoError(t, err)

	assert.Empty(t, response.Reservations)
}
