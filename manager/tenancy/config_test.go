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
	"onboarding_mode": "preregistered_only"
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
		// the console renders its form from these
		assert.NotEmpty(t, property.Title, name)
		assert.NotEmpty(t, property.Description, name)
	}
	assert.ElementsMatch(t, []string{
		"version", "default_security_profile", "accepted_cert_org_names",
		"heartbeat_interval_s", "default_tariff_ref", "onboarding_mode",
	}, names)

	// the defaults that the schema shows are the defaults that the manager applies
	defaults := &tenancy.TenantConfig{}
	assert.JSONEq(t, `"SP2"`, string(*schema.Properties["default_security_profile"].Default))
	assert.Equal(t, store.TLSWithBasicAuth, defaults.SecurityProfile())
	assert.JSONEq(t, `"pending_approval"`, string(*schema.Properties["onboarding_mode"].Default))
	assert.Equal(t, tenancy.OnboardingPendingApproval, defaults.Onboarding())
	assert.JSONEq(t, `[]`, string(*schema.Properties["accepted_cert_org_names"].Default))

	t.Run("a caller cannot change the schema", func(t *testing.T) {
		tenancy.ConfigSchema()[0] = 'x'
		assert.True(t, json.Valid(tenancy.ConfigSchema()))
	})
}

func TestValidateConfigAcceptsAValidConfig(t *testing.T) {
	for name, doc := range map[string]string{
		"every setting":           fullConfig,
		"only the version":        `{"version":1}`,
		"version zero":            `{"version":0}`,
		"an empty list of names":  `{"version":1,"accepted_cert_org_names":[]}`,
		"the lowest heartbeat":    `{"version":1,"heartbeat_interval_s":60}`,
		"the highest heartbeat":   `{"version":1,"heartbeat_interval_s":86400}`,
		"the first profile":       `{"version":1,"default_security_profile":"SP1"}`,
		"pending approval":        `{"version":1,"onboarding_mode":"pending_approval"}`,
		"twenty names":            `{"version":1,"accepted_cert_org_names":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19","20"]}`,
		"whitespace around it":    " \n{\"version\":1}\n ",
		"a name of 64 characters": `{"version":1,"accepted_cert_org_names":["0123456789012345678901234567890123456789012345678901234567890123"]}`,
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
		{name: "no version", doc: `{"default_security_profile":"SP3"}`, fields: []string{"version"}},
		{name: "an empty object", doc: `{}`, fields: []string{"version"}},
		{name: "a negative version", doc: `{"version":-1}`, fields: []string{"version"}},
		{name: "a version that does not fit in 64 bits", doc: `{"version":92233720368547758070}`, fields: []string{"version"}},
		{name: "names that are not a list", doc: `{"version":1,"accepted_cert_org_names":"Charge Co"}`, fields: []string{"accepted_cert_org_names"}},
		{name: "a name that is not a text", doc: `{"version":1,"accepted_cert_org_names":["Charge Co",7]}`, fields: []string{"accepted_cert_org_names[1]"}},
		{name: "an empty name", doc: `{"version":1,"accepted_cert_org_names":[""]}`, fields: []string{"accepted_cert_org_names[0]"}},
		{name: "no names as null", doc: `{"version":1,"accepted_cert_org_names":null}`, fields: []string{"accepted_cert_org_names"}},
		{name: "a tariff that is not a text", doc: `{"version":1,"default_tariff_ref":7}`, fields: []string{"default_tariff_ref"}},
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

	heartbeat, tariff := 120, "tariff-standard"
	assert.Equal(t, &tenancy.TenantConfig{
		Version:                12,
		DefaultSecurityProfile: "SP3",
		AcceptedCertOrgNames:   []string{"Charge Co", "Charge Co Services"},
		HeartbeatIntervalS:     &heartbeat,
		DefaultTariffRef:       &tariff,
		OnboardingMode:         tenancy.OnboardingPreregisteredOnly,
	}, cfg)
	assert.Equal(t, store.TLSWithClientSideCertificates, cfg.SecurityProfile())
	assert.Equal(t, tenancy.OnboardingPreregisteredOnly, cfg.Onboarding())
	assert.Equal(t, 2*time.Minute, cfg.HeartbeatInterval(5*time.Minute))

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
		})
	}

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
