// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/thoughtworks/maeve-csms/manager/money"
)

// TariffType is the kind of a tariff (OCPI TariffType).
type TariffType string

const (
	TariffTypeAdHocPayment TariffType = "AD_HOC_PAYMENT"
	TariffTypeProfileCheap TariffType = "PROFILE_CHEAP"
	TariffTypeProfileFast  TariffType = "PROFILE_FAST"
	TariffTypeProfileGreen TariffType = "PROFILE_GREEN"
	TariffTypeRegular      TariffType = "REGULAR"
)

// PriceComponentType is what a price component charges for (OCPI TariffDimensionType).
type PriceComponentType string

const (
	PriceComponentEnergy      PriceComponentType = "ENERGY"
	PriceComponentFlat        PriceComponentType = "FLAT"
	PriceComponentParkingTime PriceComponentType = "PARKING_TIME"
	PriceComponentTime        PriceComponentType = "TIME"
)

// DayOfWeek is a day of the week (OCPI DayOfWeek).
type DayOfWeek string

const (
	Monday    DayOfWeek = "MONDAY"
	Tuesday   DayOfWeek = "TUESDAY"
	Wednesday DayOfWeek = "WEDNESDAY"
	Thursday  DayOfWeek = "THURSDAY"
	Friday    DayOfWeek = "FRIDAY"
	Saturday  DayOfWeek = "SATURDAY"
	Sunday    DayOfWeek = "SUNDAY"
)

// ReservationRestriction says that a tariff element is for a reservation (OCPI
// ReservationRestrictionType).
type ReservationRestriction string

const (
	ReservationRestrictionReservation        ReservationRestriction = "RESERVATION"
	ReservationRestrictionReservationExpires ReservationRestriction = "RESERVATION_EXPIRES"
)

// PriceComponent is one price of a tariff element (OCPI PriceComponent), with two
// extensions for the time dimensions: a grace period and a price per step.
type PriceComponent struct {
	Type PriceComponentType `json:"type"`
	// Price is without VAT: per kWh (ENERGY), per hour (TIME, PARKING_TIME) or per
	// session (FLAT).
	Price money.Amount `json:"price"`
	// Vat is the VAT percentage of the price; nil when no VAT applies.
	Vat *money.Percent `json:"vat,omitempty"`
	// StepSize is the smallest amount that is billed: Wh (ENERGY) or seconds (TIME,
	// PARKING_TIME). It has no meaning for FLAT.
	StepSize int32 `json:"step_size"`
	// GracePeriodS is an extension for TIME and PARKING_TIME: the first seconds of the
	// dimension that are free.
	GracePeriodS *int32 `json:"grace_period_s,omitempty"`
	// PricePerStep is an extension for TIME and PARKING_TIME: the price of one step. When
	// it is set, the time is billed as steps times PricePerStep, and Price is the price
	// per hour that follows from it.
	PricePerStep *money.Amount `json:"price_per_step,omitempty"`
}

// TariffRestrictions say when a tariff element applies (OCPI TariffRestrictions). Times
// and dates are local to the location.
type TariffRestrictions struct {
	StartTime   *string                 `json:"start_time,omitempty"` // "HH:MM", inclusive
	EndTime     *string                 `json:"end_time,omitempty"`   // "HH:MM", exclusive; before start_time it is on the next day
	StartDate   *string                 `json:"start_date,omitempty"` // "YYYY-MM-DD", inclusive
	EndDate     *string                 `json:"end_date,omitempty"`   // "YYYY-MM-DD", exclusive
	MinKwh      *float64                `json:"min_kwh,omitempty"`    // inclusive
	MaxKwh      *float64                `json:"max_kwh,omitempty"`    // exclusive
	MinCurrent  *float64                `json:"min_current,omitempty"`
	MaxCurrent  *float64                `json:"max_current,omitempty"`
	MinPower    *float64                `json:"min_power,omitempty"` // kW
	MaxPower    *float64                `json:"max_power,omitempty"`
	MinDuration *int32                  `json:"min_duration,omitempty"` // seconds since the start of the session
	MaxDuration *int32                  `json:"max_duration,omitempty"`
	DayOfWeek   []DayOfWeek             `json:"day_of_week,omitempty"`
	Reservation *ReservationRestriction `json:"reservation,omitempty"`
}

// TariffElement is a group of price components that applies when its restrictions hold
// (OCPI TariffElement).
type TariffElement struct {
	PriceComponents []PriceComponent    `json:"price_components"`
	Restrictions    *TariffRestrictions `json:"restrictions,omitempty"`
}

// DisplayText is a text in a language (OCPI DisplayText).
type DisplayText struct {
	Language string `json:"language"`
	Text     string `json:"text"`
}

// IdleDetection holds the settings of a tariff for telling when a session stops charging
// and starts to park: the session is idle below the higher of the two powers. It is an
// extension; a nil field takes the default of the manager.
type IdleDetection struct {
	// ChargingPowerShare is a share of the highest power of the session, above 0 and at
	// most 1.
	ChargingPowerShare *float64 `json:"charging_power_share,omitempty"`
	// MinChargingPowerW is a power in W.
	MinChargingPowerW *float64 `json:"min_charging_power_w,omitempty"`
}

