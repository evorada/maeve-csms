// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// orgOnboardingLockClass is the first key of the advisory lock that an onboarding
// connection holds on its org until its transaction ends (the second key is a hash of
// the org id). The number has no meaning; it only has to differ from the first key of
// other two-key advisory locks in the database.
const orgOnboardingLockClass int32 = 1869573742

// onboardingDocument is how a store.StationOnboarding is kept in the onboarding column.
type onboardingDocument struct {
	FirstSeenAt time.Time  `json:"first_seen_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	Transport   string     `json:"transport"`
	Credential  string     `json:"credential"`
	Status      string     `json:"status"`
	NotifiedAt  *time.Time `json:"notified_at,omitempty"`
	Error       *string    `json:"error,omitempty"`
	CommandId   *string    `json:"command_id,omitempty"`
	// Approval is the approval that waits for the station to take its new password.
	Approval *approvalDocument `json:"approval,omitempty"`
}

// approvalDocument is how a store.PendingApproval is kept in the onboarding column.
type approvalDocument struct {
	LocationId      string                 `json:"location_id"`
	DisplayName     *string                `json:"display_name,omitempty"`
	AllowAnyToken   bool                   `json:"allow_any_token"`
	MaxCapacityKw   *float64               `json:"max_capacity_kw,omitempty"`
	SecurityProfile int                    `json:"security_profile"`
	Evses           []approvalEvseDocument `json:"evses"`
	RequestedBy     string                 `json:"requested_by"`
	RequestedAt     time.Time              `json:"requested_at"`
}

// approvalEvseDocument is an EVSE of an approval: the fields that its owner sets.
type approvalEvseDocument struct {
	OcppEvseId        int                         `json:"ocpp_evse_id"`
	EvseId            *string                     `json:"evse_id,omitempty"`
	PhysicalReference *string                     `json:"physical_reference,omitempty"`
	Status            string                      `json:"status,omitempty"`
	Connectors        []approvalConnectorDocument `json:"connectors"`
}

// approvalConnectorDocument is a connector of an EVSE of an approval: the fields that
// its owner sets.
type approvalConnectorDocument struct {
	OcppConnectorId  int     `json:"ocpp_connector_id"`
	OcpiId           string  `json:"ocpi_id,omitempty"`
	Standard         string  `json:"standard"`
	Format           string  `json:"format"`
	PowerType        string  `json:"power_type"`
	MaxVoltage       int32   `json:"max_voltage"`
	MaxAmperage      int32   `json:"max_amperage"`
	MaxElectricPower *int32  `json:"max_electric_power,omitempty"`
	TariffRef        *string `json:"tariff_ref,omitempty"`
	AdhocTariffRef   *string `json:"adhoc_tariff_ref,omitempty"`
}

func marshalOnboarding(onboarding *store.StationOnboarding) ([]byte, error) {
	return json.Marshal(onboardingDocument{
		FirstSeenAt: onboarding.FirstSeenAt.UTC(),
		LastSeenAt:  onboarding.LastSeenAt.UTC(),
		Transport:   onboarding.Transport,
		Credential:  onboarding.Credential,
		Status:      string(onboarding.Status),
		NotifiedAt:  onboarding.NotifiedAt,
		Error:       onboarding.Error,
		CommandId:   onboarding.CommandId,
		Approval:    approvalToDocument(onboarding.Approval),
	})
}

func approvalToDocument(approval *store.PendingApproval) *approvalDocument {
	if approval == nil {
		return nil
	}
	doc := &approvalDocument{
		LocationId:      approval.LocationId,
		DisplayName:     approval.DisplayName,
		AllowAnyToken:   approval.AllowAnyToken,
		MaxCapacityKw:   approval.MaxCapacityKw,
		SecurityProfile: int(approval.SecurityProfile),
		Evses:           make([]approvalEvseDocument, len(approval.Evses)),
		RequestedBy:     approval.RequestedBy,
		RequestedAt:     approval.RequestedAt.UTC(),
	}
	for i, evse := range approval.Evses {
		e := approvalEvseDocument{
			OcppEvseId:        evse.OcppEvseId,
			EvseId:            evse.EvseId,
			PhysicalReference: evse.PhysicalReference,
			Status:            string(evse.Status),
			Connectors:        make([]approvalConnectorDocument, len(evse.Connectors)),
		}
		for j, c := range evse.Connectors {
			e.Connectors[j] = approvalConnectorDocument{
				OcppConnectorId:  c.OcppConnectorId,
				OcpiId:           c.OcpiId,
				Standard:         c.Standard,
				Format:           c.Format,
				PowerType:        c.PowerType,
				MaxVoltage:       c.MaxVoltage,
				MaxAmperage:      c.MaxAmperage,
				MaxElectricPower: c.MaxElectricPower,
				TariffRef:        c.TariffRef,
				AdhocTariffRef:   c.AdhocTariffRef,
			}
		}
		doc.Evses[i] = e
	}
	return doc
}

func approvalFromDocument(doc *approvalDocument) (*store.PendingApproval, error) {
	if doc == nil {
		return nil, nil
	}
	value, err := safeIntToInt32(doc.SecurityProfile)
	if err != nil {
		return nil, fmt.Errorf("approval: security profile: %w", err)
	}
	profile, err := securityProfileFromInt32(value)
	if err != nil {
		return nil, fmt.Errorf("approval: %w", err)
	}
	approval := &store.PendingApproval{
		LocationId:      doc.LocationId,
		DisplayName:     doc.DisplayName,
		AllowAnyToken:   doc.AllowAnyToken,
		MaxCapacityKw:   doc.MaxCapacityKw,
		SecurityProfile: profile,
		Evses:           make([]*store.AssetEvse, len(doc.Evses)),
		RequestedBy:     doc.RequestedBy,
		RequestedAt:     doc.RequestedAt,
	}
	for i, e := range doc.Evses {
		evse := &store.AssetEvse{
			OcppEvseId:        e.OcppEvseId,
			EvseId:            e.EvseId,
			PhysicalReference: e.PhysicalReference,
			Status:            store.AssetEvseStatus(e.Status),
			Connectors:        make([]*store.AssetConnector, len(e.Connectors)),
		}
		for j, c := range e.Connectors {
			evse.Connectors[j] = &store.AssetConnector{
				OcppConnectorId:  c.OcppConnectorId,
				OcpiId:           c.OcpiId,
				Standard:         c.Standard,
				Format:           c.Format,
				PowerType:        c.PowerType,
				MaxVoltage:       c.MaxVoltage,
				MaxAmperage:      c.MaxAmperage,
				MaxElectricPower: c.MaxElectricPower,
				TariffRef:        c.TariffRef,
				AdhocTariffRef:   c.AdhocTariffRef,
			}
		}
		approval.Evses[i] = evse
	}
	return approval, nil
}

// unmarshalOnboarding reads the onboarding column; nil for NULL.
func unmarshalOnboarding(data []byte) (*store.StationOnboarding, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var doc onboardingDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("onboarding: %w", err)
	}
	approval, err := approvalFromDocument(doc.Approval)
	if err != nil {
		return nil, fmt.Errorf("onboarding: %w", err)
	}
	return &store.StationOnboarding{
		FirstSeenAt: doc.FirstSeenAt,
		LastSeenAt:  doc.LastSeenAt,
		Transport:   doc.Transport,
		Credential:  doc.Credential,
		Status:      store.OnboardingStatus(doc.Status),
		NotifiedAt:  doc.NotifiedAt,
		Error:       doc.Error,
		CommandId:   doc.CommandId,
		Approval:    approval,
	}, nil
}

func (s *Store) RecordOnboardingConnection(ctx context.Context, stationKey, orgId, transport, credential string, maxPending int, at time.Time) (bool, error) {
	fail := func(err error) (bool, error) {
		return false, fmt.Errorf("record onboarding connection of %s: %w", stationKey, err)
	}
	profile, ok := store.OnboardingSecurityProfile(transport)
	if !ok {
		return fail(fmt.Errorf("unknown transport %q", transport))
	}
	if !store.ValidOnboardingCredential(credential) {
		return fail(fmt.Errorf("unknown credential %q", credential))
	}
	pgOrgId, err := pgUUID(orgId)
	if err != nil {
		return fail(fmt.Errorf("invalid org id %q: %w", orgId, err))
	}
	at = at.UTC()

	tx, err := s.begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin transaction: %w", err))
	}
	defer rollback(ctx, tx)
	q := New(tx)

	if err := q.LockOrgOnboarding(ctx, LockOrgOnboardingParams{LockClass: orgOnboardingLockClass, OrgID: uuidString(pgOrgId)}); err != nil {
		return fail(err)
	}

	created := false
	row, err := q.GetChargeStationOnboardingForUpdate(ctx, stationKey)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		pending, err := q.CountChargeStationRecords(ctx, CountChargeStationRecordsParams{
			OrgID:  pgOrgId,
			States: []string{string(store.StationStatePending)},
		})
		if err != nil {
			return fail(err)
		}
		if pending >= int64(maxPending) {
			return fail(fmt.Errorf("org %s has %d pending stations: %w", orgId, pending, store.ErrLimitExceeded))
		}
		doc, err := marshalOnboarding(&store.StationOnboarding{
			FirstSeenAt: at,
			LastSeenAt:  at,
			Transport:   transport,
			Credential:  credential,
			Status:      store.OnboardingAwaitingApproval,
		})
		if err != nil {
			return fail(err)
		}
		rows, err := q.CreatePendingChargeStation(ctx, CreatePendingChargeStationParams{
			ChargeStationID: stationKey,
			SecurityProfile: int32(profile),
			OrgID:           pgOrgId,
			Onboarding:      doc,
		})
		if err != nil {
			return fail(err)
		}
		if rows == 0 {
			return fail(fmt.Errorf("the station was registered at the same time: %w", store.ErrInvalidState))
		}
		created = true
	case err != nil:
		return fail(err)
	default:
		if row.State != string(store.StationStatePending) {
			return fail(fmt.Errorf("the station is %s: %w", row.State, store.ErrInvalidState))
		}
		onboarding, err := unmarshalOnboarding(row.Onboarding)
		if err != nil {
			return fail(err)
		}
		if onboarding == nil {
			onboarding = &store.StationOnboarding{FirstSeenAt: at, Status: store.OnboardingAwaitingApproval}
		}
		onboarding.LastSeenAt = at
		onboarding.Transport = transport
		onboarding.Credential = credential
		if err := setOnboarding(ctx, q, stationKey, onboarding); err != nil {
			return fail(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return created, nil
}

func (s *Store) RecordOnboardingBoot(ctx context.Context, stationKey string, at time.Time) (*store.StationOnboarding, bool, error) {
	fail := func(err error) (*store.StationOnboarding, bool, error) {
		return nil, false, fmt.Errorf("record onboarding boot of %s: %w", stationKey, err)
	}
	at = at.UTC()

	tx, err := s.begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin transaction: %w", err))
	}
	defer rollback(ctx, tx)
	q := New(tx)

	// the row stays locked until the end of the transaction: of two boots at the same
	// time, the second sees the NotifiedAt of the first
	row, err := q.GetChargeStationOnboardingForUpdate(ctx, stationKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(store.ErrNotFound)
	}
	if err != nil {
		return fail(err)
	}
	if row.State != string(store.StationStatePending) {
		return fail(fmt.Errorf("the station is %s: %w", row.State, store.ErrInvalidState))
	}
	onboarding, err := unmarshalOnboarding(row.Onboarding)
	if err != nil {
		return fail(err)
	}
	if onboarding == nil {
		onboarding = &store.StationOnboarding{FirstSeenAt: at, Status: store.OnboardingAwaitingApproval}
	}
	onboarding.LastSeenAt = at
	notify := onboarding.NotifiedAt == nil
	if notify {
		onboarding.NotifiedAt = &at
	}
	if err := setOnboarding(ctx, q, stationKey, onboarding); err != nil {
		return fail(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return onboarding, notify, nil
}

func setOnboarding(ctx context.Context, q *Queries, stationKey string, onboarding *store.StationOnboarding) error {
	doc, err := marshalOnboarding(onboarding)
	if err != nil {
		return err
	}
	return q.SetChargeStationOnboarding(ctx, SetChargeStationOnboardingParams{
		Onboarding:      doc,
		ChargeStationID: stationKey,
	})
}

// lockPendingStation locks the row of a pending station until the end of the
// transaction of q and returns it. A station that does not exist is ErrNotFound, one
// that is not pending ErrInvalidState.
func lockPendingStation(ctx context.Context, q *Queries, stationKey string) (GetChargeStationOnboardingForUpdateRow, error) {
	row, err := q.GetChargeStationOnboardingForUpdate(ctx, stationKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, store.ErrNotFound
	}
	if err != nil {
		return row, err
	}
	if row.State != string(store.StationStatePending) {
		return row, fmt.Errorf("the station is %s: %w", row.State, store.ErrInvalidState)
	}
	return row, nil
}

func (s *Store) SetOnboarding(ctx context.Context, stationKey string, onboarding *store.StationOnboarding, pendingPasswordHash *string) error {
	fail := func(err error) error {
		return fmt.Errorf("set onboarding of %s: %w", stationKey, err)
	}
	if onboarding == nil {
		return fail(errors.New("no onboarding"))
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin transaction: %w", err))
	}
	defer rollback(ctx, tx)
	q := New(tx)

	if _, err := lockPendingStation(ctx, q, stationKey); err != nil {
		return fail(err)
	}
	if err := setOnboarding(ctx, q, stationKey, onboarding); err != nil {
		return fail(err)
	}
	err = q.SetChargeStationPendingPassword(ctx, SetChargeStationPendingPasswordParams{
		PendingBase64Sha256Password: toPgText(pendingPasswordHash),
		ChargeStationID:             stationKey,
	})
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return nil
}

// CompleteApproval runs in a transaction of its own, or in a savepoint inside the
// transaction of the caller; the EVSEs are created with CreateAssetEvse inside it.
func (s *Store) CompleteApproval(ctx context.Context, stationKey string, approval *store.PendingApproval, auth *store.ChargeStationAuth, sharedCredentials bool) error {
	fail := func(err error) error {
		return fmt.Errorf("complete the approval of %s: %w", stationKey, err)
	}
	if approval == nil || auth == nil {
		return fail(errors.New("no approval or no auth details"))
	}
	locationId, ok := assetId(approval.LocationId)
	if !ok {
		// not a UUID: no location has this id
		return fail(fmt.Errorf("location %s: %w", approval.LocationId, store.ErrNotFound))
	}
	maxCapacityKw := pgtype.Float8{}
	if approval.MaxCapacityKw != nil {
		maxCapacityKw = pgtype.Float8{Float64: *approval.MaxCapacityKw, Valid: true}
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin transaction: %w", err))
	}
	defer rollback(ctx, tx)
	q := New(tx)

	row, err := lockPendingStation(ctx, q, stationKey)
	if err != nil {
		return fail(err)
	}
	err = q.CompleteChargeStationApproval(ctx, CompleteChargeStationApprovalParams{
		LocationID:             locationId,
		DisplayName:            toPgText(approval.DisplayName),
		MaxCapacityKw:          maxCapacityKw,
		AllowAnyToken:          approval.AllowAnyToken,
		SecurityProfile:        int32(auth.SecurityProfile),
		Base64Sha256Password:   toNullableText(auth.Base64SHA256Password),
		InvalidUsernameAllowed: auth.InvalidUsernameAllowed,
		SharedCredentials:      sharedCredentials,
		ChargeStationID:        stationKey,
	})
	if err != nil {
		// a location that does not exist is a foreign key violation: ErrNotFound
		return fail(assetWriteError("approve charge station "+stationKey, err))
	}
	inTx := withTx(ctx, tx)
	for _, evse := range approval.Evses {
		evse.OrgId, evse.LocationId, evse.StationKey = uuidString(row.OrgID), approval.LocationId, stationKey
		if err := s.CreateAssetEvse(inTx, evse); err != nil {
			return fail(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return nil
}

func (s *Store) RejectChargeStation(ctx context.Context, stationKey string, reason string) error {
	fail := func(err error) error {
		return fmt.Errorf("reject charge station %s: %w", stationKey, err)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return fail(fmt.Errorf("begin transaction: %w", err))
	}
	defer rollback(ctx, tx)
	q := New(tx)

	if _, err := lockPendingStation(ctx, q, stationKey); err != nil {
		return fail(err)
	}
	err = q.RejectPendingChargeStation(ctx, RejectPendingChargeStationParams{
		StateReason:     store.RejectedStateReason(reason),
		ChargeStationID: stationKey,
	})
	if err != nil {
		return fail(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fail(fmt.Errorf("commit: %w", err))
	}
	return nil
}
