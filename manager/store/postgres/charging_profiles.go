// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetChargingProfile(ctx context.Context, profile *store.ChargingProfile) error {
	periodsJSON, err := json.Marshal(profile.ChargingSchedule.ChargingSchedulePeriod)
	if err != nil {
		return fmt.Errorf("marshaling schedule periods: %w", err)
	}

	var recurrencyKind pgtype.Text
	if profile.RecurrencyKind != nil {
		recurrencyKind = pgtype.Text{String: string(*profile.RecurrencyKind), Valid: true}
	}

	var transactionId pgtype.Int4
	if profile.TransactionId != nil {
		transactionId = pgtype.Int4{Int32: int32(*profile.TransactionId), Valid: true}
	}

	var validFrom, validTo pgtype.Timestamp
	if profile.ValidFrom != nil {
		validFrom = pgtype.Timestamp{Time: *profile.ValidFrom, Valid: true}
	}
	if profile.ValidTo != nil {
		validTo = pgtype.Timestamp{Time: *profile.ValidTo, Valid: true}
	}

	var duration pgtype.Int4
	if profile.ChargingSchedule.Duration != nil {
		duration = pgtype.Int4{Int32: int32(*profile.ChargingSchedule.Duration), Valid: true}
	}

	var startSchedule pgtype.Timestamp
	if profile.ChargingSchedule.StartSchedule != nil {
		startSchedule = pgtype.Timestamp{Time: *profile.ChargingSchedule.StartSchedule, Valid: true}
	}

	var minChargingRate pgtype.Float8
	if profile.ChargingSchedule.MinChargingRate != nil {
		minChargingRate = pgtype.Float8{Float64: *profile.ChargingSchedule.MinChargingRate, Valid: true}
	}

	params := UpsertChargingProfileParams{
		ChargeStationID:         profile.ChargeStationId,
		ConnectorID:             int32(profile.ConnectorId),
		ChargingProfileID:       int32(profile.ChargingProfileId),
		TransactionID:           transactionId,
		StackLevel:              int32(profile.StackLevel),
		ChargingProfilePurpose:  string(profile.ChargingProfilePurpose),
		ChargingProfileKind:     string(profile.ChargingProfileKind),
		RecurrencyKind:          recurrencyKind,
		ValidFrom:               validFrom,
		ValidTo:                 validTo,
		ChargingRateUnit:        string(profile.ChargingSchedule.ChargingRateUnit),
		Duration:                duration,
		StartSchedule:           startSchedule,
		MinChargingRate:         minChargingRate,
		ChargingSchedulePeriods: periodsJSON,
	}

	return s.writeQueries().UpsertChargingProfile(ctx, params)
}

func (s *Store) GetChargingProfiles(ctx context.Context, chargeStationId string, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) ([]*store.ChargingProfile, error) {
	rows, err := s.readQueries().GetChargingProfilesByStation(ctx, chargeStationId)
	if err != nil {
		return nil, fmt.Errorf("getting charging profiles: %w", err)
	}

	var result []*store.ChargingProfile
	for _, row := range rows {
		if connectorId != nil && int(row.ConnectorID) != *connectorId {
			continue
		}
		if purpose != nil && row.ChargingProfilePurpose != string(*purpose) {
			continue
		}
		if stackLevel != nil && int(row.StackLevel) != *stackLevel {
			continue
		}

		profile, err := toStoreChargingProfile(&row)
		if err != nil {
			return nil, err
		}
		result = append(result, profile)
	}

	if result == nil {
		result = make([]*store.ChargingProfile, 0)
	}
	return result, nil
}

func (s *Store) ClearChargingProfile(ctx context.Context, chargeStationId string, profileId *int, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) (int, error) {
	if profileId != nil {
		count, err := s.writeQueries().DeleteChargingProfileById(ctx, DeleteChargingProfileByIdParams{
			ChargeStationID:   chargeStationId,
			ChargingProfileID: int32(*profileId),
		})
		if err != nil {
			return 0, fmt.Errorf("deleting charging profile by id: %w", err)
		}
		return int(count), nil
	}

	// For complex filters, query first then delete matching
	if connectorId != nil || purpose != nil || stackLevel != nil {
		profiles, err := s.GetChargingProfiles(ctx, chargeStationId, connectorId, purpose, stackLevel)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, p := range profiles {
			n, err := s.writeQueries().DeleteChargingProfileById(ctx, DeleteChargingProfileByIdParams{
				ChargeStationID:   chargeStationId,
				ChargingProfileID: int32(p.ChargingProfileId),
			})
			if err != nil {
				return count, fmt.Errorf("deleting charging profile %d: %w", p.ChargingProfileId, err)
			}
			count += int(n)
		}
		return count, nil
	}

	// No filters, delete all for station
	count, err := s.writeQueries().DeleteChargingProfilesByStation(ctx, chargeStationId)
	if err != nil {
		return 0, fmt.Errorf("deleting all charging profiles: %w", err)
	}
	return int(count), nil
}

