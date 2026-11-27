// SPDX-License-Identifier: Apache-2.0

package adminui

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"html"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/thoughtworks/maeve-csms/manager/services"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

// exampleOrgName gives every charge station the organisation name "Example".
func exampleOrgName(context.Context, string) string {
	return "Example"
}

func TestConnectWithUnsecuredAuth(t *testing.T) {
	caCert, caKey := generateCA(t)

	engine := inmemory.NewStore(clock.RealClock{})
	certificateProvider := &services.LocalChargeStationCertificateProvider{
		Store:             engine,
		CertificateReader: services.StringSource{Data: caCert},
		PrivateKeyReader:  services.StringSource{Data: caKey},
	}
	server := NewServer("localhost", 80, 443, exampleOrgName, engine, certificateProvider, store.LegacyOrgId)

	r := chi.NewRouter()
	r.Mount("/adminui", server)

	form := url.Values{}
	form.Set("csid", "cs001")
	form.Set("auth", "unsecured")

	req := httptest.NewRequest(http.MethodPost, "/adminui/connect", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("request failed with status: %d", rr.Code)
	}

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unable to read response body: %v", err)
	}
	t.Log(string(data))

	auth, err := engine.LookupChargeStationAuth(context.TODO(), "cs001")
	if err != nil {
		t.Fatalf("unable to lookup charge station: %v", err)
	}
	if auth.SecurityProfile != 0 {
		t.Fatalf("expected security profile to be 0 but was %d", auth.SecurityProfile)
	}
	if auth.Base64SHA256Password == "" {
		t.Fatalf("expected base64 sha password")
	}
}

func TestConnectWithBasicAuth(t *testing.T) {
	caCert, caKey := generateCA(t)

	engine := inmemory.NewStore(clock.RealClock{})
	certificateProvider := &services.LocalChargeStationCertificateProvider{
		Store:             engine,
		CertificateReader: services.StringSource{Data: caCert},
		PrivateKeyReader:  services.StringSource{Data: caKey},
	}
	server := NewServer("localhost", 80, 443, exampleOrgName, engine, certificateProvider, store.LegacyOrgId)

	r := chi.NewRouter()
	r.Mount("/adminui", server)

	form := url.Values{}
	form.Set("csid", "cs001")
	form.Set("auth", "basic")

	req := httptest.NewRequest(http.MethodPost, "/adminui/connect", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("request failed with status: %d", rr.Code)
	}

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unable to read response body: %v", err)
	}
	t.Log(string(data))

	auth, err := engine.LookupChargeStationAuth(context.TODO(), "cs001")
	if err != nil {
		t.Fatalf("unable to lookup charge station: %v", err)
	}
	if auth.SecurityProfile != 1 {
		t.Fatalf("expected security profile to be 1 but was %d", auth.SecurityProfile)
	}
	if auth.Base64SHA256Password == "" {
		t.Fatalf("expected base64 sha password")
	}
}

func TestConnectWithMTLS(t *testing.T) {
	caCert, caKey := generateCA(t)

	engine := inmemory.NewStore(clock.RealClock{})
	certificateProvider := &services.LocalChargeStationCertificateProvider{
		Store:             engine,
		CertificateReader: services.StringSource{Data: caCert},
		PrivateKeyReader:  services.StringSource{Data: caKey},
	}
	// the organisation of the certificate is the one of the charge station
	orgNameFor := func(_ context.Context, csId string) string {
		return "Org of " + csId
	}
	server := NewServer("localhost", 80, 443, orgNameFor, engine, certificateProvider, store.LegacyOrgId)

	r := chi.NewRouter()
	r.Mount("/adminui", server)

	form := url.Values{}
	form.Set("csid", "cs001")
	form.Set("auth", "mtls")

	req := httptest.NewRequest(http.MethodPost, "/adminui/connect", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("request failed with status: %d", rr.Code)
	}

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unable to read response body: %v", err)
	}
	t.Log(string(data))

	// the first certificate on the page is the one of the charge station
	page := []byte(html.UnescapeString(string(data)))
	block, _ := pem.Decode(page[bytes.Index(page, []byte("-----BEGIN CERTIFICATE-----")):])
	if block == nil {
		t.Fatalf("no certificate on the page")
	}
	clientCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("unable to parse the client certificate: %v", err)
	}
	if clientCert.Subject.CommonName != "cs001" || len(clientCert.Subject.Organization) != 1 || clientCert.Subject.Organization[0] != "Org of cs001" {
		t.Fatalf("unexpected subject of the client certificate: %s", clientCert.Subject)
	}

	auth, err := engine.LookupChargeStationAuth(context.TODO(), "cs001")
	if err != nil {
		t.Fatalf("unable to lookup charge station: %v", err)
	}
	if auth.SecurityProfile != 2 {
		t.Fatalf("expected security profile to be 2 but was %d", auth.SecurityProfile)
	}
	if auth.Base64SHA256Password != "" {
		t.Fatalf("expected base64 sha password to not be set")
	}
}

