// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"sort"

	"github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetChargingProfile(_ context.Context, profile *store.ChargingProfile) error {
	s.Lock()
	defer s.Unlock()
	s.chargingProfiles[profile.ChargingProfileId] = profile
	return nil
}

func (s *Store) GetChargingProfiles(_ context.Context, chargeStationId string, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) ([]*store.ChargingProfile, error) {
	s.Lock()
	defer s.Unlock()

	var result []*store.ChargingProfile
	for _, p := range s.chargingProfiles {
		if p.ChargeStationId != chargeStationId {
			continue
		}
		if connectorId != nil && p.ConnectorId != *connectorId {
			continue
		}
		if purpose != nil && p.ChargingProfilePurpose != *purpose {
			continue
		}
		if stackLevel != nil && p.StackLevel != *stackLevel {
			continue
		}
		result = append(result, p)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].StackLevel != result[j].StackLevel {
			return result[i].StackLevel < result[j].StackLevel
		}
		return result[i].ChargingProfileId < result[j].ChargingProfileId
	})

	if result == nil {
		result = make([]*store.ChargingProfile, 0)
	}
	return result, nil
}

func (s *Store) ClearChargingProfile(_ context.Context, chargeStationId string, profileId *int, connectorId *int, purpose *store.ChargingProfilePurpose, stackLevel *int) (int, error) {
	s.Lock()
	defer s.Unlock()

	count := 0
	for id, p := range s.chargingProfiles {
		if p.ChargeStationId != chargeStationId {
			continue
		}
		if profileId != nil && p.ChargingProfileId != *profileId {
			continue
		}
		if connectorId != nil && p.ConnectorId != *connectorId {
			continue
		}
		if purpose != nil && p.ChargingProfilePurpose != *purpose {
			continue
		}
		if stackLevel != nil && p.StackLevel != *stackLevel {
			continue
		}
		delete(s.chargingProfiles, id)
		count++
	}
	return count, nil
}

func (s *Store) GetCompositeSchedule(_ context.Context, chargeStationId string, connectorId int, duration int, chargingRateUnit *store.ChargingRateUnit) (*store.ChargingSchedule, error) {
	s.Lock()
	defer s.Unlock()

	// Collect applicable profiles for this connector (and connector 0 for defaults)
	var profiles []*store.ChargingProfile
	for _, p := range s.chargingProfiles {
		if p.ChargeStationId != chargeStationId {
			continue
		}
		if p.ConnectorId != connectorId && p.ConnectorId != 0 {
			continue
		}
		if chargingRateUnit != nil && p.ChargingSchedule.ChargingRateUnit != *chargingRateUnit {
			continue
		}
		profiles = append(profiles, p)
	}

	if len(profiles) == 0 {
		return nil, nil
	}

	// Sort by purpose priority: ChargePointMaxProfile > TxDefaultProfile > TxProfile
	// Then by stack level (higher = higher priority)
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

	// Use the highest priority profile's schedule as the composite
	// A full composite schedule calculation would merge periods from all profiles,
	// but for now we return the highest priority profile's schedule with the requested duration.
	best := profiles[0]
	now := s.clock.Now()

	rateUnit := best.ChargingSchedule.ChargingRateUnit
	if chargingRateUnit != nil {
		rateUnit = *chargingRateUnit
	}

	schedule := &store.ChargingSchedule{
		Duration:               &duration,
		StartSchedule:          &now,
		ChargingRateUnit:       rateUnit,
		ChargingSchedulePeriod: filterPeriodsForDuration(best.ChargingSchedule.ChargingSchedulePeriod, duration),
		MinChargingRate:        best.ChargingSchedule.MinChargingRate,
	}

	return schedule, nil
}

// filterPeriodsForDuration returns only the periods that fall within the given duration.
func filterPeriodsForDuration(periods []store.ChargingSchedulePeriod, duration int) []store.ChargingSchedulePeriod {
	var result []store.ChargingSchedulePeriod
	for _, p := range periods {
		if p.StartPeriod < duration {
			result = append(result, p)
		}
	}
	if result == nil {
		result = make([]store.ChargingSchedulePeriod, 0)
	}
	return result
}