func (s *Store) GetCompositeSchedule(ctx context.Context, chargeStationId string, connectorId int, duration int, chargingRateUnit *store.ChargingRateUnit) (*store.ChargingSchedule, error) {
	// Get profiles for the specific connector and connector 0 (defaults)
	rows, err := s.readQueries().GetChargingProfilesByStation(ctx, chargeStationId)
	if err != nil {
		return nil, fmt.Errorf("getting charging profiles: %w", err)
	}

	var profiles []*store.ChargingProfile
	for _, row := range rows {
		if int(row.ConnectorID) != connectorId && int(row.ConnectorID) != 0 {
			continue
		}
		if chargingRateUnit != nil && row.ChargingRateUnit != string(*chargingRateUnit) {
			continue
		}
		profile, err := toStoreChargingProfile(&row)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}

	if len(profiles) == 0 {
		return nil, nil
	}

	// Sort by purpose priority then stack level
	purposePriority := map[store.ChargingProfilePurpose]int{
		store.ChargingProfilePurposeChargePointMaxProfile: 3,
		store.ChargingProfilePurposeTxDefaultProfile:      2,
		store.ChargingProfilePurposeTxProfile:             1,
	}

	sort.Slice(profiles, func(i, j int) bool {
		pi := purposePriority[profiles[i].ChargingProfilePurpose]
		pj := purposePriority[profiles[j].ChargingProfilePurpose]
		if pi != pj {
			return pi > pj
		}
		return profiles[i].StackLevel > profiles[j].StackLevel
	})

	best := profiles[0]
	now := time.Now()

	rateUnit := best.ChargingSchedule.ChargingRateUnit
	if chargingRateUnit != nil {
		rateUnit = *chargingRateUnit
	}

	var periods []store.ChargingSchedulePeriod
	for _, p := range best.ChargingSchedule.ChargingSchedulePeriod {
		if p.StartPeriod < duration {
			periods = append(periods, p)
		}
	}
	if periods == nil {
		periods = make([]store.ChargingSchedulePeriod, 0)
	}

	schedule := &store.ChargingSchedule{
		Duration:               &duration,
		StartSchedule:          &now,
		ChargingRateUnit:       rateUnit,
		ChargingSchedulePeriod: periods,
		MinChargingRate:        best.ChargingSchedule.MinChargingRate,
	}

	return schedule, nil
}

func toStoreChargingProfile(row *ChargingProfile) (*store.ChargingProfile, error) {
	var periods []store.ChargingSchedulePeriod
	if err := json.Unmarshal(row.ChargingSchedulePeriods, &periods); err != nil {
		return nil, fmt.Errorf("unmarshaling schedule periods: %w", err)
	}

	profile := &store.ChargingProfile{
		ChargeStationId:        row.ChargeStationID,
		ConnectorId:            int(row.ConnectorID),
		ChargingProfileId:      int(row.ChargingProfileID),
		StackLevel:             int(row.StackLevel),
		ChargingProfilePurpose: store.ChargingProfilePurpose(row.ChargingProfilePurpose),
		ChargingProfileKind:    store.ChargingProfileKind(row.ChargingProfileKind),
		ChargingSchedule: store.ChargingSchedule{
			ChargingRateUnit:       store.ChargingRateUnit(row.ChargingRateUnit),
			ChargingSchedulePeriod: periods,
		},
	}

	if row.TransactionID.Valid {
		txId := int(row.TransactionID.Int32)
		profile.TransactionId = &txId
	}
	if row.RecurrencyKind.Valid {
		rk := store.RecurrencyKind(row.RecurrencyKind.String)
		profile.RecurrencyKind = &rk
	}
	if row.ValidFrom.Valid {
		profile.ValidFrom = &row.ValidFrom.Time
	}
	if row.ValidTo.Valid {
		profile.ValidTo = &row.ValidTo.Time
	}
	if row.Duration.Valid {
		d := int(row.Duration.Int32)
		profile.ChargingSchedule.Duration = &d
	}
	if row.StartSchedule.Valid {
		profile.ChargingSchedule.StartSchedule = &row.StartSchedule.Time
	}
	if row.MinChargingRate.Valid {
		profile.ChargingSchedule.MinChargingRate = &row.MinChargingRate.Float64
	}

	return profile, nil
}
