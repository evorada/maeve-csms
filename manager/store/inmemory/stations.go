// SPDX-License-Identifier: Apache-2.0

package inmemory

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The registry keeps the rules of the charge_stations table: a record for every station
// with auth details, created in state active (the default of the column), a location that
// must exist, and the capacity rounded to the three decimals of NUMERIC(10,3).

// registerChargeStation creates the registry record of a station whose auth details were
// set, or marks the record as updated. The caller holds the lock.
func (s *Store) registerChargeStation(stationKey string) {
	now := s.clock.Now().UTC()
	if registered := s.chargeStations[stationKey]; registered != nil {
		registered.UpdatedAt = now
		return
	}
	s.chargeStations[stationKey] = &store.ChargeStation{
		StationKey:     stationKey,
		State:          store.StationStateActive,
		StateChangedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func (s *Store) LookupChargeStation(_ context.Context, stationKey string) (*store.ChargeStation, error) {
	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return nil, nil
	}
	return s.chargeStationRecord(registered), nil
}

// chargeStationRecord returns a copy of the record with its org and runtime details. The
// caller holds the lock.
func (s *Store) chargeStationRecord(registered *store.ChargeStation) *store.ChargeStation {
	record := *registered
	record.OrgId = stationOrgId(s.orgIdForStation(registered.StationKey))
	record.LocationId = clonePtr(registered.LocationId)
	record.DisplayName = clonePtr(registered.DisplayName)
	record.MaxCapacityKw = clonePtr(registered.MaxCapacityKw)
	record.StateReason = clonePtr(registered.StateReason)
	record.Onboarding = copyOnboarding(registered.Onboarding)
	record.Runtime = copyRuntimeDetails(s.chargeStationRuntimeDetails[registered.StationKey])
	return &record
}

func (s *Store) UpdateChargeStation(_ context.Context, cs *store.ChargeStation) error {
	locationId, err := optionalAssetKey(cs.LocationId)
	if err != nil {
		// not a UUID: no location has this id
		return fmt.Errorf("update charge station %s: location %s: %w", cs.StationKey, *cs.LocationId, store.ErrNotFound)
	}
	var maxCapacityKw *float64
	if cs.MaxCapacityKw != nil {
		rounded := math.Round(*cs.MaxCapacityKw*1000) / 1000
		maxCapacityKw = &rounded
	}

	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[cs.StationKey]
	if registered == nil {
		return fmt.Errorf("update charge station %s: %w", cs.StationKey, store.ErrNotFound)
	}
	if locationId != nil && s.assetLocations[*locationId] == nil {
		return fmt.Errorf("update charge station %s: location %s: %w", cs.StationKey, *locationId, store.ErrNotFound)
	}
	registered.LocationId = locationId
	registered.DisplayName = clonePtr(cs.DisplayName)
	registered.MaxCapacityKw = maxCapacityKw
	registered.AllowAnyToken = cs.AllowAnyToken
	registered.UpdatedAt = s.clock.Now().UTC()
	cs.UpdatedAt = registered.UpdatedAt
	return nil
}

func (s *Store) SetChargeStationState(_ context.Context, stationKey string, state store.StationState, reason *string) error {
	if !state.Valid() {
		return fmt.Errorf("set state of charge station %s: unknown state %q", stationKey, state)
	}
	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return fmt.Errorf("set state of charge station %s: %w", stationKey, store.ErrNotFound)
	}
	now := s.clock.Now().UTC()
	registered.State = state
	registered.StateReason = clonePtr(reason)
	registered.StateChangedAt = now
	registered.UpdatedAt = now
	return nil
}

func (s *Store) ListChargeStations(_ context.Context, filter store.ChargeStationFilter) ([]*store.ChargeStation, string, error) {
	afterKey, err := store.DecodeKeyCursor(filter.Page.Cursor)
	if err != nil {
		return nil, "", fmt.Errorf("list charge stations: %w", err)
	}
	orgIds := make(map[string]bool, len(filter.OrgIds))
	for _, orgId := range filter.OrgIds {
		u, err := uuid.Parse(orgId)
		if err != nil {
			return nil, "", fmt.Errorf("list charge stations: invalid org id %q: %w", orgId, err)
		}
		orgIds[u.String()] = true
	}
	var locationId string
	if filter.LocationId != nil {
		var ok bool
		if locationId, ok = assetKey(*filter.LocationId); !ok {
			// not a UUID: no station is at a location with this id
			return []*store.ChargeStation{}, "", nil
		}
	}
	query := ""
	if filter.Query != nil {
		query = strings.ToLower(*filter.Query)
	}

	s.Lock()
	defer s.Unlock()
	var matching []*store.ChargeStation
	for stationKey, registered := range s.chargeStations {
		if stationKey <= afterKey || !orgIds[stationOrgId(s.orgIdForStation(stationKey))] {
			continue
		}
		record := s.chargeStationRecord(registered)
		if s.chargeStationMatches(record, filter, locationId, query) {
			matching = append(matching, record)
		}
	}
	slices.SortFunc(matching, func(a, b *store.ChargeStation) int { return strings.Compare(a.StationKey, b.StationKey) })

	limit := filter.Page.EffectiveLimit()
	nextCursor := ""
	if len(matching) > limit {
		matching = matching[:limit]
		nextCursor = store.EncodeKeyCursor(matching[limit-1].StationKey)
	}
	if matching == nil {
		matching = []*store.ChargeStation{}
	}
	return matching, nextCursor, nil
}

