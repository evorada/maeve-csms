// SPDX-License-Identifier: Apache-2.0

package stations

import (
	"context"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/tenancy"
)

// OnboardingSettings are the settings of a tenant for the charge stations that connect
// to it without being registered.
type OnboardingSettings struct {
	// Enabled is set when such a station is admitted and kept pending until somebody
	// approves it. Otherwise it is refused, as a station that is not known.
	Enabled bool
	// OpenSP1 lets such a station connect without TLS and without a password.
	OpenSP1 bool
	// PasswordSHA256 is the base64 encoded SHA-256 hash of the onboarding password with
	// which such a station may connect; "" when the tenant has none.
	PasswordSHA256 string
	// MaxPending is the most stations of the tenant that may be pending at a time.
	MaxPending int
	// BootInterval is how long a pending station waits before its next BootNotification.
	BootInterval time.Duration
}

// LoadOnboardingSettings returns the onboarding settings of the tenant of an org.
// Onboarding is enabled for a tenant in the onboarding mode pending_approval, which is
// the mode of a tenant that has not chosen one. It is disabled, so that a station that is
// not registered is refused as before, for an org that is not a tenant (always so in
// single mode) and for a tenant whose stored config cannot be read: what such a tenant
// wants is not known.
func LoadOnboardingSettings(ctx context.Context, configs *tenancy.ConfigProvider, orgId string) (OnboardingSettings, error) {
	cfg, ok, err := configs.StoredConfigForOrg(ctx, orgId)
	if err != nil {
		return OnboardingSettings{}, err
	}
	if !ok {
		cfg = nil
	}
	return OnboardingSettings{
		Enabled:        ok && cfg.Onboarding() == tenancy.OnboardingPendingApproval,
		OpenSP1:        cfg.OnboardingWithoutPassword(),
		PasswordSHA256: cfg.OnboardingPassword(),
		MaxPending:     cfg.OnboardingMaxPendingStations(),
		BootInterval:   cfg.OnboardingBootInterval(),
	}, nil
}
