// SPDX-License-Identifier: Apache-2.0

package firestore

import (
	"context"
	"fmt"
	"sort"

	"github.com/thoughtworks/maeve-csms/manager/store"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func chargingProfileDocPath(chargeStationId string, profileId int) string {
	return fmt.Sprintf("ChargingProfile/%s-%d", chargeStationId, profileId)
}

func (s *Store) SetChargingProfile(ctx context.Context, profile *store.ChargingProfile) error {
	docRef := s.client.Doc(chargingProfileDocPath(profile.ChargeStationId, profile.ChargingProfileId))
	_, err := docRef.Set(ctx, profile)
	if err != nil {
		return fmt.Errorf("setting charging profile %d for %s: %w", profile.ChargingProfileId, profile.ChargeStationId, err)
	}
	return nil
}

func (s *Store) GetChargingProfiles(ctx context.Context, chargeStationId string, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) ([]*store.ChargingProfile, error) {
	query := s.client.Collection("ChargingProfile").Where("chargeStationId", "==", chargeStationId)

	if connectorId != nil {
		query = query.Where("connectorId", "==", *connectorId)
	}
	if purpose != nil {
		query = query.Where("chargingProfilePurpose", "==", string(*purpose))
	}
	if stackLevel != nil {
		query = query.Where("stackLevel", "==", *stackLevel)
	}

	iter := query.Documents(ctx)
	defer iter.Stop()

	var profiles []*store.ChargingProfile
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("iterating charging profiles: %w", err)
		}

		var profile store.ChargingProfile
		if err := doc.DataTo(&profile); err != nil {
			return nil, fmt.Errorf("mapping charging profile %s: %w", doc.Ref.ID, err)
		}
		profiles = append(profiles, &profile)
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].StackLevel != profiles[j].StackLevel {
			return profiles[i].StackLevel < profiles[j].StackLevel
		}
		return profiles[i].ChargingProfileId < profiles[j].ChargingProfileId
	})

	if profiles == nil {
		profiles = make([]*store.ChargingProfile, 0)
	}
	return profiles, nil
}

func (s *Store) ClearChargingProfile(ctx context.Context, chargeStationId string, profileId *int, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) (int, error) {
	// If profileId is specified, try to delete directly
	if profileId != nil {
		docRef := s.client.Doc(chargingProfileDocPath(chargeStationId, *profileId))
		snap, err := docRef.Get(ctx)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return 0, nil
			}
			return 0, fmt.Errorf("getting charging profile %d: %w", *profileId, err)
		}

		var profile store.ChargingProfile
		if err := snap.DataTo(&profile); err != nil {
			return 0, fmt.Errorf("mapping charging profile: %w", err)
		}

		// Check additional filters
		if connectorId != nil && profile.ConnectorId != *connectorId {
			return 0, nil
		}
		if purpose != nil && profile.ChargingProfilePurpose != *purpose {
			return 0, nil
		}
		if stackLevel != nil && profile.StackLevel != *stackLevel {
			return 0, nil
		}

		_, err = docRef.Delete(ctx)
		if err != nil {
			return 0, fmt.Errorf("deleting charging profile %d: %w", *profileId, err)
		}
		return 1, nil
	}

	// Otherwise query and delete matching profiles
	profiles, err := s.GetChargingProfiles(ctx, chargeStationId, connectorId, purpose, stackLevel)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, p := range profiles {
		docRef := s.client.Doc(chargingProfileDocPath(chargeStationId, p.ChargingProfileId))
		_, err := docRef.Delete(ctx)
		if err != nil {
			return count, fmt.Errorf("deleting charging profile %d: %w", p.ChargingProfileId, err)
		}
		count++
	}

	return count, nil
}

func (s *Store) GetCompositeSchedule(ctx context.Context, chargeStationId string, connectorId int, duration int, chargingRateUnit *store.ChargingRateUnit) (*store.ChargingSchedule, error) {
	// Get all profiles for this connector and connector 0 (defaults)
	var allProfiles []*store.ChargingProfile

	for _, cid := range []int{connectorId, 0} {
		profiles, err := s.GetChargingProfiles(ctx, chargeStationId, &cid, nil, nil)
		if err != nil {
			return nil, err
		}
		allProfiles = append(allProfiles, profiles...)
	}

	// Remove connector 0 duplicates if connectorId is already 0
	if connectorId == 0 {
		seen := make(map[int]bool)
		var deduped []*store.ChargingProfile
		for _, p := range allProfiles {
			if !seen[p.ChargingProfileId] {
				seen[p.ChargingProfileId] = true
				deduped = append(deduped, p)
			}
		}
		allProfiles = deduped
	}

	// Filter by charging rate unit if specified
	if chargingRateUnit != nil {
		var filtered []*store.ChargingProfile
		for _, p := range allProfiles {
			if p.ChargingSchedule.ChargingRateUnit == *chargingRateUnit {
				filtered = append(filtered, p)
			}
		}
		allProfiles = filtered
	}

	if len(allProfiles) == 0 {
		return nil, nil
	}

	// Sort by purpose priority then stack level
	purposePriority := map[store.ChargingProfilePurpose]int{
		store.ChargingProfilePurposeChargePointMaxProfile: 3,
		store.ChargingProfilePurposeTxDefaultProfile:      2,
		store.ChargingProfilePurposeTxProfile:             1,
	}

	sort.Slice(allProfiles, func(i, j int) bool {
		pi := purposePriority[allProfiles[i].ChargingProfilePurpose]
		pj := purposePriority[allProfiles[j].ChargingProfilePurpose]
		if pi != pj {
			return pi > pj
		}
		return allProfiles[i].StackLevel > allProfiles[j].StackLevel
	})

	best := allProfiles[0]
	now := s.clock.Now()

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
