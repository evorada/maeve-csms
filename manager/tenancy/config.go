// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

//go:embed config_schema.json
var configSchemaDocument []byte

var configSchema = compileConfigSchema()

// The values of TenantConfig.DefaultSecurityProfile: the OCPP security profiles 1 to 3.
const (
	SecurityProfileSP1 = "SP1"
	SecurityProfileSP2 = "SP2"
	SecurityProfileSP3 = "SP3"
)

// The values of TenantConfig.OnboardingMode.
const (
	OnboardingPendingApproval   = "pending_approval"
	OnboardingPreregisteredOnly = "preregistered_only"
)

// The limits of TenantConfig.HeartbeatIntervalS, as in the schema.
const (
	minHeartbeatIntervalS = 60
	maxHeartbeatIntervalS = 86400
)

// The defaults and limits of the onboarding settings, as in the schema.
const (
	DefaultOnboardingMaxPending   = 50
	DefaultOnboardingBootInterval = 60 * time.Second

	maxOnboardingMaxPending   = 10000
	minOnboardingBootInterval = 10
	maxOnboardingBootInterval = 86400
)

// TenantConfig is the config document of a tenant. The zero value, and a nil pointer,
// stand for a tenant that has not set anything: the methods give the defaults.
type TenantConfig struct {
	Version int64 `json:"version"`
	// DefaultSecurityProfile is SP1, SP2 or SP3; see SecurityProfile.
	DefaultSecurityProfile string `json:"default_security_profile,omitempty"`
	// AcceptedCertOrgNames are the organisation names that are accepted in the client
	// certificate of a charge station. Empty means that the organisation is not checked.
	AcceptedCertOrgNames []string `json:"accepted_cert_org_names,omitempty"`
	// HeartbeatIntervalS is 60 to 86400; see HeartbeatInterval.
	HeartbeatIntervalS *int `json:"heartbeat_interval_s,omitempty"`
	// DefaultTariffRef is the id of the tariff of a session for which no other tariff is
	// found; see DefaultTariff.
	DefaultTariffRef *string `json:"default_tariff_ref,omitempty"`
	// AdhocEmspParties are the parties whose tokens make a session an ad hoc session,
	// each as "CC*PID"; see AdhocEmspParty.
	AdhocEmspParties []string `json:"adhoc_emsp_parties,omitempty"`
	// OnboardingMode is pending_approval or preregistered_only; see Onboarding.
	OnboardingMode string `json:"onboarding_mode,omitempty"`
	// OnboardingOpenSP1 lets a station that is not registered connect for its onboarding
	// without TLS and without a password; see OnboardingWithoutPassword.
	OnboardingOpenSP1 *bool `json:"onboarding_open_sp1,omitempty"`
	// OnboardingPasswordSHA256 is the base64 encoded SHA-256 hash of the password with
	// which a station that is not registered may connect for its onboarding.
	OnboardingPasswordSHA256 string `json:"onboarding_password_sha256,omitempty"`
	// OnboardingMaxPending is 0 to 10000; see OnboardingMaxPendingStations.
	OnboardingMaxPending *int `json:"onboarding_max_pending,omitempty"`
	// OnboardingBootIntervalS is 10 to 86400; see OnboardingBootInterval.
	OnboardingBootIntervalS *int `json:"onboarding_boot_interval_s,omitempty"`
}

// SecurityProfile returns the security profile of a charge station that is registered
// without one. A tenant that names none, or anything that is not a profile, gets TLS
// with basic auth (SP2): never the profile without TLS.
func (c *TenantConfig) SecurityProfile() store.SecurityProfile {
	if c != nil {
		switch c.DefaultSecurityProfile {
		case SecurityProfileSP1:
			return store.UnsecuredTransportWithBasicAuth
		case SecurityProfileSP3:
			return store.TLSWithClientSideCertificates
		}
	}
	return store.TLSWithBasicAuth
}

// Onboarding returns what happens to a charge station that is not registered:
// OnboardingPendingApproval unless the tenant chose OnboardingPreregisteredOnly.
func (c *TenantConfig) Onboarding() string {
	if c != nil && c.OnboardingMode == OnboardingPreregisteredOnly {
		return OnboardingPreregisteredOnly
	}
	return OnboardingPendingApproval
}

