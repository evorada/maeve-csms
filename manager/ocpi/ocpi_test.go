// SPDX-License-Identifier: Apache-2.0

package ocpi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"k8s.io/utils/clock"
)

// ocpiTestOrgId is the default org of the OCPI API under test.
const ocpiTestOrgId = "0193b2c4-0000-7000-8000-0000000000a1"

func TestGetVersions(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", store.LegacyOrgId)

	want := []ocpi.Version{
		{
			Version: "2.2",
			Url:     "/ocpi/2.2",
		},
	}

	got, err := ocpiApi.GetVersions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetVersionDetails(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", store.LegacyOrgId)

	want := ocpi.VersionDetail{
		Version: "2.2",
		Endpoints: []ocpi.Endpoint{
			{
				Identifier: "credentials",
				Role:       ocpi.RECEIVER,
				Url:        "/ocpi/2.2/credentials",
			},
			{
				Identifier: "commands",
				Role:       ocpi.RECEIVER,
				Url:        "/ocpi/receiver/2.2/commands",
			},
			{
				Identifier: "tokens",
				Role:       ocpi.RECEIVER,
				Url:        "/ocpi/receiver/2.2/tokens/",
			},
		},
	}

	got, err := ocpiApi.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetToken(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", store.LegacyOrgId)
	err := engine.SetToken(context.Background(), &store.Token{
		OrgId:       store.LegacyOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		ContractId:  "GBTWKTWTW000018",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
	})
	require.NoError(t, err)

	want := &ocpi.Token{
		ContractId:  "GBTWKTWTW000018",
		CountryCode: "GB",
		Issuer:      "Thoughtworks",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		Valid:       true,
		Whitelist:   "ALWAYS",
	}

	got, err := ocpiApi.GetToken(context.Background(), "GB", "TWK", "DEADBEEF")
	require.NoError(t, err)

	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`, got.LastUpdated)
	got.LastUpdated = ""
	assert.Equal(t, want, got)
}

func TestGetTokenThatIsNotKnown(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", ocpiTestOrgId)

	got, err := ocpiApi.GetToken(context.Background(), "GB", "TWK", "NOSUCHTOKEN")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetTokenOnlyReadsTheTokensOfTheDefaultOrg(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", ocpiTestOrgId)
	token := func(orgId, uid string) *store.Token {
		return &store.Token{
			OrgId:       orgId,
			CountryCode: "GB",
			PartyId:     "TWK",
			Type:        "RFID",
			Uid:         uid,
			ContractId:  "GBTWKTWTW000018",
			Issuer:      "Thoughtworks",
			Valid:       true,
			CacheMode:   "ALWAYS",
		}
	}
	require.NoError(t, engine.SetToken(context.Background(), token(ocpiTestOrgId, "DEADBEEF")))
	require.NoError(t, engine.SetToken(context.Background(), token("0193b2c4-0000-7000-8000-0000000000b2", "OTHERORG")))

	got, err := ocpiApi.GetToken(context.Background(), "GB", "TWK", "DEADBEEF")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "DEADBEEF", got.Uid)

	got, err = ocpiApi.GetToken(context.Background(), "GB", "TWK", "OTHERORG")
	require.NoError(t, err)
	assert.Nil(t, got)

	// the token of another party is not returned either
	got, err = ocpiApi.GetToken(context.Background(), "GB", "EVO", "DEADBEEF")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSetToken(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", ocpiTestOrgId)

	err := ocpiApi.SetToken(context.Background(), ocpi.Token{
		ContractId:  "GBTWKTWTW000018",
		CountryCode: "GB",
		Issuer:      "Thoughtworks",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		Valid:       true,
		Whitelist:   "ALWAYS",
	})
	require.NoError(t, err)

	// the token belongs to the default org of the OCPI API
	want := &store.Token{
		OrgId:       ocpiTestOrgId,
		CountryCode: "GB",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		ContractId:  "GBTWKTWTW000018",
		Issuer:      "Thoughtworks",
		Valid:       true,
		CacheMode:   "ALWAYS",
	}

	got, err := engine.LookupTokenForOrg(context.Background(), ocpiTestOrgId, "DEADBEEF")
	require.NoError(t, err)
	require.NotNil(t, got)
	got.LastUpdated = ""
	assert.Equal(t, want, got)
}

func TestSetTokenWithoutDefaultOrgUsesTheLegacyOrg(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", "")

	err := ocpiApi.SetToken(context.Background(), ocpi.Token{
		ContractId:  "GBTWKTWTW000018",
		CountryCode: "GB",
		Issuer:      "Thoughtworks",
		PartyId:     "TWK",
		Type:        "RFID",
		Uid:         "DEADBEEF",
		Valid:       true,
		Whitelist:   "ALWAYS",
	})
	require.NoError(t, err)

	got, err := engine.LookupTokenForOrg(context.Background(), store.LegacyOrgId, "DEADBEEF")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestPushLocation(t *testing.T) {
	engine := inmemory.NewStore(clock.RealClock{})
	ocpiApi := ocpi.NewOCPI(engine, http.DefaultClient, "GB", "TWK", store.LegacyOrgId)

	mux := http.NewServeMux()
	receiverServer := httptest.NewServer(mux)
	defer receiverServer.Close()
	mux.HandleFunc("/ocpi/versions", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"data":[{"version":"2.2","url":"%s/ocpi/2.2"}], "status_code":1000}`, receiverServer.URL)))
	})
	mux.HandleFunc("/ocpi/2.2", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`{"data":{
				"version":"2.2",
				"endpoints":[{"identifier":"locations","role":"RECEIVER","url":"%s/ocpi/receiver/2.2/locations"}]},
				"status_code":1000}`,
			receiverServer.URL)))
	})
	mux.HandleFunc("/ocpi/receiver/2.2/locations/GB/TWK/loc001", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, r.Header.Get("X-Correlation-ID"), "some-correlation-id")
		var got []byte
		r.Body.Read(got)
		assert.Equal(t, []byte(nil), got)
		w.WriteHeader(http.StatusCreated)
	})
	err := ocpiApi.SetCredentials(context.Background(), "some-token-123", ocpi.Credentials{
		Roles: []ocpi.CredentialsRole{
			{
				CountryCode: "GB",
				PartyId:     "TWK",
				Role:        ocpi.CredentialsRoleRoleEMSP,
			},
		},
		Token: "some-token-456",
		Url:   receiverServer.URL + "/ocpi/versions",
	})
	require.NoError(t, err)

	ctxWithCorrelationId := context.WithValue(context.Background(), ocpi.ContextKeyCorrelationId, "some-correlation-id")
	err = ocpiApi.PushLocation(ctxWithCorrelationId, ocpi.Location{Id: "loc001"})

	require.NoError(t, err)
}
