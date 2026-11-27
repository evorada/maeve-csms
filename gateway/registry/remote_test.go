package registry_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/gateway/registry"
	"go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const multiTenantStationAuth = `{"orgId":"0193b2c4-0000-7000-8000-000000000001","orgKey":"aaaa","stationKey":"aaaa.cs001",` +
	`"tenantStatus":"active","securityProfile":1,"base64SHA256Password":"DEADBEEF","invalidUsernameAllowed":true}`

// managerStub serves the same body with the same status for every request and records
// the last request.
type managerStub struct {
	*httptest.Server
	mu         sync.Mutex
	path       string
	query      url.Values
	requestURI string
}

func newManagerStub(t *testing.T, status int, body string) *managerStub {
	stub := &managerStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.path = r.URL.Path
		stub.query = r.URL.Query()
		stub.requestURI = r.RequestURI
		stub.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (m *managerStub) lastRequest() (path string, query url.Values, requestURI string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.path, m.query, m.requestURI
}

func TestLookupChargeStation(t *testing.T) {
	stub := newManagerStub(t, http.StatusOK, multiTenantStationAuth)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL,
	}

	want := &registry.ChargeStation{
		ClientId:               "cs001",
		StationKey:             "aaaa.cs001",
		OrgId:                  "0193b2c4-0000-7000-8000-000000000001",
		OrgKey:                 "aaaa",
		SecurityProfile:        registry.TLSWithBasicAuth,
		Base64SHA256Password:   "DEADBEEF",
		InvalidUsernameAllowed: true,
	}

	got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	path, query, _ := stub.lastRequest()
	assert.Equal(t, "/_internal/v1/station-auth", path)
	assert.Equal(t, url.Values{"host": {"a.example"}, "stationId": {"cs001"}}, query)
}

func TestLookupChargeStationInSingleTenantMode(t *testing.T) {
	stub := newManagerStub(t, http.StatusOK,
		`{"orgId":"00000000-0000-0000-0000-000000000000","stationKey":"cs001","securityProfile":0,"base64SHA256Password":"DEADBEEF","invalidUsernameAllowed":false}`)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL + "/",
	}

	want := &registry.ChargeStation{
		ClientId:             "cs001",
		StationKey:           "cs001",
		OrgId:                "00000000-0000-0000-0000-000000000000",
		SecurityProfile:      registry.UnsecuredTransportWithBasicAuth,
		Base64SHA256Password: "DEADBEEF",
	}

	got, err := reg.LookupChargeStation(context.Background(), "localhost", "cs001")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	path, _, _ := stub.lastRequest()
	assert.Equal(t, "/_internal/v1/station-auth", path)
}