// chargeStationMatches reports whether the record passes the filter (but for the org and
// the page). The caller holds the lock.
func (s *Store) chargeStationMatches(record *store.ChargeStation, filter store.ChargeStationFilter, locationId, query string) bool {
	runtime := record.Runtime
	if runtime == nil {
		runtime = &store.ChargeStationRuntimeDetails{}
	}
	switch {
	case filter.LocationId != nil && (record.LocationId == nil || *record.LocationId != locationId),
		len(filter.States) > 0 && !slices.Contains(filter.States, record.State),
		filter.Vendor != nil && (runtime.Vendor == nil || !strings.EqualFold(*runtime.Vendor, *filter.Vendor)),
		filter.Model != nil && (runtime.Model == nil || !strings.EqualFold(*runtime.Model, *filter.Model)),
		filter.Connected != nil && s.chargeStationConnected(record.StationKey) != *filter.Connected,
		len(filter.StatusAnyOf) > 0 && !s.chargeStationHasConnectorStatus(record.StationKey, filter.StatusAnyOf),
		filter.Query != nil && !chargeStationContains(record, runtime, query):
		return false
	}
	return true
}

// chargeStationConnected reports whether the websocket of the station is open; a station
// without a status has never connected. The caller holds the lock.
func (s *Store) chargeStationConnected(stationKey string) bool {
	status := s.chargeStationStatuses[stationKey]
	return status != nil && status.Connected
}

// chargeStationHasConnectorStatus reports whether a connector of the station (connector
// 0 included) is in one of the statuses. The caller holds the lock.
func (s *Store) chargeStationHasConnectorStatus(stationKey string, statuses []store.ConnectorStatusType) bool {
	for _, status := range s.connectorStatuses[stationKey] {
		if slices.Contains(statuses, status.Status) {
			return true
		}
	}
	return false
}

