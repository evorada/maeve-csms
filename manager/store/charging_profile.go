// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"time"
)

// ChargingProfilePurpose indicates the purpose of a charging profile.
type ChargingProfilePurpose string

const (
	ChargingProfilePurposeTxProfile             ChargingProfilePurpose = "TxProfile"
	ChargingProfilePurposeTxDefaultProfile      ChargingProfilePurpose = "TxDefaultProfile"
	ChargingProfilePurposeChargePointMaxProfile ChargingProfilePurpose = "ChargePointMaxProfile"
)

// ChargingProfileKind indicates the kind of a charging profile.
type ChargingProfileKind string

const (
	ChargingProfileKindAbsolute  ChargingProfileKind = "Absolute"
	ChargingProfileKindRelative  ChargingProfileKind = "Relative"
	ChargingProfileKindRecurring ChargingProfileKind = "Recurring"
)

// RecurrencyKind indicates the recurrency kind of a charging profile.
type RecurrencyKind string

const (
	RecurrencyKindDaily  RecurrencyKind = "Daily"
	RecurrencyKindWeekly RecurrencyKind = "Weekly"
)

// ChargingRateUnit indicates the unit of a charging rate.
type ChargingRateUnit string

const (
	ChargingRateUnitW ChargingRateUnit = "W"
	ChargingRateUnitA ChargingRateUnit = "A"
)

// ChargingSchedulePeriod represents a period within a charging schedule.
type ChargingSchedulePeriod struct {
	StartPeriod  int     `json:"startPeriod" firestore:"startPeriod"`
	Limit        float64 `json:"limit" firestore:"limit"`
	NumberPhases *int    `json:"numberPhases,omitempty" firestore:"numberPhases,omitempty"`
}

// ChargingSchedule represents a charging schedule within a charging profile.
type ChargingSchedule struct {
	Duration               *int                     `json:"duration,omitempty" firestore:"duration,omitempty"`
	StartSchedule          *time.Time               `json:"startSchedule,omitempty" firestore:"startSchedule,omitempty"`
	ChargingRateUnit       ChargingRateUnit         `json:"chargingRateUnit" firestore:"chargingRateUnit"`
	ChargingSchedulePeriod []ChargingSchedulePeriod `json:"chargingSchedulePeriod" firestore:"chargingSchedulePeriod"`
	MinChargingRate        *float64                 `json:"minChargingRate,omitempty" firestore:"minChargingRate,omitempty"`
}

// ChargingProfile represents an OCPP 1.6 charging profile.
type ChargingProfile struct {
	ChargeStationId        string                 `json:"chargeStationId" firestore:"chargeStationId"`
	ConnectorId            int                    `json:"connectorId" firestore:"connectorId"`
	ChargingProfileId      int                    `json:"chargingProfileId" firestore:"chargingProfileId"`
	TransactionId          *int                   `json:"transactionId,omitempty" firestore:"transactionId,omitempty"`
	StackLevel             int                    `json:"stackLevel" firestore:"stackLevel"`
	ChargingProfilePurpose ChargingProfilePurpose `json:"chargingProfilePurpose" firestore:"chargingProfilePurpose"`
	ChargingProfileKind    ChargingProfileKind    `json:"chargingProfileKind" firestore:"chargingProfileKind"`
	RecurrencyKind         *RecurrencyKind        `json:"recurrencyKind,omitempty" firestore:"recurrencyKind,omitempty"`
	ValidFrom              *time.Time             `json:"validFrom,omitempty" firestore:"validFrom,omitempty"`
	ValidTo                *time.Time             `json:"validTo,omitempty" firestore:"validTo,omitempty"`
	ChargingSchedule       ChargingSchedule       `json:"chargingSchedule" firestore:"chargingSchedule"`
}

// ChargingProfileStore provides methods for managing charging profiles.
type ChargingProfileStore interface {
	// SetChargingProfile creates or updates a charging profile for a charge station connector.
	// If a profile with the same chargingProfileId already exists, it is replaced.
	SetChargingProfile(ctx context.Context, profile *ChargingProfile) error

	// GetChargingProfiles retrieves charging profiles matching the given filters.
	// All filter parameters are optional (nil means no filter).
	GetChargingProfiles(ctx context.Context, chargeStationId string, connectorId *int, purpose *ChargingProfilePurpose, stackLevel *int) ([]*ChargingProfile, error)

	// ClearChargingProfile removes charging profiles matching the given filters.
	// Returns the number of profiles removed.
	ClearChargingProfile(ctx context.Context, chargeStationId string, profileId *int, connectorId *int, purpose *ChargingProfilePurpose, stackLevel *int) (int, error)

	// GetCompositeSchedule calculates the composite charging schedule for a connector.
	GetCompositeSchedule(ctx context.Context, chargeStationId string, connectorId int, duration int, chargingRateUnit *ChargingRateUnit) (*ChargingSchedule, error)
}