// OnboardingWithoutPassword reports whether a station that is not registered may connect
// for its onboarding without TLS and without a password (true unless the tenant turned
// it off).
func (c *TenantConfig) OnboardingWithoutPassword() bool {
	return c == nil || c.OnboardingOpenSP1 == nil || *c.OnboardingOpenSP1
}

// OnboardingPassword returns the base64 encoded SHA-256 hash of the onboarding password
// of the tenant, "" when it has none.
func (c *TenantConfig) OnboardingPassword() string {
	if c == nil {
		return ""
	}
	return c.OnboardingPasswordSHA256
}

// OnboardingMaxPendingStations returns the most stations of the tenant that may wait for
// approval at the same time: DefaultOnboardingMaxPending unless the tenant set a number
// inside the limits of the schema.
func (c *TenantConfig) OnboardingMaxPendingStations() int {
	if c == nil || c.OnboardingMaxPending == nil {
		return DefaultOnboardingMaxPending
	}
	maxPending := *c.OnboardingMaxPending
	if maxPending < 0 || maxPending > maxOnboardingMaxPending {
		return DefaultOnboardingMaxPending
	}
	return maxPending
}

// OnboardingBootInterval returns how long a station that waits for approval waits before
// its next BootNotification: DefaultOnboardingBootInterval unless the tenant set an
// interval inside the limits of the schema.
func (c *TenantConfig) OnboardingBootInterval() time.Duration {
	if c == nil || c.OnboardingBootIntervalS == nil {
		return DefaultOnboardingBootInterval
	}
	seconds := *c.OnboardingBootIntervalS
	if seconds < minOnboardingBootInterval || seconds > maxOnboardingBootInterval {
		return DefaultOnboardingBootInterval
	}
	return time.Duration(seconds) * time.Second
}

// DefaultTariff returns the id of the default tariff of the tenant, "" when it has none.
func (c *TenantConfig) DefaultTariff() string {
	if c == nil || c.DefaultTariffRef == nil {
		return ""
	}
	return *c.DefaultTariffRef
}

// AdhocEmspParty reports whether the party with the country code and the party id is one
// of the parties of the tenant that issue the tokens of ad hoc payment. Country codes and
// party ids are compared without regard to case, as OCPI does.
func (c *TenantConfig) AdhocEmspParty(countryCode, partyId string) bool {
	if c == nil {
		return false
	}
	party := countryCode + "*" + partyId
	for _, adhoc := range c.AdhocEmspParties {
		if strings.EqualFold(adhoc, party) {
			return true
		}
	}
	return false
}

// HeartbeatInterval returns the heartbeat interval of the charge stations of the
// tenant: global unless the tenant set one inside the limits of the schema.
func (c *TenantConfig) HeartbeatInterval(global time.Duration) time.Duration {
	if c == nil || c.HeartbeatIntervalS == nil {
		return global
	}
	seconds := *c.HeartbeatIntervalS
	if seconds < minHeartbeatIntervalS || seconds > maxHeartbeatIntervalS {
		return global
	}
	return time.Duration(seconds) * time.Second
}

// ConfigProblem is one thing that is wrong with the config document of a tenant.
type ConfigProblem struct {
	// Field is the setting: "heartbeat_interval_s", "accepted_cert_org_names[1]", or
	// "" for the document as a whole.
	Field string
	// Message says which rule the value breaks. It only repeats the value and the rule.
	Message string
}

// ConfigError is the error of a document that is not a valid tenant config.
type ConfigError struct {
	// Problems is never empty.
	Problems []ConfigProblem
}

func (e *ConfigError) Error() string {
	parts := make([]string, 0, len(e.Problems))
	for _, problem := range e.Problems {
		if problem.Field == "" {
			parts = append(parts, problem.Message)
			continue
		}
		parts = append(parts, problem.Field+": "+problem.Message)
	}
	return "invalid tenant config: " + strings.Join(parts, "; ")
}

// ConfigSchema returns the JSON Schema (draft-07) of the config of a tenant, as the
// manifest of the control plane publishes it.
func ConfigSchema() json.RawMessage {
	return bytes.Clone(configSchemaDocument)
}

// ValidateConfig checks a config document against ConfigSchema. A document that is not
// valid gives a *ConfigError that names every setting that is wrong.
func ValidateConfig(doc json.RawMessage) error {
	_, err := decodeConfig(doc)
	return err
}

