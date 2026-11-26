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
	"github.com/thoughtworks/maeve-csms/manager/handlers/ocpp16"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	types "github.com/thoughtworks/maeve-csms/manager/ocpp/ocpp16"
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

func TestSetAndLookupTokenWithExpiryTime(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	post := func(payload string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(payload))
		req.Header.Set("content-type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusCreated, rr.Result().StatusCode)
	}
	get := func() map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/token/012345678", nil)
		req.Header.Set("accept", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Result().StatusCode)
		var got map[string]any
		require.NoError(t, json.NewDecoder(rr.Result().Body).Decode(&got))
		return got
	}

	post(`{
		"cacheMode": "ALWAYS",
		"contractId": "GB-TWK-012345678-V",
		"countryCode": "GB",
		"issuer": "Thoughtworks",
		"partyId": "TWK",
		"type": "RFID",
		"uid": "012345678",
		"valid": true,
		"expiresAt": "2027-01-31T09:30:00+01:00"
	}`)

	expiresAt := time.Date(2027, 1, 31, 8, 30, 0, 0, time.UTC)
	stored, err := engine.LookupToken(context.Background(), "012345678")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.ExpiresAt)
	assert.True(t, expiresAt.Equal(*stored.ExpiresAt), "stored expiry time %s, want %s", stored.ExpiresAt, expiresAt)

	got := get()
	require.Contains(t, got, "expiresAt")
	gotExpiresAt, err := time.Parse(time.RFC3339, got["expiresAt"].(string))
	require.NoError(t, err)
	assert.True(t, expiresAt.Equal(gotExpiresAt), "expiresAt %s, want %s", gotExpiresAt, expiresAt)

	// setting the token without expiresAt removes the expiry time
	post(`{
		"cacheMode": "ALWAYS",
		"contractId": "GB-TWK-012345678-V",
		"countryCode": "GB",
		"issuer": "Thoughtworks",
		"partyId": "TWK",
		"type": "RFID",
		"uid": "012345678",
		"valid": true
	}`)

	stored, err = engine.LookupToken(context.Background(), "012345678")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Nil(t, stored.ExpiresAt)
	assert.NotContains(t, get(), "expiresAt")
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

