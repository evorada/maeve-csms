// SPDX-License-Identifier: Apache-2.0

package tenancy_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

const fullConfig = `{
	"version": 12,
	"default_security_profile": "SP3",
	"accepted_cert_org_names": ["Charge Co", "Charge Co Services"],
	"heartbeat_interval_s": 120,
	"default_tariff_ref": "tariff-standard",
	"adhoc_emsp_parties": ["XX*PAY", "es*Ad1"],
	"onboarding_mode": "preregistered_only",
	"onboarding_open_sp1": false,
	"onboarding_password_sha256": "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=",
	"onboarding_max_pending": 10,
	"onboarding_boot_interval_s": 120
}`

func TestConfigSchema(t *testing.T) {
	var schema struct {
		Schema               string   `json:"$schema"`
		Type                 string   `json:"type"`
		Required             []string `json:"required"`
		AdditionalProperties *bool    `json:"additionalProperties"`
		Properties           map[string]struct {
			Title       string           `json:"title"`
			Description string           `json:"description"`
			Default     *json.RawMessage `json:"default"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tenancy.ConfigSchema(), &schema))

	assert.Equal(t, "http://json-schema.org/draft-07/schema#", schema.Schema)
	assert.Equal(t, "object", schema.Type)
	assert.Equal(t, []string{"version"}, schema.Required)
	require.NotNil(t, schema.AdditionalProperties)
	assert.False(t, *schema.AdditionalProperties)

	// the settings that the manager reads are the settings of the schema
	var names []string
	for name, property := range schema.Properties {
		names = append(names, name)
		// the platform renders its form from these
		assert.NotEmpty(t, property.Title, name)
		assert.NotEmpty(t, property.Description, name)
	}
	assert.ElementsMatch(t, []string{
		"version", "default_security_profile", "accepted_cert_org_names",
		"heartbeat_interval_s", "default_tariff_ref", "adhoc_emsp_parties", "onboarding_mode",
		"onboarding_open_sp1", "onboarding_password_sha256", "onboarding_max_pending", "onboarding_boot_interval_s",
	}, names)

	// the defaults that the schema shows are the defaults that the manager applies
	defaults := &tenancy.TenantConfig{}
	assert.JSONEq(t, `"SP2"`, string(*schema.Properties["default_security_profile"].Default))
	assert.Equal(t, store.TLSWithBasicAuth, defaults.SecurityProfile())
	assert.JSONEq(t, `"pending_approval"`, string(*schema.Properties["onboarding_mode"].Default))
	assert.Equal(t, tenancy.OnboardingPendingApproval, defaults.Onboarding())
	assert.JSONEq(t, `[]`, string(*schema.Properties["accepted_cert_org_names"].Default))
	assert.JSONEq(t, `[]`, string(*schema.Properties["adhoc_emsp_parties"].Default))
	assert.False(t, defaults.AdhocEmspParty("XX", "PAY"))
	assert.Nil(t, schema.Properties["default_tariff_ref"].Default, "there is no default tariff unless the tenant sets one")
	assert.Empty(t, defaults.DefaultTariff())
	assert.JSONEq(t, `true`, string(*schema.Properties["onboarding_open_sp1"].Default))
	assert.True(t, defaults.OnboardingWithoutPassword())
	assert.JSONEq(t, `50`, string(*schema.Properties["onboarding_max_pending"].Default))
	assert.Equal(t, 50, defaults.OnboardingMaxPendingStations())
	assert.JSONEq(t, `60`, string(*schema.Properties["onboarding_boot_interval_s"].Default))
	assert.Equal(t, 60*time.Second, defaults.OnboardingBootInterval())
	assert.Nil(t, schema.Properties["onboarding_password_sha256"].Default, "there is no onboarding password unless the tenant sets one")
	assert.Empty(t, defaults.OnboardingPassword())

	t.Run("a caller cannot change the schema", func(t *testing.T) {
		tenancy.ConfigSchema()[0] = 'x'
		assert.True(t, json.Valid(tenancy.ConfigSchema()))
	})
}

func TestValidateConfigAcceptsAValidConfig(t *testing.T) {
	for name, doc := range map[string]string{
		"every setting":                            fullConfig,
		"only the version":                         `{"version":1}`,
		"version zero":                             `{"version":0}`,
		"an empty list of names":                   `{"version":1,"accepted_cert_org_names":[]}`,
		"the lowest heartbeat":                     `{"version":1,"heartbeat_interval_s":60}`,
		"the highest heartbeat":                    `{"version":1,"heartbeat_interval_s":86400}`,
		"the first profile":                        `{"version":1,"default_security_profile":"SP1"}`,
		"pending approval":                         `{"version":1,"onboarding_mode":"pending_approval"}`,
		"twenty names":                             `{"version":1,"accepted_cert_org_names":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20"]}`,
		"whitespace around it":                     " \n{\"version\":1}\n ",
		"a name of 64 characters":                  `{"version":1,"accepted_cert_org_names":["0123456789012345678901234567890123456789012345678901234567890123"]}`,
		"onboarding without a limit or a password": `{"version":1,"onboarding_max_pending":0,"onboarding_open_sp1":false}`,
		"the most pending stations":                `{"version":1,"onboarding_max_pending":10000}`,
		"the shortest boot interval":               `{"version":1,"onboarding_boot_interval_s":10}`,
		"the longest boot interval":                `{"version":1,"onboarding_boot_interval_s":86400}`,
		"an onboarding password with + and /":      `{"version":1,"onboarding_password_sha256":"+/+/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`,
		"no parties of ad hoc payment":             `{"version":1,"adhoc_emsp_parties":[]}`,
		"parties of ad hoc payment in any case":    `{"version":1,"adhoc_emsp_parties":["XX*PAY","nl*a1b","De*0Z9"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, tenancy.ValidateConfig(json.RawMessage(doc)))
		})
	}
}

func TestValidateConfigRefusesAnInvalidConfig(t *testing.T) {
	cases := []struct {
		name   string
		doc    string
		fields []string // the fields of the problems, in order
	}{
		{name: "a security profile that does not exist", doc: `{"version":1,"default_security_profile":"SP4"}`, fields: []string{"default_security_profile"}},
		{name: "a security profile as a number", doc: `{"version":1,"default_security_profile":2}`, fields: []string{"default_security_profile"}},
		{name: "a security profile in lower case", doc: `{"version":1,"default_security_profile":"sp1"}`, fields: []string{"default_security_profile"}},
		{name: "a heartbeat below the minimum", doc: `{"version":1,"heartbeat_interval_s":30}`, fields: []string{"heartbeat_interval_s"}},
		{name: "a heartbeat above the maximum", doc: `{"version":1,"heartbeat_interval_s":86401}`, fields: []string{"heartbeat_interval_s"}},
		{name: "a heartbeat that is not a whole number", doc: `{"version":1,"heartbeat_interval_s":120.5}`, fields: []string{"heartbeat_interval_s"}},
		{name: "a heartbeat as a text", doc: `{"version":1,"heartbeat_interval_s":"120"}`, fields: []string{"heartbeat_interval_s"}},
		{name: "an unknown setting", doc: `{"version":1,"heartbeat_interval":120}`, fields: []string{"heartbeat_interval"}},
		{name: "session rules", doc: `{"version":1,"session_rules":{"max_kwh":50}}`, fields: []string{"session_rules"}},
		{name: "a setting in another case", doc: `{"version":1,"Default_Security_Profile":"SP1"}`, fields: []string{"Default_Security_Profile"}},
		{name: "an onboarding mode that does not exist", doc: `{"version":1,"onboarding_mode":"open"}`, fields: []string{"onboarding_mode"}},
		{name: "an onboarding password that is not a hash", doc: `{"version":1,"onboarding_password_sha256":"secret"}`, fields: []string{"onboarding_password_sha256"}},
		{name: "an onboarding password that is not base64", doc: `{"version":1,"onboarding_password_sha256":"XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQt-="}`, fields: []string{"onboarding_password_sha256"}},
		{name: "an empty onboarding password", doc: `{"version":1,"onboarding_password_sha256":""}`, fields: []string{"onboarding_password_sha256"}},
		{name: "onboarding without a password as a text", doc: `{"version":1,"onboarding_open_sp1":"false"}`, fields: []string{"onboarding_open_sp1"}},
		{name: "a negative number of pending stations", doc: `{"version":1,"onboarding_max_pending":-1}`, fields: []string{"onboarding_max_pending"}},
		{name: "too many pending stations", doc: `{"version":1,"onboarding_max_pending":10001}`, fields: []string{"onboarding_max_pending"}},
		{name: "a boot interval below the minimum", doc: `{"version":1,"onboarding_boot_interval_s":9}`, fields: []string{"onboarding_boot_interval_s"}},
		{name: "a boot interval above the maximum", doc: `{"version":1,"onboarding_boot_interval_s":86401}`, fields: []string{"onboarding_boot_interval_s"}},
		{name: "no version", doc: `{"default_security_profile":"SP3"}`, fields: []string{"version"}},
		{name: "an empty object", doc: `{}`, fields: []string{"version"}},
		{name: "a negative version", doc: `{"version":-1}`, fields: []string{"version"}},
		{name: "a version that does not fit in 64 bits", doc: `{"version":92233720368547758070}`, fields: []string{"version"}},
		{name: "names that are not a list", doc: `{"version":1,"accepted_cert_org_names":"Charge Co"}`, fields: []string{"accepted_cert_org_names"}},
		{name: "a name that is not a text", doc: `{"version":1,"accepted_cert_org_names":["Charge Co",7]}`, fields: []string{"accepted_cert_org_names[1]"}},
		{name: "an empty name", doc: `{"version":1,"accepted_cert_org_names":[""]}`, fields: []string{"accepted_cert_org_names[0]"}},
		{name: "no names as null", doc: `{"version":1,"accepted_cert_org_names":null}`, fields: []string{"accepted_cert_org_names"}},
		{name: "a tariff that is not a text", doc: `{"version":1,"default_tariff_ref":7}`, fields: []string{"default_tariff_ref"}},
		{name: "parties that are not a list", doc: `{"version":1,"adhoc_emsp_parties":"XX*PAY"}`, fields: []string{"adhoc_emsp_parties"}},
		{name: "a party without the asterisk", doc: `{"version":1,"adhoc_emsp_parties":["XX*PAY","XXPAY"]}`, fields: []string{"adhoc_emsp_parties[1]"}},
		{name: "a party with a hyphen", doc: `{"version":1,"adhoc_emsp_parties":["XX-PAY"]}`, fields: []string{"adhoc_emsp_parties[0]"}},
		{name: "a party id that is too long", doc: `{"version":1,"adhoc_emsp_parties":["XX*PAYS"]}`, fields: []string{"adhoc_emsp_parties[0]"}},
		{name: "a country code that is too short", doc: `{"version":1,"adhoc_emsp_parties":["X*PAY"]}`, fields: []string{"adhoc_emsp_parties[0]"}},
		{name: "a party with something after it", doc: `{"version":1,"adhoc_emsp_parties":["XX*PAY\n"]}`, fields: []string{"adhoc_emsp_parties[0]"}},
		{name: "a party that is not a text", doc: `{"version":1,"adhoc_emsp_parties":[7]}`, fields: []string{"adhoc_emsp_parties[0]"}},
		{name: "not an object", doc: `["version"]`, fields: []string{""}},
		{name: "null", doc: `null`, fields: []string{""}},
		{name: "not JSON", doc: `{"version":1`, fields: []string{""}},
		{name: "two documents", doc: `{"version":1}{"version":2}`, fields: []string{""}},
		{name: "nothing", doc: ``, fields: []string{""}},
		{
			name:   "every problem is named, in the order of the settings",
			doc:    `{"session_rules":{},"heartbeat_interval_s":30,"default_security_profile":"SP4"}`,
			fields: []string{"default_security_profile", "heartbeat_interval_s", "session_rules", "version"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tenancy.ValidateConfig(json.RawMessage(tc.doc))
			require.Error(t, err)
			var configError *tenancy.ConfigError
			require.ErrorAs(t, err, &configError)

			var fields []string
			for _, problem := range configError.Problems {
				fields = append(fields, problem.Field)
				assert.NotEmpty(t, problem.Message)
			}
			assert.Equal(t, tc.fields, fields, err.Error())

			// a document that is not valid is never read as a config
			cfg, err := tenancy.ParseConfig(json.RawMessage(tc.doc))
			if err == nil {
				// what the tenant store keeps for a tenant without a config
				assert.Equal(t, &tenancy.TenantConfig{}, cfg, "only no config at all may be read as the defaults")
				assert.Contains(t, []string{`{}`, `null`, ``}, tc.doc)
			}
		})
	}

	t.Run("the message repeats the rule", func(t *testing.T) {
		err := tenancy.ValidateConfig(json.RawMessage(`{"version":1,"heartbeat_interval_s":30}`))
		assert.EqualError(t, err, "invalid tenant config: heartbeat_interval_s: must be >= 60 but found 30")
	})
}

func TestParseConfig(t *testing.T) {
	cfg, err := tenancy.ParseConfig(json.RawMessage(fullConfig))
	require.NoError(t, err)

	heartbeat, tariff, openSP1, maxPending, bootInterval := 120, "tariff-standard", false, 10, 120
	assert.Equal(t, &tenancy.TenantConfig{
		Version:                  12,
		DefaultSecurityProfile:   "SP3",
		AcceptedCertOrgNames:     []string{"Charge Co", "Charge Co Services"},
		HeartbeatIntervalS:       &heartbeat,
		DefaultTariffRef:         &tariff,
		AdhocEmspParties:         []string{"XX*PAY", "es*Ad1"},
		OnboardingMode:           tenancy.OnboardingPreregisteredOnly,
		OnboardingOpenSP1:        &openSP1,
		OnboardingPasswordSHA256: "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=",
		OnboardingMaxPending:     &maxPending,
		OnboardingBootIntervalS:  &bootInterval,
	}, cfg)
	assert.False(t, cfg.OnboardingWithoutPassword())
	assert.Equal(t, "XohImNooBHFR0OVvjcYpJ3NgPQ1qq73WKhHvch0VQtg=", cfg.OnboardingPassword())
	assert.Equal(t, 10, cfg.OnboardingMaxPendingStations())
	assert.Equal(t, 2*time.Minute, cfg.OnboardingBootInterval())
	assert.Equal(t, store.TLSWithClientSideCertificates, cfg.SecurityProfile())
	assert.Equal(t, tenancy.OnboardingPreregisteredOnly, cfg.Onboarding())
	assert.Equal(t, 2*time.Minute, cfg.HeartbeatInterval(5*time.Minute))
	assert.Equal(t, "tariff-standard", cfg.DefaultTariff())
	assert.True(t, cfg.AdhocEmspParty("XX", "PAY"))
	assert.True(t, cfg.AdhocEmspParty("xx", "pay"), "country codes and party ids have no case")
	assert.True(t, cfg.AdhocEmspParty("ES", "AD1"))
	assert.False(t, cfg.AdhocEmspParty("XX", "PAX"))
	assert.False(t, cfg.AdhocEmspParty("X", "XPAY"), "the two parts are not run together")
	assert.False(t, cfg.AdhocEmspParty("", ""))
	var none *tenancy.TenantConfig
	assert.False(t, none.AdhocEmspParty("XX", "PAY"))
	assert.Empty(t, none.DefaultTariff())

	t.Run("a tenant without a config has the zero config", func(t *testing.T) {
		for _, doc := range []string{``, `null`, `{}`, ` { } `} {
			cfg, err := tenancy.ParseConfig(json.RawMessage(doc))
			require.NoError(t, err, doc)
			assert.Equal(t, &tenancy.TenantConfig{}, cfg, doc)
		}
	})
}

func TestTenantConfigDefaults(t *testing.T) {
	global := 5 * time.Minute

	for name, cfg := range map[string]*tenancy.TenantConfig{
		"an empty config": {},
		"no config":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			// TLS with basic auth (SP2)
			assert.Equal(t, store.SecurityProfile(1), cfg.SecurityProfile())
			assert.Equal(t, tenancy.OnboardingPendingApproval, cfg.Onboarding())
			assert.Equal(t, global, cfg.HeartbeatInterval(global))
			assert.True(t, cfg.OnboardingWithoutPassword())
			assert.Empty(t, cfg.OnboardingPassword())
			assert.Equal(t, 50, cfg.OnboardingMaxPendingStations())
			assert.Equal(t, time.Minute, cfg.OnboardingBootInterval())
		})
	}

	t.Run("onboarding settings outside the limits are not applied", func(t *testing.T) {
		for value, want := range map[int]int{0: 0, 10000: 10000, -1: 50, 10001: 50} {
			value := value
			cfg := &tenancy.TenantConfig{OnboardingMaxPending: &value}
			assert.Equal(t, want, cfg.OnboardingMaxPendingStations(), value)
		}
		for seconds, want := range map[int]time.Duration{10: 10 * time.Second, 86400: 24 * time.Hour, 9: time.Minute, 0: time.Minute, 86401: time.Minute} {
			seconds := seconds
			cfg := &tenancy.TenantConfig{OnboardingBootIntervalS: &seconds}
			assert.Equal(t, want, cfg.OnboardingBootInterval(), seconds)
		}
		openSP1 := true
		assert.True(t, (&tenancy.TenantConfig{OnboardingOpenSP1: &openSP1}).OnboardingWithoutPassword())
	})

	t.Run("the security profiles", func(t *testing.T) {
		for value, want := range map[string]store.SecurityProfile{
			"SP1": store.UnsecuredTransportWithBasicAuth,
			"SP2": store.TLSWithBasicAuth,
			"SP3": store.TLSWithClientSideCertificates,
			// nothing that the schema refuses becomes the profile without TLS
			"":    store.TLSWithBasicAuth,
			"SP0": store.TLSWithBasicAuth,
			"SP4": store.TLSWithBasicAuth,
			"sp1": store.TLSWithBasicAuth,
			"0":   store.TLSWithBasicAuth,
		} {
			cfg := &tenancy.TenantConfig{DefaultSecurityProfile: value}
			assert.Equal(t, want, cfg.SecurityProfile(), value)
		}
	})

	t.Run("a heartbeat interval outside the limits is not applied", func(t *testing.T) {
		for seconds, want := range map[int]time.Duration{
			60:    time.Minute,
			86400: 24 * time.Hour,
			59:    global,
			0:     global,
			-120:  global,
			86401: global,
		} {
			seconds := seconds
			cfg := &tenancy.TenantConfig{HeartbeatIntervalS: &seconds}
			assert.Equal(t, want, cfg.HeartbeatInterval(global), seconds)
		}
	})

	t.Run("an onboarding mode that does not exist is the default", func(t *testing.T) {
		cfg := &tenancy.TenantConfig{OnboardingMode: "open"}
		assert.Equal(t, tenancy.OnboardingPendingApproval, cfg.Onboarding())
	})
}