// chargeStationContains reports whether the station key, the display name, the serial
// number or the ICCID of the station contains the lowercase query, ignoring case.
func chargeStationContains(record *store.ChargeStation, runtime *store.ChargeStationRuntimeDetails, query string) bool {
	fields := []string{record.StationKey}
	for _, field := range []*string{record.DisplayName, runtime.SerialNumber, runtime.Iccid} {
		if field != nil {
			fields = append(fields, *field)
		}
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func (s *Store) CountChargeStations(_ context.Context, orgId string, states ...store.StationState) (int, error) {
	u, err := uuid.Parse(orgId)
	if err != nil {
		return 0, fmt.Errorf("count charge stations: invalid org id %q: %w", orgId, err)
	}
	s.Lock()
	defer s.Unlock()
	count := 0
	for stationKey, registered := range s.chargeStations {
		if stationOrgId(s.orgIdForStation(stationKey)) != u.String() {
			continue
		}
		if len(states) == 0 || slices.Contains(states, registered.State) {
			count++
		}
	}
	return count, nil
}

func (s *Store) RecordOnboardingConnection(_ context.Context, stationKey, orgId, transport, credential string, maxPending int, at time.Time) (bool, error) {
	profile, ok := store.OnboardingSecurityProfile(transport)
	if !ok {
		return false, fmt.Errorf("record onboarding connection of %s: unknown transport %q", stationKey, transport)
	}
	if !store.ValidOnboardingCredential(credential) {
		return false, fmt.Errorf("record onboarding connection of %s: unknown credential %q", stationKey, credential)
	}
	org, err := uuid.Parse(orgId)
	if err != nil {
		return false, fmt.Errorf("record onboarding connection of %s: invalid org id %q: %w", stationKey, orgId, err)
	}
	at = at.UTC()

	s.Lock()
	defer s.Unlock()
	if registered := s.chargeStations[stationKey]; registered != nil {
		if registered.State != store.StationStatePending {
			return false, fmt.Errorf("record onboarding connection of %s: the station is %s: %w", stationKey, registered.State, store.ErrInvalidState)
		}
		if registered.Onboarding == nil {
			registered.Onboarding = &store.StationOnboarding{FirstSeenAt: at, Status: store.OnboardingAwaitingApproval}
		}
		registered.Onboarding.LastSeenAt = at
		registered.Onboarding.Transport = transport
		registered.Onboarding.Credential = credential
		registered.UpdatedAt = s.clock.Now().UTC()
		return false, nil
	}
	if s.chargeStationAuth[stationKey] != nil {
		// auth details always come with a record; a station that has them is registered
		return false, fmt.Errorf("record onboarding connection of %s: the station is registered: %w", stationKey, store.ErrInvalidState)
	}

	pending := 0
	for key, registered := range s.chargeStations {
		if registered.State == store.StationStatePending && stationOrgId(s.orgIdForStation(key)) == org.String() {
			pending++
		}
	}
	if pending >= maxPending {
		return false, fmt.Errorf("record onboarding connection of %s: org %s has %d pending stations: %w", stationKey, org, pending, store.ErrLimitExceeded)
	}

	now := s.clock.Now().UTC()
	s.chargeStationAuth[stationKey] = &store.ChargeStationAuth{SecurityProfile: profile, OrgId: org.String()}
	s.chargeStations[stationKey] = &store.ChargeStation{
		StationKey:     stationKey,
		State:          store.StationStatePending,
		StateChangedAt: now,
		Onboarding: &store.StationOnboarding{
			FirstSeenAt: at,
			LastSeenAt:  at,
			Transport:   transport,
			Credential:  credential,
			Status:      store.OnboardingAwaitingApproval,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	return true, nil
}

func (s *Store) RecordOnboardingBoot(_ context.Context, stationKey string, at time.Time) (*store.StationOnboarding, bool, error) {
	at = at.UTC()
	s.Lock()
	defer s.Unlock()
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return nil, false, fmt.Errorf("record onboarding boot of %s: %w", stationKey, store.ErrNotFound)
	}
	if registered.State != store.StationStatePending {
		return nil, false, fmt.Errorf("record onboarding boot of %s: the station is %s: %w", stationKey, registered.State, store.ErrInvalidState)
	}
	if registered.Onboarding == nil {
		registered.Onboarding = &store.StationOnboarding{FirstSeenAt: at, Status: store.OnboardingAwaitingApproval}
	}
	onboarding := registered.Onboarding
	onboarding.LastSeenAt = at
	notify := onboarding.NotifiedAt == nil
	if notify {
		onboarding.NotifiedAt = &at
	}
	return copyOnboarding(onboarding), notify, nil
}

// pendingStation returns the record of a pending station. A station that does not exist
// is ErrNotFound, one that is not pending ErrInvalidState. The caller holds the lock.
func (s *Store) pendingStation(stationKey string) (*store.ChargeStation, error) {
	registered := s.chargeStations[stationKey]
	if registered == nil {
		return nil, store.ErrNotFound
	}
	if registered.State != store.StationStatePending {
		return nil, fmt.Errorf("the station is %s: %w", registered.State, store.ErrInvalidState)
	}
	return registered, nil
}

// setPendingPassword sets the pending password hash of the auth details of the station
// ("" clears it). The auth details are handed out as they are stored, so they are
// replaced, not changed. The caller holds the lock.
func (s *Store) setPendingPassword(stationKey, hash string) {
	if current := s.chargeStationAuth[stationKey]; current != nil {
		updated := *current
		updated.PendingBase64SHA256Password = hash
		s.chargeStationAuth[stationKey] = &updated
	}
}

func (s *Store) SetOnboarding(_ context.Context, stationKey string, onboarding *store.StationOnboarding, pendingPasswordHash *string) error {
	if onboarding == nil {
		return fmt.Errorf("set onboarding of %s: no onboarding", stationKey)
	}
	s.Lock()
	defer s.Unlock()
	registered, err := s.pendingStation(stationKey)
	if err != nil {
		return fmt.Errorf("set onboarding of %s: %w", stationKey, err)
	}
	registered.Onboarding = copyOnboarding(onboarding)
	registered.UpdatedAt = s.clock.Now().UTC()
	hash := ""
	if pendingPasswordHash != nil {
		hash = *pendingPasswordHash
	}
	s.setPendingPassword(stationKey, hash)
	return nil
}

func (s *Store) CompleteApproval(_ context.Context, stationKey string, approval *store.PendingApproval, auth *store.ChargeStationAuth, sharedCredentials bool) error {
	fail := func(err error) error {
		return fmt.Errorf("complete the approval of %s: %w", stationKey, err)
	}
	if approval == nil || auth == nil {
		return fail(fmt.Errorf("no approval or no auth details"))
	}
	var maxCapacityKw *float64
	if approval.MaxCapacityKw != nil {
		rounded := math.Round(*approval.MaxCapacityKw*1000) / 1000
		maxCapacityKw = &rounded
	}

	s.Lock()
	defer s.Unlock()
	registered, err := s.pendingStation(stationKey)
	if err != nil {
		return fail(err)
	}
	locationId, ok := assetKey(approval.LocationId)
	if !ok || s.assetLocations[locationId] == nil {
		return fail(fmt.Errorf("location %s: %w", approval.LocationId, store.ErrNotFound))
	}

	// the EVSEs are created one by one and deleted again when one of them cannot be, so
	// that the approval is stored whole or not at all
	orgId := stationOrgId(s.orgIdForStation(stationKey))
	var created []string
	for _, evse := range approval.Evses {
		evse.OrgId, evse.LocationId, evse.StationKey = orgId, locationId, stationKey
		if err := s.createAssetEvse(evse); err != nil {
			for _, id := range created {
				s.deleteAssetEvse(id)
			}
			return fail(err)
		}
		created = append(created, evse.Id)
	}

	now := s.clock.Now().UTC()
	registered.LocationId = &locationId
	registered.DisplayName = clonePtr(approval.DisplayName)
	registered.MaxCapacityKw = maxCapacityKw
	registered.AllowAnyToken = approval.AllowAnyToken
	registered.State = store.StationStateActive
	registered.StateReason = nil
	registered.StateChangedAt = now
	registered.UpdatedAt = now
	registered.Onboarding = nil
	registered.SharedCredentials = sharedCredentials
	s.chargeStationAuth[stationKey] = &store.ChargeStationAuth{
		SecurityProfile:        auth.SecurityProfile,
		Base64SHA256Password:   auth.Base64SHA256Password,
		InvalidUsernameAllowed: auth.InvalidUsernameAllowed,
		OrgId:                  s.orgIdForStation(stationKey),
	}
	return nil
}

func (s *Store) RejectChargeStation(_ context.Context, stationKey string, reason string) error {
	s.Lock()
	defer s.Unlock()
	registered, err := s.pendingStation(stationKey)
	if err != nil {
		return fmt.Errorf("reject charge station %s: %w", stationKey, err)
	}
	now := s.clock.Now().UTC()
	stateReason := store.RejectedStateReason(reason)
	registered.State = store.StationStateDecommissioned
	registered.StateReason = &stateReason
	registered.StateChangedAt = now
	registered.UpdatedAt = now
	s.setPendingPassword(stationKey, "")
	return nil
}

// stationOrgId returns the canonical (lowercase) form of an org id that is a UUID, as
// PostgreSQL returns it, and any other org id as it is.
func stationOrgId(orgId string) string {
	if u, err := uuid.Parse(orgId); err == nil {
		return u.String()
	}
	return orgId
}

func copyOnboarding(onboarding *store.StationOnboarding) *store.StationOnboarding {
	if onboarding == nil {
		return nil
	}
	copied := *onboarding
	copied.NotifiedAt = clonePtr(onboarding.NotifiedAt)
	copied.Error = clonePtr(onboarding.Error)
	copied.CommandId = clonePtr(onboarding.CommandId)
	copied.Approval = copyPendingApproval(onboarding.Approval)
	return &copied
}

func copyPendingApproval(approval *store.PendingApproval) *store.PendingApproval {
	if approval == nil {
		return nil
	}
	copied := *approval
	copied.DisplayName = clonePtr(approval.DisplayName)
	copied.MaxCapacityKw = clonePtr(approval.MaxCapacityKw)
	if approval.Evses != nil {
		copied.Evses = make([]*store.AssetEvse, len(approval.Evses))
		for i, evse := range approval.Evses {
			copied.Evses[i] = copyAssetEvse(evse)
		}
	}
	return &copied
}

func copyRuntimeDetails(details *store.ChargeStationRuntimeDetails) *store.ChargeStationRuntimeDetails {
	if details == nil {
		return nil
	}
	copied := *details
	copied.FirmwareVersion = clonePtr(details.FirmwareVersion)
	copied.Model = clonePtr(details.Model)
	copied.Vendor = clonePtr(details.Vendor)
	copied.SerialNumber = clonePtr(details.SerialNumber)
	copied.Iccid = clonePtr(details.Iccid)
	copied.Imsi = clonePtr(details.Imsi)
	copied.MeterSerialNumber = clonePtr(details.MeterSerialNumber)
	copied.MeterType = clonePtr(details.MeterType)
	copied.LastBootAt = clonePtr(details.LastBootAt)
	return &copied
}

func clonePtr[T any](v *T) *T {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}