// ParseConfig reads the stored config document of a tenant. A tenant that was never
// given a config (no document, null or an empty object) has the zero config. Any other
// document must be valid (see ValidateConfig).
func ParseConfig(doc json.RawMessage) (*TenantConfig, error) {
	if noConfig(doc) {
		return &TenantConfig{}, nil
	}
	return decodeConfig(doc)
}

// noConfig reports whether the document is what the tenant store keeps for a tenant
// without a config.
func noConfig(doc json.RawMessage) bool {
	trimmed := bytes.TrimSpace(doc)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return true
	}
	var members map[string]json.RawMessage
	return json.Unmarshal(trimmed, &members) == nil && len(members) == 0
}

func decodeConfig(doc json.RawMessage) (*TenantConfig, error) {
	if !json.Valid(doc) {
		return nil, &ConfigError{Problems: []ConfigProblem{{Message: "must be a JSON document"}}}
	}
	value, err := jsonschema.DecodeJSON(bytes.NewReader(doc))
	if err != nil {
		return nil, &ConfigError{Problems: []ConfigProblem{{Message: "must be a JSON document"}}}
	}
	if err := configSchema.ValidateInterface(value); err != nil {
		return nil, &ConfigError{Problems: configProblems(value, err)}
	}

	var cfg TenantConfig
	if err := json.Unmarshal(doc, &cfg); err != nil {
		// what the schema accepts and the manager cannot keep, like a version that does
		// not fit in 64 bits
		problem := ConfigProblem{Message: "cannot be read"}
		var typeError *json.UnmarshalTypeError
		if errors.As(err, &typeError) {
			problem.Field = typeError.Field
		}
		return nil, &ConfigError{Problems: []ConfigProblem{problem}}
	}
	return &cfg, nil
}

// configProblems names every setting of a document that the schema refused. The schema
// validator stops at the first setting that is wrong, so the settings are checked one by
// one here, each against its own part of the schema.
func configProblems(value interface{}, schemaError error) []ConfigProblem {
	var problems []ConfigProblem
	if settings, ok := value.(map[string]interface{}); ok {
		names := make([]string, 0, len(settings))
		for name := range settings {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			setting, known := configSchema.Properties[name]
			if !known {
				problems = append(problems, ConfigProblem{Field: name, Message: "is not a setting of the tenant config"})
				continue
			}
			if err := setting.ValidateInterface(settings[name]); err != nil {
				pointer, message := firstCause(err)
				problems = append(problems, ConfigProblem{Field: name + fieldSuffix(pointer), Message: message})
			}
		}
		for _, name := range configSchema.Required {
			if _, ok := settings[name]; !ok {
				problems = append(problems, ConfigProblem{Field: name, Message: "is required"})
			}
		}
	}
	if len(problems) == 0 {
		// refused for another reason than one of its settings
		pointer, message := firstCause(schemaError)
		problems = append(problems, ConfigProblem{Field: strings.TrimPrefix(fieldSuffix(pointer), "."), Message: message})
	}
	return problems
}

// firstCause returns where in the checked value the first rule was broken, as a JSON
// pointer, and the message of that rule.
func firstCause(err error) (pointer, message string) {
	var validationError *jsonschema.ValidationError
	if !errors.As(err, &validationError) {
		return "", "is not valid"
	}
	for len(validationError.Causes) > 0 {
		validationError = validationError.Causes[0]
	}
	return validationError.InstancePtr, validationError.Message
}

// fieldSuffix turns the JSON pointer of a schema error ("#", "#/1", "#/a/0") into what
// follows the name of a setting: "", "[1]", ".a[0]".
func fieldSuffix(pointer string) string {
	var suffix strings.Builder
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "#"), "/") {
		if part == "" {
			continue
		}
		if _, err := strconv.Atoi(part); err == nil {
			suffix.WriteString("[" + part + "]")
			continue
		}
		suffix.WriteString("." + part)
	}
	return suffix.String()
}

func compileConfigSchema() *jsonschema.Schema {
	const name = "config_schema.json"
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7
	if err := compiler.AddResource(name, bytes.NewReader(configSchemaDocument)); err != nil {
		panic(fmt.Sprintf("tenant config schema: %v", err))
	}
	schema, err := compiler.Compile(name)
	if err != nil {
		panic(fmt.Sprintf("tenant config schema: %v", err))
	}
	return schema
}