func TestGetLocalListVersion(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	// Set up some data
	err := engine.UpdateLocalAuthList(context.Background(), "cs001", 5, store.LocalAuthListUpdateTypeFull, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/local-list/version", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.LocalListVersionResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Equal(t, int32(5), got.ListVersion)
}

func TestGetLocalListVersionForNewChargeStation(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/cs/new-cs/local-list/version", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.LocalListVersionResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Equal(t, int32(0), got.ListVersion)
}

func TestGetLocalAuthorizationList(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	// Set up test data
	expiryDate := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	parentIdTag := "PARENT001"
	entries := []*store.LocalAuthListEntry{
		{
			IdTag: "USER001",
			IdTagInfo: &store.IdTagInfo{
				Status:      store.IdTagStatusAccepted,
				ExpiryDate:  &expiryDate,
				ParentIdTag: &parentIdTag,
			},
		},
		{
			IdTag: "USER002",
			IdTagInfo: &store.IdTagInfo{
				Status: store.IdTagStatusBlocked,
			},
		},
	}

	err := engine.UpdateLocalAuthList(context.Background(), "cs001", 3, store.LocalAuthListUpdateTypeFull, entries)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/local-list", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.LocalAuthorizationListResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Equal(t, int32(3), got.ListVersion)
	assert.Len(t, got.LocalAuthorizationList, 2)
	assert.Equal(t, "USER001", got.LocalAuthorizationList[0].IdTag)
	assert.Equal(t, api.IdTagInfoStatusAccepted, got.LocalAuthorizationList[0].IdTagInfo.Status)
	assert.NotNil(t, got.LocalAuthorizationList[0].IdTagInfo.ExpiryDate)
	assert.NotNil(t, got.LocalAuthorizationList[0].IdTagInfo.ParentIdTag)
	assert.Equal(t, parentIdTag, *got.LocalAuthorizationList[0].IdTagInfo.ParentIdTag)

	assert.Equal(t, "USER002", got.LocalAuthorizationList[1].IdTag)
	assert.Equal(t, api.IdTagInfoStatusBlocked, got.LocalAuthorizationList[1].IdTagInfo.Status)
}

func TestUpdateLocalAuthorizationListFull(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	expiryDate := time.Now().Add(48 * time.Hour)
	parentIdTag := "PARENT002"
	reqBody := api.UpdateLocalListRequest{
		ListVersion: 10,
		UpdateType:  api.Full,
		LocalAuthorizationList: &[]api.LocalAuthorizationEntry{
			{
				IdTag: "USER003",
				IdTagInfo: api.IdTagInfo{
					Status:      api.IdTagInfoStatusAccepted,
					ExpiryDate:  &expiryDate,
					ParentIdTag: &parentIdTag,
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001/local-list", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)

	// Verify the update was stored
	version, err := engine.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 10, version)

	entries, err := engine.GetLocalAuthList(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "USER003", entries[0].IdTag)
}

func TestUpdateLocalAuthorizationListDifferential(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	// Set up initial list
	initialEntries := []*store.LocalAuthListEntry{
		{
			IdTag: "USER001",
			IdTagInfo: &store.IdTagInfo{
				Status: store.IdTagStatusAccepted,
			},
		},
	}
	err := engine.UpdateLocalAuthList(context.Background(), "cs001", 5, store.LocalAuthListUpdateTypeFull, initialEntries)
	require.NoError(t, err)

	// Add new entry via differential update
	reqBody := api.UpdateLocalListRequest{
		ListVersion: 6,
		UpdateType:  api.Differential,
		LocalAuthorizationList: &[]api.LocalAuthorizationEntry{
			{
				IdTag: "USER002",
				IdTagInfo: api.IdTagInfo{
					Status: api.IdTagInfoStatusBlocked,
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001/local-list", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)

	// Verify the update
	version, err := engine.GetLocalListVersion(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Equal(t, 6, version)

	entries, err := engine.GetLocalAuthList(context.Background(), "cs001")
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestUpdateLocalAuthorizationListInvalidUpdateType(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	reqBody := map[string]interface{}{
		"listVersion": 1,
		"updateType":  "Invalid",
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/cs/cs001/local-list", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Result().StatusCode)
}

func TestGetChargeStationConfiguration(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	// Setup test data
	err := engine.UpdateChargeStationSettings(context.Background(), "cs001", &store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:  "300",
				Status: store.ChargeStationSettingStatusAccepted,
			},
			"MeterValueSampleInterval": {
				Value:  "60",
				Status: store.ChargeStationSettingStatusAccepted,
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/configuration", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.ConfigurationResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Len(t, got.ConfigurationKey, 2)
	// Check that both keys are present (order may vary)
	keys := make(map[string]string)
	for _, kv := range got.ConfigurationKey {
		if kv.Value != nil {
			keys[kv.Key] = *kv.Value
		}
	}
	assert.Equal(t, "300", keys["HeartbeatInterval"])
	assert.Equal(t, "60", keys["MeterValueSampleInterval"])
}

func TestGetChargeStationConfigurationWithFilter(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	// Setup test data
	err := engine.UpdateChargeStationSettings(context.Background(), "cs001", &store.ChargeStationSettings{
		ChargeStationId: "cs001",
		Settings: map[string]*store.ChargeStationSetting{
			"HeartbeatInterval": {
				Value:  "300",
				Status: store.ChargeStationSettingStatusAccepted,
			},
			"MeterValueSampleInterval": {
				Value:  "60",
				Status: store.ChargeStationSettingStatusAccepted,
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/configuration?key=HeartbeatInterval", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.ConfigurationResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Len(t, got.ConfigurationKey, 1)
	assert.Equal(t, "HeartbeatInterval", got.ConfigurationKey[0].Key)
	assert.NotNil(t, got.ConfigurationKey[0].Value)
	assert.Equal(t, "300", *got.ConfigurationKey[0].Value)
}

func TestChangeChargeStationConfiguration(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()

	reqBody := `{"HeartbeatInterval": "600", "MeterValueSampleInterval": "120"}`
	req := httptest.NewRequest(http.MethodPatch, "/cs/cs001/configuration", strings.NewReader(reqBody))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.ConfigurationChangeResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Len(t, got.Results, 2)

	// Verify settings were stored
	settings, err := engine.LookupChargeStationSettings(context.Background(), "cs001")
	require.NoError(t, err)
	assert.NotNil(t, settings)
	assert.Equal(t, "600", settings.Settings["HeartbeatInterval"].Value)
	assert.Equal(t, store.ChargeStationSettingStatusPending, settings.Settings["HeartbeatInterval"].Status)
	assert.Equal(t, "120", settings.Settings["MeterValueSampleInterval"].Value)
	assert.Equal(t, store.ChargeStationSettingStatusPending, settings.Settings["MeterValueSampleInterval"].Status)
}

func TestGetChargeStationVariables(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	req := httptest.NewRequest(http.MethodGet, "/cs/cs001/variables", nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.VariablesResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	// Should return empty array for now (not implemented)
	assert.Empty(t, got.Variables)
}

func TestSetChargeStationVariables(t *testing.T) {
	server, r, _, _ := setupServer(t)
	defer server.Close()

	reqBody := `{"variables": [{"component": {"name": "OCPPCommCtrlr"}, "variable": {"name": "HeartbeatInterval"}, "attributeValue": "600"}]}`
	req := httptest.NewRequest(http.MethodPatch, "/cs/cs001/variables", strings.NewReader(reqBody))
	req.Header.Set("content-type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Result().StatusCode)
	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)

	var got api.VariablesChangeResponse
	err = json.Unmarshal(b, &got)
	require.NoError(t, err)

	assert.Len(t, got.Results, 1)
	assert.Equal(t, "Accepted", string(got.Results[0].AttributeStatus))
	assert.Equal(t, "OCPPCommCtrlr", got.Results[0].Component.Name)
	assert.Equal(t, "HeartbeatInterval", got.Results[0].Variable.Name)
}

// getJson sends a GET request and decodes the body of the 200 response into got.
func getJson(t *testing.T, r *chi.Mux, url string, got any) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("accept", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	b, err := io.ReadAll(rr.Result().Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rr.Result().StatusCode, string(b))
	require.NoError(t, json.Unmarshal(b, got), string(b))
}

func transactionSample(sampleContext, measurand string, value float64) store.SampledValue {
	location := "Outlet"
	return store.SampledValue{
		Context:       &sampleContext,
		Location:      &location,
		Measurand:     &measurand,
		UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh"},
		Value:         value,
	}
}

// TestTransactionsReturnOcpp16Transaction checks that what the OCPP 1.6 StartTransaction
// and StopTransaction handlers store is returned by the list and by the detail.
func TestTransactionsReturnOcpp16Transaction(t *testing.T) {
	server, r, engine, c := setupServer(t)
	defer server.Close()
	ctx := context.Background()

	require.NoError(t, engine.SetToken(ctx, &store.Token{
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "MYRFIDTAG",
		ContractId:  "GBTWK012345678V",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "NEVER",
	}))

	startHandler := ocpp16.StartTransactionHandler{
		Clock:            c,
		TokenStore:       engine,
		TransactionStore: engine,
		ReservationStore: engine,
	}
	startResponse, err := startHandler.HandleCall(ctx, "cs001", &types.StartTransactionJson{
		ConnectorId: 2,
		IdTag:       "MYRFIDTAG",
		MeterStart:  1000,
		Timestamp:   "2026-11-26T10:00:00Z",
	})
	require.NoError(t, err)
	transactionId := startResponse.(*types.StartTransactionResponseJson).TransactionId
	wantTransactionId := ocpp16.ConvertToUUID(transactionId)

	startTime := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	stopTime := time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC)
	connectorId := 2
	tokenType := "ISO14443"
	outlet := "Outlet"
	energyRegister := "Energy.Active.Import.Register"
	wattHours := "Wh"
	transactionBegin := "Transaction.Begin"
	transactionEnd := "Transaction.End"
	beginMeterValue := api.MeterValue{
		Timestamp: startTime,
		SampledValue: []api.MeterValuesSampledValue{
			{Value: "1000", Context: &transactionBegin, Location: &outlet, Measurand: &energyRegister, Unit: &wattHours},
		},
	}

	// while the transaction is active
	gotList := new(api.TransactionList)
	getJson(t, r, "/cs/cs001/transactions", gotList)
	wantList := &api.TransactionList{
		Total:  1,
		Limit:  50,
		Offset: 0,
		Transactions: []api.TransactionSummary{
			{
				TransactionId: wantTransactionId,
				IdTag:         "MYRFIDTAG",
				StartTime:     startTime,
				MeterStart:    1000,
				Status:        api.TransactionSummaryStatusActive,
				ConnectorId:   &connectorId,
				EvseId:        &connectorId,
			},
		},
	}
	assert.Equal(t, wantList, gotList)

	gotDetail := new(api.TransactionDetail)
	getJson(t, r, "/cs/cs001/transaction/"+wantTransactionId, gotDetail)
	wantDetail := &api.TransactionDetail{
		TransactionId: wantTransactionId,
		IdTag:         "MYRFIDTAG",
		TokenType:     &tokenType,
		StartTime:     startTime,
		MeterStart:    1000,
		Status:        api.TransactionDetailStatusActive,
		ConnectorId:   &connectorId,
		EvseId:        &connectorId,
		MeterValues:   []api.MeterValue{beginMeterValue},
	}
	assert.Equal(t, wantDetail, gotDetail)

	// after the stop
	stopHandler := ocpp16.StopTransactionHandler{
		Clock:            c,
		TokenStore:       engine,
		TransactionStore: engine,
	}
	reason := types.StopTransactionJsonReasonEVDisconnected
	_, err = stopHandler.HandleCall(ctx, "cs001", &types.StopTransactionJson{
		TransactionId: transactionId,
		MeterStop:     2500,
		Reason:        &reason,
		Timestamp:     "2026-11-26T10:30:00Z",
	})
	require.NoError(t, err)

	meterStop := 2500
	energy := int64(1500)
	stopReason := "EVDisconnected"

	gotList = new(api.TransactionList)
	getJson(t, r, "/cs/cs001/transactions", gotList)
	wantList.Transactions[0].Status = api.TransactionSummaryStatusCompleted
	wantList.Transactions[0].StopTime = &stopTime
	wantList.Transactions[0].MeterStop = &meterStop
	wantList.Transactions[0].StopReason = &stopReason
	wantList.Transactions[0].EnergyWh = &energy
	assert.Equal(t, wantList, gotList)

	gotDetail = new(api.TransactionDetail)
	getJson(t, r, "/cs/cs001/transaction/"+wantTransactionId, gotDetail)
	wantDetail.Status = api.TransactionDetailStatusCompleted
	wantDetail.StopTime = &stopTime
	wantDetail.MeterStop = &meterStop
	wantDetail.StopReason = &stopReason
	wantDetail.EnergyWh = &energy
	wantDetail.MeterValues = []api.MeterValue{
		beginMeterValue,
		{
			Timestamp: stopTime,
			SampledValue: []api.MeterValuesSampledValue{
				{Value: "2500", Context: &transactionEnd, Location: &outlet, Measurand: &energyRegister, Unit: &wattHours},
			},
		},
	}
	assert.Equal(t, wantDetail, gotDetail)
}

// TestTransactionsPreferStoredDetailsOverSamples checks that the times and the meter
// readings that are stored with the transaction win over the ones of its samples.
func TestTransactionsPreferStoredDetailsOverSamples(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	ctx := context.Background()

	err := engine.CreateTransaction(ctx, "cs001", "tx001", "MYRFIDTAG", "ISO14443", []store.MeterValue{
		{
			Timestamp:     "2026-11-26T10:00:07Z",
			SampledValues: []store.SampledValue{transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 1010)},
		},
	}, 0, false)
	require.NoError(t, err)
	err = engine.EndTransaction(ctx, "cs001", "tx001", "MYRFIDTAG", "ISO14443", []store.MeterValue{
		{
			Timestamp:     "2026-11-26T10:29:55Z",
			SampledValues: []store.SampledValue{transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 2490)},
		},
	}, 1)
	require.NoError(t, err)

	startTime := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	stopTime := time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC)
	connectorId := 1
	evseId := 3
	meterStartWh := int64(1000)
	meterStopWh := int64(2500)
	energy := int64(1500)
	stopReason := "Remote"
	err = engine.UpdateTransactionDetails(ctx, "cs001", "tx001", &store.TransactionDetails{
		ConnectorId:  &connectorId,
		EvseId:       &evseId,
		StartedAt:    &startTime,
		StoppedAt:    &stopTime,
		MeterStartWh: &meterStartWh,
		MeterStopWh:  &meterStopWh,
		EnergyWh:     &energy,
		StopReason:   &stopReason,
	})
	require.NoError(t, err)

	meterStop := 2500
	want := api.TransactionSummary{
		TransactionId: "tx001",
		IdTag:         "MYRFIDTAG",
		StartTime:     startTime,
		StopTime:      &stopTime,
		MeterStart:    1000,
		MeterStop:     &meterStop,
		Status:        api.TransactionSummaryStatusCompleted,
		ConnectorId:   &connectorId,
		EvseId:        &evseId,
		StopReason:    &stopReason,
		EnergyWh:      &energy,
	}

	gotList := new(api.TransactionList)
	getJson(t, r, "/cs/cs001/transactions", gotList)
	assert.Equal(t, []api.TransactionSummary{want}, gotList.Transactions)

	gotDetail := new(api.TransactionDetail)
	getJson(t, r, "/cs/cs001/transaction/tx001", gotDetail)
	assert.Equal(t, want.StartTime, gotDetail.StartTime)
	assert.Equal(t, want.StopTime, gotDetail.StopTime)
	assert.Equal(t, want.MeterStart, gotDetail.MeterStart)
	assert.Equal(t, want.MeterStop, gotDetail.MeterStop)
	assert.Equal(t, want.ConnectorId, gotDetail.ConnectorId)
	assert.Equal(t, want.EvseId, gotDetail.EvseId)
	assert.Equal(t, want.StopReason, gotDetail.StopReason)
	assert.Equal(t, want.EnergyWh, gotDetail.EnergyWh)
	assert.Len(t, gotDetail.MeterValues, 2)
}

// TestTransactionsFallBackToSamples covers transactions that have no stored details
// (written before the details existed, or by a store that does not keep them): the
// times and the meter readings are derived from the samples.
func TestTransactionsFallBackToSamples(t *testing.T) {
	firstTime := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	lastTime := time.Date(2026, 11, 26, 10, 30, 0, 0, time.UTC)

	// the energy register is not in the first and not in the last meter value
	meterValues := []store.MeterValue{
		{
			Timestamp:     "2026-11-26T10:00:00Z",
			SampledValues: []store.SampledValue{transactionSample("Transaction.Begin", "MeterValue", 990)},
		},
		{
			Timestamp: "2026-11-26T10:10:00Z",
			SampledValues: []store.SampledValue{
				transactionSample("Sample.Periodic", "Power.Active.Import", 7400),
				transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 1200),
				transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 1201),
			},
		},
		{
			Timestamp: "2026-11-26T10:20:00Z",
			SampledValues: []store.SampledValue{
				transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 2399),
				transactionSample("Sample.Periodic", "Energy.Active.Import.Register", 2400),
				transactionSample("Sample.Periodic", "Power.Active.Import", 7300),
			},
		},
		{
			Timestamp:     "2026-11-26T10:30:00Z",
			SampledValues: []store.SampledValue{transactionSample("Transaction.End", "Power.Active.Import", 0)},
		},
	}
	noRegister := []store.MeterValue{meterValues[0], meterValues[3]}

	tests := []struct {
		name           string
		meterValues    []store.MeterValue
		ended          bool
		wantStart      time.Time
		wantStop       *time.Time
		wantMeterStart int
		wantMeterStop  *int
	}{
		{
			name:           "active transaction has no stop time and no meter stop",
			meterValues:    meterValues,
			ended:          false,
			wantStart:      firstTime,
			wantMeterStart: 1200,
		},
		{
			name:           "completed transaction",
			meterValues:    meterValues,
			ended:          true,
			wantStart:      firstTime,
			wantStop:       &lastTime,
			wantMeterStart: 1200,
			wantMeterStop:  intRef(2400),
		},
		{
			name:           "completed transaction without energy register samples",
			meterValues:    noRegister,
			ended:          true,
			wantStart:      firstTime,
			wantStop:       &lastTime,
			wantMeterStart: 0,
		},
		{
			name:      "active transaction without meter values",
			ended:     false,
			wantStart: time.Time{},
		},
		{
			name:      "completed transaction without meter values",
			ended:     true,
			wantStart: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, r, engine, _ := setupServer(t)
			defer server.Close()
			ctx := context.Background()

			require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx001", "MYRFIDTAG", "ISO14443", tt.meterValues, 0, false))
			wantStatus := api.TransactionSummaryStatusActive
			if tt.ended {
				require.NoError(t, engine.EndTransaction(ctx, "cs001", "tx001", "MYRFIDTAG", "ISO14443", nil, 1))
				wantStatus = api.TransactionSummaryStatusCompleted
			}

			gotList := new(api.TransactionList)
			getJson(t, r, "/cs/cs001/transactions", gotList)
			want := api.TransactionSummary{
				TransactionId: "tx001",
				IdTag:         "MYRFIDTAG",
				StartTime:     tt.wantStart,
				StopTime:      tt.wantStop,
				MeterStart:    tt.wantMeterStart,
				MeterStop:     tt.wantMeterStop,
				Status:        wantStatus,
			}
			assert.Equal(t, []api.TransactionSummary{want}, gotList.Transactions)

			gotDetail := new(api.TransactionDetail)
			getJson(t, r, "/cs/cs001/transaction/tx001", gotDetail)
			assert.Equal(t, tt.wantStart, gotDetail.StartTime)
			assert.Equal(t, tt.wantStop, gotDetail.StopTime)
			assert.Equal(t, tt.wantMeterStart, gotDetail.MeterStart)
			assert.Equal(t, tt.wantMeterStop, gotDetail.MeterStop)
			assert.Equal(t, api.TransactionDetailStatus(wantStatus), gotDetail.Status)
			assert.Len(t, gotDetail.MeterValues, len(tt.meterValues))
			assert.Nil(t, gotDetail.ConnectorId)
			assert.Nil(t, gotDetail.EvseId)
			assert.Nil(t, gotDetail.StopReason)
			assert.Nil(t, gotDetail.EnergyWh)
		})
	}
}

func intRef(v int) *int {
	return &v
}

// listedReservationsStore lists its reservations whatever their status. The stores only
// list the reservations that are still active, and the API has no other way to read one.
type listedReservationsStore struct {
	store.Engine
	reservations []*store.Reservation
}

func (s listedReservationsStore) GetActiveReservations(context.Context, string) ([]*store.Reservation, error) {
	return s.reservations, nil
}

func TestListReservationsRendersEveryReservationStatus(t *testing.T) {
	statuses := []store.ReservationStatus{
		store.ReservationStatusAccepted,
		store.ReservationStatusFaulted,
		store.ReservationStatusOccupied,
		store.ReservationStatusRejected,
		store.ReservationStatusUnavailable,
		store.ReservationStatusCancelled,
		store.ReservationStatusExpired,
		store.ReservationStatusUsed,
	}

	now := time.Now().UTC()
	c := clockTest.NewFakePassiveClock(now)
	engine := listedReservationsStore{Engine: inmemory.NewStore(c)}
	for i, status := range statuses {
		engine.reservations = append(engine.reservations, &store.Reservation{
			ReservationId:   i + 1,
			ChargeStationId: "cs001",
			ConnectorId:     1,
			IdTag:           "USER001",
			ExpiryDate:      now.Add(time.Hour),
			Status:          status,
			CreatedAt:       now,
		})
	}

	srv, err := api.NewServer(engine, c, ocpi.NewOCPI(engine, nil, "GB", "TWK"))
	require.NoError(t, err)
	r := chi.NewRouter()
	r.Use(api.ValidationMiddleware)
	r.Mount("/", api.Handler(srv))

	response := new(api.ReservationList)
	getJson(t, r, "/cs/cs001/reservations", response)

	got := make([]store.ReservationStatus, 0, len(response.Reservations))
	for _, reservation := range response.Reservations {
		got = append(got, store.ReservationStatus(reservation.Status))
	}
	assert.Equal(t, statuses, got)
	assert.Equal(t, api.ReservationResponseStatusUsed, response.Reservations[len(statuses)-1].Status)

	// every status that is rendered is a value of the schema of the response
	swagger, err := api.GetSwagger()
	require.NoError(t, err)
	statusSchema := swagger.Components.Schemas["ReservationResponse"].Value.Properties["status"].Value
	for _, status := range statuses {
		assert.Containsf(t, statusSchema.Enum, string(status), "reservation status %s is not in the API schema", status)
	}
}
