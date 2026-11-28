// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	})
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
	return &store.StationOnboarding{
		FirstSeenAt: doc.FirstSeenAt,
		LastSeenAt:  doc.LastSeenAt,
		Transport:   doc.Transport,
		Credential:  doc.Credential,
		Status:      store.OnboardingStatus(doc.Status),
		NotifiedAt:  doc.NotifiedAt,
		Error:       doc.Error,
		CommandId:   doc.CommandId,
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