// Tariff is a tariff of an org, published through OCPI by one of the parties of the org.
// What a session is charged is in the versions of the tariff.
type Tariff struct {
	Id       string // UUIDv7, generated by the store when empty
	OrgId    string
	PartyRef string // id of a party of the tenant (Tenant.Parties[].Id)
	OcpiId   string // OCPI tariff id (CiString(36)), unique per party; the store sets it to Id when empty
	Name     string // a label for the owner; it can change and is not part of a version
	// CurrentVersion is the number of the newest version: 1 for a new tariff.
	CurrentVersion int
	// Current is the newest version. Every read of a tariff fills it.
	Current *TariffVersion
	// DeletedAt is when the tariff was deleted; nil for a tariff that is in use.
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TariffVersion is the content of a tariff (OCPI Tariff) at one version. A stored version
// never changes. AltText and Elements are read back as empty slices, never nil.
type TariffVersion struct {
	TariffId      string          `json:"tariff_id,omitempty"`
	Version       int             `json:"version,omitempty"`
	Currency      string          `json:"currency"` // ISO 4217
	Type          *TariffType     `json:"type,omitempty"`
	AltText       []DisplayText   `json:"tariff_alt_text,omitempty"`
	AltUrl        *string         `json:"tariff_alt_url,omitempty"`
	MinPrice      *money.Price    `json:"min_price,omitempty"`
	MaxPrice      *money.Price    `json:"max_price,omitempty"`
	StartDateTime *time.Time      `json:"start_date_time,omitempty"`
	EndDateTime   *time.Time      `json:"end_date_time,omitempty"`
	Elements      []TariffElement `json:"elements"`
	// EnergyMix is an OCPI EnergyMix document that the store keeps as it is given. It
	// may be read back with another layout (order of the keys, white space).
	EnergyMix     json.RawMessage `json:"energy_mix,omitempty"`
	IdleDetection *IdleDetection  `json:"idle_detection,omitempty"`
	CreatedAt     time.Time       `json:"created_at,omitzero"`
	CreatedBy     string          `json:"created_by,omitempty"` // subject of the principal that made the version
}

// TariffFilter selects the tariffs that ListTariffs returns.
type TariffFilter struct {
	// OrgIds are the orgs whose tariffs are listed (the principal's org, or its subtree,
	// resolved by the caller). No org lists nothing.
	OrgIds   []string
	PartyRef *string
	// Type is the type of the current version.
	Type *TariffType
	// Query is a case-insensitive substring of the name or the OCPI id.
	Query *string
	// IncludeDeleted also lists the deleted tariffs.
	IncludeDeleted bool
	Page           PageRequest
}

// TariffStore holds the tariffs of the orgs and their versions. A tariff changes by
// getting a new version; the versions that it had stay as they were, so that a session
// that started under a version can still be rated with it.
//
// Lookups return nil, nil when there is no such record; an id that is not a UUID names
// no record. A deleted tariff is still looked up (with DeletedAt set), and its versions
// stay readable, but ListTariffs leaves it out unless the filter asks for it, and it
// takes no new version and no new name: those return an error wrapping ErrNotFound, as
// they do for a tariff that does not exist.
//
// The store does not check the content of a version: the caller validates it first.
type TariffStore interface {
	// CreateTariff stores the tariff with v as its version 1. It fills in what the store
	// sets: the Id (when empty), the OcpiId (when empty), CurrentVersion, Current and
	// the timestamps of t, and TariffId, Version and CreatedAt of v. It returns
	// ErrAlreadyExists when the party already has a tariff with the OCPI id, deleted or
	// not.
	CreateTariff(ctx context.Context, t *Tariff, v *TariffVersion) error
	// AddTariffVersion stores v as version expectedCurrent+1 of the tariff and makes it
	// the current one, when the current version of the tariff is expectedCurrent;
	// otherwise it returns ErrVersionConflict. It fills in TariffId, Version and
	// CreatedAt of v.
	AddTariffVersion(ctx context.Context, tariffId string, expectedCurrent int, v *TariffVersion) error
	RenameTariff(ctx context.Context, tariffId, name string) error
	LookupTariff(ctx context.Context, tariffId string) (*Tariff, error)
	LookupTariffByOcpiId(ctx context.Context, partyRef, ocpiId string) (*Tariff, error)
	LookupTariffVersion(ctx context.Context, tariffId string, version int) (*TariffVersion, error)
	// ListTariffs returns a page ordered by id and the cursor of the next page ("" after
	// the last page).
	ListTariffs(ctx context.Context, filter TariffFilter) ([]*Tariff, string, error)
	// ListTariffVersions returns every version of the tariff, the newest first; none for
	// a tariff that does not exist.
	ListTariffVersions(ctx context.Context, tariffId string) ([]*TariffVersion, error)
	// DeleteTariff marks the tariff as deleted; the tariff and its versions stay stored.
	// It returns ErrInUse while a connector refers to the tariff. Deleting a tariff that
	// does not exist, or that is deleted already, does nothing.
	DeleteTariff(ctx context.Context, tariffId string) error
}