// The organisation names of the tenant are only taken from an answer that has them: an
// answer without the list, or with null, leaves the check to the list of the gateway.
func TestLookupChargeStationReadsTheCertOrgNamesOfTheTenant(t *testing.T) {
	const prefix = `{"orgId":"0193b2c4-0000-7000-8000-000000000001","orgKey":"aaaa","stationKey":"aaaa.cs001","securityProfile":2,"invalidUsernameAllowed":false`

	for name, tc := range map[string]struct {
		body      string
		wantList  bool
		wantNames []string
	}{
		"names":          {body: prefix + `,"acceptedCertOrgNames":["Charge Co","Other"]}`, wantList: true, wantNames: []string{"Charge Co", "Other"}},
		"an empty list":  {body: prefix + `,"acceptedCertOrgNames":[]}`, wantList: true, wantNames: []string{}},
		"no list":        {body: prefix + `}`},
		"null":           {body: prefix + `,"acceptedCertOrgNames":null}`},
		"in single mode": {body: `{"orgId":"0193b2c4-0000-7000-8000-000000000001","stationKey":"cs001","securityProfile":2,"invalidUsernameAllowed":false}`},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newManagerStub(t, http.StatusOK, tc.body)
			reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}

			got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tc.wantList, got.HasTenantOrgNames)
			assert.Equal(t, tc.wantNames, got.AcceptedCertOrgNames)
		})
	}

	// a list that cannot be read is not taken for an empty one
	for name, member := range map[string]string{
		"a text":           `"Charge Co"`,
		"a name of a kind": `["Charge Co",7]`,
		"an object":        `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			stub := newManagerStub(t, http.StatusOK, prefix+`,"acceptedCertOrgNames":`+member+`}`)
			reg := registry.RemoteRegistry{ManagerApiAddr: stub.URL}

			got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
			assert.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestLookupChargeStationEscapesTheQueryParameters(t *testing.T) {
	stub := newManagerStub(t, http.StatusNotFound, `{"status":"Not Found","error":"unknown_station"}`)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL,
	}

	host := "a.example:443&stationId=other"
	clientId := "cs 001&host=b.example#x?y=z%41"

	_, err := reg.LookupChargeStation(context.Background(), host, clientId)
	require.NoError(t, err)

	path, query, requestURI := stub.lastRequest()
	assert.Equal(t, "/_internal/v1/station-auth", path)
	assert.Equal(t, url.Values{"host": {host}, "stationId": {clientId}}, query)
	assert.Equal(t, 1, strings.Count(requestURI, "&"), "request uri: %s", requestURI)
}

func TestLookupChargeStationNotFound(t *testing.T) {
	for _, reason := range []string{"unknown_host", "unknown_station"} {
		t.Run(reason, func(t *testing.T) {
			stub := newManagerStub(t, http.StatusNotFound, fmt.Sprintf(`{"status":"Not Found","error":"%s"}`, reason))

			reg := registry.RemoteRegistry{
				ManagerApiAddr: stub.URL,
			}

			exporter := tracetest.NewInMemoryExporter()
			tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(exporter))
			ctx, span := tracerProvider.Tracer("test").Start(context.Background(), "lookup")

			got, err := reg.LookupChargeStation(ctx, "a.example", "cs001")
			span.End()
			require.NoError(t, err)
			assert.Nil(t, got)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			assert.Equal(t, []attribute.KeyValue{attribute.String("cs.lookup.error", reason)}, spans[0].Attributes)
		})
	}
}

func TestLookupChargeStationRefusesANotFoundThatIsNotFromTheInternalApi(t *testing.T) {
	bodies := map[string]string{
		"plain text":     "404 page not found\n",
		"no reason":      `{"status":"Not Found"}`,
		"unknown reason": `{"status":"Not Found","error":"something_else"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			stub := newManagerStub(t, http.StatusNotFound, body)

			reg := registry.RemoteRegistry{
				ManagerApiAddr: stub.URL,
			}

			got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
			assert.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestLookupChargeStationFailsWhenTheManagerFails(t *testing.T) {
	responses := map[string]struct {
		status int
		body   string
	}{
		"internal server error": {http.StatusInternalServerError, `{"status":"Internal Server Error"}`},
		"bad request":           {http.StatusBadRequest, `{"status":"Bad Request","error":"stationId: station id contains '#'"}`},
		"no content":            {http.StatusNoContent, ""},
		// a success with the auth details in it, but not the one that is expected
		"accepted": {http.StatusAccepted, multiTenantStationAuth},
	}
	for name, response := range responses {
		t.Run(name, func(t *testing.T) {
			stub := newManagerStub(t, response.status, response.body)

			reg := registry.RemoteRegistry{
				ManagerApiAddr: stub.URL,
			}

			got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
			assert.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestLookupChargeStationRefusesAnAnswerThatDoesNotFitTheStation(t *testing.T) {
	bodies := map[string]string{
		"not json":                       "OK",
		"empty object":                   `{}`,
		"no station key":                 `{"orgKey":"aaaa","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"no security profile":            `{"orgKey":"aaaa","stationKey":"aaaa.cs001","base64SHA256Password":"DEADBEEF"}`,
		"key of another station":         `{"orgKey":"aaaa","stationKey":"aaaa.cs002","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"key of another tenant":          `{"orgKey":"aaaa","stationKey":"bbbb.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"plain key with an org key":      `{"orgKey":"aaaa","stationKey":"cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"namespaced key without org key": `{"stationKey":"aaaa.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"org key with a wildcard":        `{"orgKey":"aaa+","stationKey":"aaa+.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"org key with a topic separator": `{"orgKey":"aa/a","stationKey":"aa/a.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"org key in upper case":          `{"orgKey":"AAAA","stationKey":"AAAA.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"org key that is too short":      `{"orgKey":"aaa","stationKey":"aaa.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
		"org key that is too long":       `{"orgKey":"aaaaaaaaa","stationKey":"aaaaaaaaa.cs001","securityProfile":1,"base64SHA256Password":"DEADBEEF"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			stub := newManagerStub(t, http.StatusOK, body)

			reg := registry.RemoteRegistry{
				ManagerApiAddr: stub.URL,
			}

			got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
			require.Error(t, err)
			assert.Nil(t, got)
			assert.NotContains(t, err.Error(), "DEADBEEF")
		})
	}
}

func TestLookupChargeStationHonoursTheClientTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: server.URL,
		HTTPClient:     &http.Client{Timeout: 20 * time.Millisecond},
	}

	got, err := reg.LookupChargeStation(context.Background(), "a.example", "cs001")
	require.Error(t, err)
	assert.Nil(t, got)

	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout())
}

func TestLookupChargeStationHonoursTheContext(t *testing.T) {
	stub := newManagerStub(t, http.StatusOK, multiTenantStationAuth)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := reg.LookupChargeStation(ctx, "a.example", "cs001")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

func TestLookupCertificate(t *testing.T) {
	want := generateCertificate(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: want.Raw})
		blockWithNewlinesReplaced := strings.Replace(string(block), "\n", "\\n", -1)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"certificate":"%s"}`, blockWithNewlinesReplaced)))
	}))
	defer server.Close()

	reg := registry.RemoteRegistry{
		ManagerApiAddr: server.URL,
	}

	certHash := sha256.Sum256(want.Raw)
	b64CertHash := base64.StdEncoding.EncodeToString(certHash[:])

	got, err := reg.LookupCertificate(context.Background(), b64CertHash)
	require.NoError(t, err)

	assert.Equal(t, want.Raw, got.Raw)
}

func TestLookupCertificateWithSlashesAndPlusesInHash(t *testing.T) {
	var want *x509.Certificate
	var certHash [32]byte
	var b64CertHash string

	count := 0
	for {
		count++
		want = generateCertificate(t)
		certHash = sha256.Sum256(want.Raw)
		b64CertHash = base64.StdEncoding.EncodeToString(certHash[:])

		if strings.Contains(b64CertHash, "/") && strings.Contains(b64CertHash, "+") {
			break
		}
	}
	t.Logf("Generated %d certificates before finding one with slashes and pluses in the hash", count)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hashToMatch := strings.Replace(b64CertHash, "/", "_", -1)
		hashToMatch = strings.Replace(hashToMatch, "+", "-", -1)
		if r.URL.Path != fmt.Sprintf("/_internal/v1/certificates/%s", hashToMatch) {
			http.NotFound(w, r)
			return
		}
		block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: want.Raw})
		blockWithNewlinesReplaced := strings.Replace(string(block), "\n", "\\n", -1)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"certificate":"%s"}`, blockWithNewlinesReplaced)))
	}))
	defer server.Close()

	reg := registry.RemoteRegistry{
		ManagerApiAddr: server.URL,
	}

	got, err := reg.LookupCertificate(context.Background(), b64CertHash)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, want.Raw, got.Raw)
}

func TestLookupCertificateNotFound(t *testing.T) {
	stub := newManagerStub(t, http.StatusNotFound, `{"status":"Not Found"}`)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL,
	}

	got, err := reg.LookupCertificate(context.Background(), "a/b+c?d")
	require.NoError(t, err)
	assert.Nil(t, got)

	path, query, _ := stub.lastRequest()
	assert.Equal(t, "/_internal/v1/certificates/a_b-c?d", path)
	assert.Empty(t, query)
}

func TestLookupCertificateFailsWhenTheManagerFails(t *testing.T) {
	stub := newManagerStub(t, http.StatusInternalServerError, `{"status":"Internal Server Error"}`)

	reg := registry.RemoteRegistry{
		ManagerApiAddr: stub.URL,
	}

	got, err := reg.LookupCertificate(context.Background(), "hash")
	assert.Error(t, err)
	assert.Nil(t, got)
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