func TestRegisterTokenWithShortUid(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})

	server := NewServer("localhost", 80, 443, exampleOrgName, engine, nil, store.LegacyOrgId)

	r := chi.NewRouter()
	r.Mount("/adminui", server)

	uid := "ABC1"

	form := url.Values{}
	form.Set("uid", uid)

	req := httptest.NewRequest(http.MethodPost, "/adminui/token", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("request failed with status: %d", rr.Code)
	}

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unable to read response body: %v", err)
	}
	t.Log(string(data))

	tok, err := engine.LookupToken(context.TODO(), uid)
	if err != nil {
		t.Fatalf("unable to read token from db: %v", err)
	}

	if tok.Uid != uid {
		t.Errorf("wrong uid, want %s got %s", uid, tok.Uid)
	}
	if tok.ContractId != "GBTWK00000ABC1R" {
		t.Errorf("wrong contract id, want GBTWK00000ABC1R got %s", tok.ContractId)
	}
}

func TestRegisterTokenWithLongUid(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})

	server := NewServer("localhost", 80, 443, exampleOrgName, engine, nil, store.LegacyOrgId)

	r := chi.NewRouter()
	r.Mount("/adminui", server)

	uid := "ABC1234567890"

	form := url.Values{}
	form.Set("uid", uid)

	req := httptest.NewRequest(http.MethodPost, "/adminui/token", strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("request failed with status: %d", rr.Code)
	}

	data, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("unable to read response body: %v", err)
	}
	t.Log(string(data))

	tok, err := engine.LookupToken(context.TODO(), uid)
	if err != nil {
		t.Fatalf("unable to read token from db: %v", err)
	}

	if tok.Uid != uid {
		t.Errorf("wrong uid, want %s got %s", uid, tok.Uid)
	}
	if tok.ContractId != "GBTWKABC123456Y" {
		t.Errorf("wrong contract id, want GBTWKABC123456Y got %s", tok.ContractId)
	}
}

func generateCA(t *testing.T) (string, string) {
	caKeyPair, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate root ca keypair: %v", err)
	}

	caSerialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		t.Fatalf("generating root CA serial number: %v", err)
	}

	caCertTemplate := x509.Certificate{
		SerialNumber: caSerialNumber,
		Subject: pkix.Name{
			CommonName:   "ca",
			Organization: []string{"Thoughtworks"},
		},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Minute),
	}

	caCertBytes, err :=
		x509.CreateCertificate(rand.Reader, &caCertTemplate, &caCertTemplate, &caKeyPair.PublicKey, caKeyPair)
	if err != nil {
		t.Fatalf("creating root CA certificate: %v", err)
	}

	pemCert := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caCertBytes,
	})

	pemKey := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caKeyPair),
	})

	return string(pemCert), string(pemKey)
}

// The charge stations and tokens that are registered on the admin pages belong to the
// default org of the server.
func TestRegistrationsBelongToTheDefaultOrg(t *testing.T) {
	const defaultOrgId = "0193b2c4-0000-7000-8000-0000000000a1"

	for name, tt := range map[string]struct {
		defaultOrgId string
		wantOrgId    string
	}{
		"default org":    {defaultOrgId: defaultOrgId, wantOrgId: defaultOrgId},
		"no default org": {defaultOrgId: "", wantOrgId: store.LegacyOrgId},
	} {
		t.Run(name, func(t *testing.T) {
			engine := inmemory.NewStore(clock.RealClock{})
			r := chi.NewRouter()
			r.Mount("/adminui", NewServer("localhost", 80, 443, exampleOrgName, engine, nil, tt.defaultOrgId))

			post := func(path string, form url.Values) {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
				req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Fatalf("request to %s failed with status: %d", path, rr.Code)
				}
			}
			post("/adminui/connect", url.Values{"csid": {"cs001"}, "auth": {"unsecured"}})
			post("/adminui/token", url.Values{"uid": {"ABC123"}})

			auth, err := engine.LookupChargeStationAuth(context.TODO(), "cs001")
			if err != nil || auth == nil {
				t.Fatalf("unable to lookup charge station: %v", err)
			}
			if auth.OrgId != tt.wantOrgId {
				t.Errorf("wrong org of the charge station, want %s got %s", tt.wantOrgId, auth.OrgId)
			}

			tok, err := engine.LookupTokenForOrg(context.TODO(), tt.wantOrgId, "ABC123")
			if err != nil || tok == nil {
				t.Fatalf("unable to read the token of the org from db: %v", err)
			}
			if tok.OrgId != tt.wantOrgId {
				t.Errorf("wrong org of the token, want %s got %s", tt.wantOrgId, tok.OrgId)
			}
		})
	}
}
