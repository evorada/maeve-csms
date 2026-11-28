// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	assetStationA = "k7f2.CP001"
	assetStationB = "k7f2.CP002"
	assetPartyA   = "party-a"
	assetPartyB   = "party-b"
)

// testAssetId is a fixed UUID per number; a higher number sorts after a lower one.
func testAssetId(n int) string {
	return fmt.Sprintf("0193b2c5-0000-7000-8000-%012d", n)
}

// testAssetLocation is a location of orgA without an id: the store generates it.
func testAssetLocation(name string) *store.AssetLocation {
	return &store.AssetLocation{
		OrgId:     orgA,
		PartyRef:  assetPartyA,
		Name:      ptr(name),
		Address:   "Calle Mayor 1",
		City:      "Madrid",
		Country:   "ESP",
		Latitude:  "40.416775",
		Longitude: "-3.703790",
		TimeZone:  "Europe/Madrid",
		Publish:   true,
	}
}

// testAssetConnector is an AC connector with the OCPP connector id.
func testAssetConnector(ocppConnectorId int) *store.AssetConnector {
	return &store.AssetConnector{
		OcppConnectorId: ocppConnectorId,
		Standard:        "IEC_62196_T2",
		Format:          "SOCKET",
		PowerType:       "AC_3_PHASE",
		MaxVoltage:      400,
		MaxAmperage:     32,
	}
}

// testAssetEvse is an EVSE of the station at the location, with connectors of the given
// OCPP connector ids.
func testAssetEvse(locationId, stationKey string, ocppEvseId int, ocppConnectorIds ...int) *store.AssetEvse {
	evse := &store.AssetEvse{
		OrgId:      orgA,
		LocationId: locationId,
		StationKey: stationKey,
		OcppEvseId: ocppEvseId,
	}
	for _, id := range ocppConnectorIds {
		evse.Connectors = append(evse.Connectors, testAssetConnector(id))
	}
	return evse
}

// registerAssetStations registers the charge stations that the EVSEs of the tests belong to.
func registerAssetStations(t *testing.T, s store.Engine) {
	t.Helper()
	for _, stationKey := range []string{assetStationA, assetStationB} {
		require.NoError(t, s.SetChargeStationAuth(context.Background(), stationKey,
			&store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: orgA}))
	}
}

func createAssetLocation(t *testing.T, s store.Engine, loc *store.AssetLocation) *store.AssetLocation {
	t.Helper()
	require.NoError(t, s.CreateAssetLocation(context.Background(), loc))
	return loc
}

func createAssetEvse(t *testing.T, s store.Engine, evse *store.AssetEvse) *store.AssetEvse {
	t.Helper()
	require.NoError(t, s.CreateAssetEvse(context.Background(), evse))
	return evse
}

func lookupAssetLocation(t *testing.T, s store.Engine, id string) *store.AssetLocation {
	t.Helper()
	loc, err := s.LookupAssetLocation(context.Background(), id)
	require.NoError(t, err)
	return loc
}

func lookupAssetEvse(t *testing.T, s store.Engine, id string) *store.AssetEvse {
	t.Helper()
	evse, err := s.LookupAssetEvse(context.Background(), id)
	require.NoError(t, err)
	return evse
}

func lookupAssetConnector(t *testing.T, s store.Engine, id string) *store.AssetConnector {
	t.Helper()
	c, err := s.LookupAssetConnector(context.Background(), id)
	require.NoError(t, err)
	return c
}

func assertUUIDv7(t *testing.T, id string) {
	t.Helper()
	u, err := uuid.Parse(id)
	if assert.NoError(t, err, id) {
		assert.Equal(t, uuid.Version(7), u.Version(), id)
		assert.Equal(t, u.String(), id, "canonical form")
	}
}

func evseUids(evses []*store.AssetEvse) []string {
	ids := make([]string, len(evses))
	for i, evse := range evses {
		ids[i] = evse.Id
	}
	return ids
}

func connectorIds(evse *store.AssetEvse) []int {
	ids := make([]int, len(evse.Connectors))
	for i, c := range evse.Connectors {
		ids[i] = c.OcppConnectorId
	}
	return ids
}

func locationNames(locations []*store.AssetLocation) []string {
	names := make([]string, len(locations))
	for i, loc := range locations {
		names[i] = *loc.Name
	}
	return names
}

// listAssetLocationNames lists the names of the matching locations (one page).
func listAssetLocationNames(t *testing.T, s store.Engine, filter store.AssetLocationFilter) []string {
	t.Helper()
	locations, _, err := s.ListAssetLocations(context.Background(), filter)
	require.NoError(t, err)
	return locationNames(locations)
}

// TestAssetStore is the contract of store.AssetStore. newStore returns an empty store.
func TestAssetStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("location round trip", func(t *testing.T) {
		s := newStore(t)
		loc := testAssetLocation("Plaza")
		loc.PostalCode = ptr("28013")
		loc.State = ptr("Madrid")
		loc.ParkingType = ptr("ON_STREET")
		loc.Facilities = []string{"CAFE", "WIFI"}
		loc.ChargingWhenClosed = ptr(false)
		loc.ExternalRefs = []string{"ext-1", "ext-2"}
		loc.OpeningTimes = &store.OpeningTimes{
			RegularHours: []store.RegularHours{
				{Weekday: 1, PeriodBegin: "08:00", PeriodEnd: "20:00"},
				{Weekday: 6, PeriodBegin: "10:00", PeriodEnd: "14:00"},
			},
			ExceptionalClosings: []store.ExceptionalPeriod{
				{PeriodBegin: time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC), PeriodEnd: time.Date(2026, 12, 26, 0, 0, 0, 0, time.UTC)},
			},
		}
		createAssetLocation(t, s, loc)

		assertUUIDv7(t, loc.Id)
		assert.Equal(t, loc.Id, loc.OcpiId, "the OCPI id defaults to the id")
		assert.Equal(t, int64(1), loc.Version)
		assert.False(t, loc.CreatedAt.IsZero())
		assert.Equal(t, loc.CreatedAt, loc.UpdatedAt)

		got := lookupAssetLocation(t, s, loc.Id)
		assert.Equal(t, loc, got)
		got = lookupAssetLocation(t, s, strings.ToUpper(loc.Id))
		assert.Equal(t, loc, got, "an id is a UUID in any case")
		byOcpiId, err := s.LookupAssetLocationByOcpiId(ctx, assetPartyA, loc.Id)
		require.NoError(t, err)
		assert.Equal(t, loc, byOcpiId)

		// the store keeps its own copy
		got.Facilities[0] = "HOTEL"
		got.OpeningTimes.RegularHours[0].Weekday = 7
		*got.Name = "changed"
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id))
	})

	t.Run("location with given id and OCPI id, without lists", func(t *testing.T) {
		s := newStore(t)
		loc := testAssetLocation("Given")
		loc.Id = strings.ToUpper(testAssetId(1))
		loc.OrgId = strings.ToUpper(orgA)
		loc.OcpiId = "LOC-1"
		loc.Publish = false
		createAssetLocation(t, s, loc)

		assert.Equal(t, testAssetId(1), loc.Id, "canonical form")
		assert.Equal(t, orgA, loc.OrgId, "canonical form")
		assert.Equal(t, "LOC-1", loc.OcpiId)
		assert.Equal(t, []string{}, loc.Facilities, "empty, not nil")
		assert.Equal(t, []string{}, loc.ExternalRefs, "empty, not nil")
		assert.Nil(t, loc.OpeningTimes)
		assert.Equal(t, loc, lookupAssetLocation(t, s, testAssetId(1)))
	})

	t.Run("lookups of what is not there", func(t *testing.T) {
		s := newStore(t)
		createAssetLocation(t, s, testAssetLocation("Plaza"))

		assert.Nil(t, lookupAssetLocation(t, s, testAssetId(99)))
		assert.Nil(t, lookupAssetLocation(t, s, "not-a-uuid"))
		loc, err := s.LookupAssetLocationByOcpiId(ctx, assetPartyB, "LOC-1")
		require.NoError(t, err)
		assert.Nil(t, loc)
		assert.Nil(t, lookupAssetEvse(t, s, testAssetId(99)))
		assert.Nil(t, lookupAssetEvse(t, s, "not-a-uuid"))
		evse, err := s.LookupAssetEvseByOcpp(ctx, assetStationA, 1)
		require.NoError(t, err)
		assert.Nil(t, evse)
		assert.Nil(t, lookupAssetConnector(t, s, testAssetId(99)))
		assert.Nil(t, lookupAssetConnector(t, s, "not-a-uuid"))
		c, err := s.LookupAssetConnectorByOcpp(ctx, assetStationA, 1, 1)
		require.NoError(t, err)
		assert.Nil(t, c)
		evses, err := s.ListAssetEvsesForLocation(ctx, testAssetId(99))
		require.NoError(t, err)
		assert.Empty(t, evses)
		evses, err = s.ListAssetEvsesForStation(ctx, assetStationA)
		require.NoError(t, err)
		assert.Empty(t, evses)
	})

	t.Run("EVSE with two connectors", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		createTariffWithId(t, s, testAssetId(50), orgA)
		createTariffWithId(t, s, testAssetId(51), orgA)
		evse := testAssetEvse(loc.Id, assetStationA, 1, 2, 1)
		evse.EvseId = ptr("ES*CCM*ECP001*1")
		evse.PhysicalReference = ptr("A1")
		evse.Connectors[0].OcpiId = "two"
		evse.Connectors[0].MaxElectricPower = ptr(int32(22000))
		evse.Connectors[0].TariffRef = ptr(strings.ToUpper(testAssetId(50)))
		evse.Connectors[0].AdhocTariffRef = ptr(testAssetId(51))
		evse.Connectors[0].OrgId = orgB // the org of a connector is the org of its EVSE
		createAssetEvse(t, s, evse)

		assertUUIDv7(t, evse.Id)
		assert.Equal(t, store.AssetEvseStatusActive, evse.Status, "status defaults to ACTIVE")
		assert.Equal(t, int64(1), evse.Version)
		assert.False(t, evse.CreatedAt.IsZero())
		require.Len(t, evse.Connectors, 2)
		for _, c := range evse.Connectors {
			assertUUIDv7(t, c.Id)
			assert.Equal(t, evse.Id, c.EvseUid)
			assert.Equal(t, orgA, c.OrgId)
			assert.Equal(t, int64(1), c.Version)
		}
		assert.Equal(t, "two", evse.Connectors[0].OcpiId)
		assert.Equal(t, "1", evse.Connectors[1].OcpiId, "the OCPI id defaults to the OCPP connector id")
		assert.Equal(t, ptr(testAssetId(50)), evse.Connectors[0].TariffRef, "canonical form")
		assert.Equal(t, ptr(int32(22000)), evse.Connectors[0].MaxElectricPower)

		got := lookupAssetEvse(t, s, evse.Id)
		require.NotNil(t, got)
		assert.Equal(t, []int{1, 2}, connectorIds(got), "ordered by OCPP connector id")
		want := *evse
		want.Connectors = []*store.AssetConnector{evse.Connectors[1], evse.Connectors[0]}
		assert.Equal(t, &want, got)
		byOcpp, err := s.LookupAssetEvseByOcpp(ctx, assetStationA, 1)
		require.NoError(t, err)
		assert.Equal(t, &want, byOcpp)

		assert.Equal(t, evse.Connectors[0], lookupAssetConnector(t, s, evse.Connectors[0].Id))
		c, err := s.LookupAssetConnectorByOcpp(ctx, assetStationA, 1, 2)
		require.NoError(t, err)
		assert.Equal(t, evse.Connectors[0], c)
		c, err = s.LookupAssetConnectorByOcpp(ctx, assetStationA, 1, 3)
		require.NoError(t, err)
		assert.Nil(t, c)
		c, err = s.LookupAssetConnectorByOcpp(ctx, assetStationB, 1, 1)
		require.NoError(t, err)
		assert.Nil(t, c)

		// the store keeps its own copy
		got.Connectors[0].Standard = "CHADEMO"
		*got.EvseId = "changed"
		assert.Equal(t, &want, lookupAssetEvse(t, s, evse.Id))
	})

	t.Run("EVSE lists", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		garage := createAssetLocation(t, s, testAssetLocation("Garage"))
		a2 := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationA, 2, 1))
		b1 := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationB, 1, 1))
		a1 := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationA, 1, 1))
		a3 := createAssetEvse(t, s, testAssetEvse(garage.Id, assetStationA, 3))

		evses, err := s.ListAssetEvsesForLocation(ctx, plaza.Id)
		require.NoError(t, err)
		assert.Equal(t, []string{a1.Id, b1.Id, a2.Id}, evseUids(evses), "by OCPP EVSE id, then station")
		assert.Equal(t, a1, evses[0], "with connectors")

		evses, err = s.ListAssetEvsesForStation(ctx, assetStationA)
		require.NoError(t, err)
		assert.Equal(t, []string{a1.Id, a2.Id, a3.Id}, evseUids(evses))
		assert.Equal(t, []*store.AssetConnector{}, evses[2].Connectors, "empty, not nil")
	})

	t.Run("records that a create refers to must exist", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))

		err := s.CreateAssetEvse(ctx, testAssetEvse(testAssetId(99), assetStationA, 1, 1))
		assert.ErrorIs(t, err, store.ErrNotFound, "unknown location")
		err = s.CreateAssetEvse(ctx, testAssetEvse(loc.Id, "k7f2.unknown", 1, 1))
		assert.ErrorIs(t, err, store.ErrNotFound, "unknown charge station")
		evses, err := s.ListAssetEvsesForLocation(ctx, loc.Id)
		require.NoError(t, err)
		assert.Empty(t, evses)

		c := testAssetConnector(1)
		c.EvseUid = testAssetId(99)
		assert.ErrorIs(t, s.CreateAssetConnector(ctx, c), store.ErrNotFound, "unknown EVSE")
		c.EvseUid = "not-a-uuid"
		assert.ErrorIs(t, s.CreateAssetConnector(ctx, c), store.ErrNotFound, "unknown EVSE")
	})

	t.Run("the tariffs that a connector refers to must exist", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		tariff := createTariffWithId(t, s, testAssetId(50), orgA)

		for name, set := range map[string]func(c *store.AssetConnector){
			"unknown tariff":        func(c *store.AssetConnector) { c.TariffRef = ptr(testAssetId(98)) },
			"unknown ad hoc tariff": func(c *store.AssetConnector) { c.AdhocTariffRef = ptr(testAssetId(98)) },
		} {
			evse := testAssetEvse(loc.Id, assetStationA, 1, 1, 2)
			set(evse.Connectors[1])
			assert.ErrorIs(t, s.CreateAssetEvse(ctx, evse), store.ErrNotFound, name)
			got, err := s.LookupAssetEvseByOcpp(ctx, assetStationA, 1)
			require.NoError(t, err)
			assert.Nil(t, got, "an EVSE with a connector that is refused is not stored: %s", name)
		}

		evse := createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 1, 1))
		for name, set := range map[string]func(c *store.AssetConnector){
			"unknown tariff":        func(c *store.AssetConnector) { c.TariffRef = ptr(testAssetId(98)) },
			"unknown ad hoc tariff": func(c *store.AssetConnector) { c.AdhocTariffRef = ptr(testAssetId(98)) },
		} {
			added := testAssetConnector(2)
			added.EvseUid = evse.Id
			set(added)
			assert.ErrorIs(t, s.CreateAssetConnector(ctx, added), store.ErrNotFound, name)

			changed := *evse.Connectors[0]
			set(&changed)
			assert.ErrorIs(t, s.UpdateAssetConnector(ctx, &changed), store.ErrNotFound, name)
		}
		assert.Equal(t, evse, lookupAssetEvse(t, s, evse.Id), "a refused connector changes nothing")

		// a deleted tariff is still there
		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))
		c := evse.Connectors[0]
		c.TariffRef = &tariff.Id
		c.AdhocTariffRef = &tariff.Id
		require.NoError(t, s.UpdateAssetConnector(ctx, c))
		assert.Equal(t, c, lookupAssetConnector(t, s, c.Id))
	})

	t.Run("the default tariff of a location", func(t *testing.T) {
		s := newStore(t)
		tariff := createTariffWithId(t, s, testAssetId(50), orgA)
		other := createTariffWithId(t, s, testAssetId(51), orgA)

		loc := testAssetLocation("Plaza")
		loc.DefaultTariffRef = ptr(strings.ToUpper(tariff.Id))
		createAssetLocation(t, s, loc)
		assert.Equal(t, &tariff.Id, loc.DefaultTariffRef, "canonical form")
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id))
		byOcpiId, err := s.LookupAssetLocationByOcpiId(ctx, assetPartyA, loc.OcpiId)
		require.NoError(t, err)
		assert.Equal(t, loc, byOcpiId)
		listed, _, err := s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA}})
		require.NoError(t, err)
		assert.Equal(t, []*store.AssetLocation{loc}, listed)

		// the store keeps its own copy
		got := lookupAssetLocation(t, s, loc.Id)
		*got.DefaultTariffRef = other.Id
		assert.Equal(t, &tariff.Id, lookupAssetLocation(t, s, loc.Id).DefaultTariffRef)

		loc.DefaultTariffRef = &other.Id
		require.NoError(t, s.UpdateAssetLocation(ctx, loc))
		assert.Equal(t, &other.Id, lookupAssetLocation(t, s, loc.Id).DefaultTariffRef)
		loc.DefaultTariffRef = nil
		require.NoError(t, s.UpdateAssetLocation(ctx, loc))
		assert.Nil(t, lookupAssetLocation(t, s, loc.Id).DefaultTariffRef)
		assert.Equal(t, int64(3), loc.Version)

		// a tariff that does not exist
		unknown := testAssetLocation("Unknown tariff")
		unknown.Id = testAssetId(60)
		unknown.OcpiId = "UNKNOWN"
		unknown.DefaultTariffRef = ptr(testAssetId(98))
		assert.ErrorIs(t, s.CreateAssetLocation(ctx, unknown), store.ErrNotFound)
		assert.Nil(t, lookupAssetLocation(t, s, testAssetId(60)), "a location that is refused is not stored")
		changed := *loc
		changed.DefaultTariffRef = ptr(testAssetId(98))
		assert.ErrorIs(t, s.UpdateAssetLocation(ctx, &changed), store.ErrNotFound)
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id), "a refused update changes nothing")
		notAnId := testAssetLocation("Not an id")
		notAnId.OcpiId = "NOT-AN-ID"
		notAnId.DefaultTariffRef = ptr("not-a-uuid")
		assert.Error(t, s.CreateAssetLocation(ctx, notAnId))

		// a deleted tariff is still there
		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))
		loc.DefaultTariffRef = &tariff.Id
		require.NoError(t, s.UpdateAssetLocation(ctx, loc))
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id))
	})

	t.Run("unique keys", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := testAssetLocation("Plaza")
		loc.OcpiId = "LOC-1"
		createAssetLocation(t, s, loc)

		dup := testAssetLocation("Other")
		dup.OcpiId = "LOC-1"
		assert.ErrorIs(t, s.CreateAssetLocation(ctx, dup), store.ErrAlreadyExists, "same party and OCPI id")
		otherParty := testAssetLocation("Other party")
		otherParty.OcpiId = "LOC-1"
		otherParty.PartyRef = assetPartyB
		assert.NoError(t, s.CreateAssetLocation(ctx, otherParty), "same OCPI id, other party")
		sameId := testAssetLocation("Same id")
		sameId.Id = loc.Id
		assert.ErrorIs(t, s.CreateAssetLocation(ctx, sameId), store.ErrAlreadyExists, "same id")

		evse := testAssetEvse(loc.Id, assetStationA, 1, 1)
		evse.EvseId = ptr("ES*CCM*ECP001*1")
		createAssetEvse(t, s, evse)
		dupEvseId := testAssetEvse(loc.Id, assetStationB, 1, 1)
		dupEvseId.EvseId = ptr("ES*CCM*ECP001*1")
		assert.ErrorIs(t, s.CreateAssetEvse(ctx, dupEvseId), store.ErrAlreadyExists, "same eMI3 id")
		assert.ErrorIs(t, s.CreateAssetEvse(ctx, testAssetEvse(loc.Id, assetStationA, 1, 1)), store.ErrAlreadyExists, "same station and OCPP EVSE id")
		// EVSEs without an eMI3 id do not clash
		createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 2, 1))
		createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 3, 1))

		c := testAssetConnector(1)
		c.EvseUid = evse.Id
		assert.ErrorIs(t, s.CreateAssetConnector(ctx, c), store.ErrAlreadyExists, "same OCPP connector id")
		c = testAssetConnector(2)
		c.EvseUid = evse.Id
		c.OcpiId = "1"
		assert.ErrorIs(t, s.CreateAssetConnector(ctx, c), store.ErrAlreadyExists, "same OCPI connector id")
		c.OcpiId = ""
		require.NoError(t, s.CreateAssetConnector(ctx, c))
		assert.Equal(t, "2", c.OcpiId)
		assert.Equal(t, orgA, c.OrgId)
		assert.Equal(t, []int{1, 2}, connectorIds(lookupAssetEvse(t, s, evse.Id)))

		// an EVSE whose connectors clash is not stored, nor are any of its connectors
		clash := testAssetEvse(loc.Id, assetStationB, 1, 1, 1)
		assert.ErrorIs(t, s.CreateAssetEvse(ctx, clash), store.ErrAlreadyExists)
		got, err := s.LookupAssetEvseByOcpp(ctx, assetStationB, 1)
		require.NoError(t, err)
		assert.Nil(t, got)
		clash = testAssetEvse(loc.Id, assetStationB, 1, 1, 2)
		clash.Connectors[1].OcpiId = "1"
		assert.ErrorIs(t, s.CreateAssetEvse(ctx, clash), store.ErrAlreadyExists)
		got, err = s.LookupAssetEvseByOcpp(ctx, assetStationB, 1)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("location update is optimistic", func(t *testing.T) {
		s := newStore(t)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		other := testAssetLocation("Other")
		other.OcpiId = "LOC-2"
		createAssetLocation(t, s, other)
		created := *loc

		loc.Name = ptr("Plaza Mayor")
		loc.OcpiId = "LOC-1"
		loc.Publish = false
		loc.Facilities = []string{"MALL"}
		loc.OrgId = orgB // not written by an update
		require.NoError(t, s.UpdateAssetLocation(ctx, loc))
		assert.Equal(t, int64(2), loc.Version)
		assert.Equal(t, orgA, loc.OrgId)
		assert.Equal(t, created.CreatedAt, loc.CreatedAt)
		assert.False(t, loc.UpdatedAt.Before(created.UpdatedAt))
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id))
		assert.Equal(t, ptr("Plaza Mayor"), loc.Name)

		stale := created
		stale.Name = ptr("Stale")
		assert.ErrorIs(t, s.UpdateAssetLocation(ctx, &stale), store.ErrVersionConflict)
		assert.Equal(t, loc, lookupAssetLocation(t, s, loc.Id))

		loc.OcpiId = "LOC-2"
		assert.ErrorIs(t, s.UpdateAssetLocation(ctx, loc), store.ErrAlreadyExists, "OCPI id of another location")
		loc.OcpiId = ""
		require.NoError(t, s.UpdateAssetLocation(ctx, loc))
		assert.Equal(t, loc.Id, loc.OcpiId, "the OCPI id defaults to the id")
		assert.Equal(t, int64(3), loc.Version)

		unknown := testAssetLocation("Unknown")
		unknown.Id = testAssetId(99)
		unknown.Version = 1
		assert.ErrorIs(t, s.UpdateAssetLocation(ctx, unknown), store.ErrNotFound)
	})

	t.Run("EVSE update is optimistic and keeps the connectors", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		garage := createAssetLocation(t, s, testAssetLocation("Garage"))
		evse := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationA, 1, 1, 2))
		taken := testAssetEvse(plaza.Id, assetStationA, 2, 1)
		taken.EvseId = ptr("ES*CCM*ECP001*2")
		createAssetEvse(t, s, taken)
		created := *evse

		update := *evse
		update.LocationId = garage.Id
		update.EvseId = ptr("ES*CCM*ECP001*1")
		update.Status = store.AssetEvseStatusPlanned
		update.Connectors = nil
		require.NoError(t, s.UpdateAssetEvse(ctx, &update))
		assert.Equal(t, int64(2), update.Version)
		assert.Equal(t, created.CreatedAt, update.CreatedAt)
		assert.Equal(t, []int{1, 2}, connectorIds(&update), "connectors are read back")
		got := lookupAssetEvse(t, s, evse.Id)
		assert.Equal(t, &update, got)
		assert.Equal(t, garage.Id, got.LocationId)
		assert.Equal(t, store.AssetEvseStatusPlanned, got.Status)

		assert.ErrorIs(t, s.UpdateAssetEvse(ctx, &created), store.ErrVersionConflict)
		update.EvseId = ptr("ES*CCM*ECP001*2")
		assert.ErrorIs(t, s.UpdateAssetEvse(ctx, &update), store.ErrAlreadyExists, "eMI3 id of another EVSE")
		update.EvseId = nil
		update.OcppEvseId = 2
		assert.ErrorIs(t, s.UpdateAssetEvse(ctx, &update), store.ErrAlreadyExists, "OCPP EVSE id of another EVSE")
		update.OcppEvseId = 1
		update.LocationId = testAssetId(99)
		assert.ErrorIs(t, s.UpdateAssetEvse(ctx, &update), store.ErrNotFound, "unknown location")
		update.LocationId = plaza.Id
		update.Status = ""
		require.NoError(t, s.UpdateAssetEvse(ctx, &update))
		assert.Equal(t, store.AssetEvseStatusActive, update.Status)
		assert.Equal(t, int64(3), update.Version)

		unknown := testAssetEvse(plaza.Id, assetStationB, 9)
		unknown.Id = testAssetId(99)
		unknown.Version = 1
		assert.ErrorIs(t, s.UpdateAssetEvse(ctx, unknown), store.ErrNotFound)
	})

	t.Run("connector update is optimistic", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		evse := createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 1, 1, 2))
		createTariffWithId(t, s, testAssetId(50), orgA)
		c := evse.Connectors[0]
		created := *c

		c.Standard = "IEC_62196_T2_COMBO"
		c.PowerType = "DC"
		c.MaxElectricPower = ptr(int32(50000))
		c.TariffRef = ptr(testAssetId(50))
		c.EvseUid = testAssetId(99) // not written by an update
		require.NoError(t, s.UpdateAssetConnector(ctx, c))
		assert.Equal(t, int64(2), c.Version)
		assert.Equal(t, evse.Id, c.EvseUid)
		assert.Equal(t, created.CreatedAt, c.CreatedAt)
		assert.Equal(t, c, lookupAssetConnector(t, s, c.Id))

		assert.ErrorIs(t, s.UpdateAssetConnector(ctx, &created), store.ErrVersionConflict)
		assert.Equal(t, c, lookupAssetConnector(t, s, c.Id))
		c.OcppConnectorId = 2
		assert.ErrorIs(t, s.UpdateAssetConnector(ctx, c), store.ErrAlreadyExists, "OCPP connector id of another connector")
		c.OcppConnectorId = 1
		c.OcpiId = "2"
		assert.ErrorIs(t, s.UpdateAssetConnector(ctx, c), store.ErrAlreadyExists, "OCPI id of another connector")

		unknown := testAssetConnector(9)
		unknown.Id = testAssetId(99)
		unknown.Version = 1
		assert.ErrorIs(t, s.UpdateAssetConnector(ctx, unknown), store.ErrNotFound)
	})

	t.Run("deletes", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		evse := createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 1, 1, 2))
		other := createAssetEvse(t, s, testAssetEvse(loc.Id, assetStationA, 2, 1))

		assert.ErrorIs(t, s.DeleteAssetLocation(ctx, loc.Id), store.ErrInUse)
		assert.NotNil(t, lookupAssetLocation(t, s, loc.Id))

		require.NoError(t, s.DeleteAssetConnector(ctx, other.Connectors[0].Id))
		assert.Nil(t, lookupAssetConnector(t, s, other.Connectors[0].Id))
		assert.Empty(t, lookupAssetEvse(t, s, other.Id).Connectors)

		require.NoError(t, s.DeleteAssetEvse(ctx, evse.Id))
		assert.Nil(t, lookupAssetEvse(t, s, evse.Id))
		for _, c := range evse.Connectors {
			assert.Nil(t, lookupAssetConnector(t, s, c.Id), "the connectors go with the EVSE")
		}
		assert.ErrorIs(t, s.DeleteAssetLocation(ctx, loc.Id), store.ErrInUse, "one EVSE is left")
		require.NoError(t, s.DeleteAssetEvse(ctx, other.Id))
		require.NoError(t, s.DeleteAssetLocation(ctx, loc.Id))
		assert.Nil(t, lookupAssetLocation(t, s, loc.Id))

		// deleting what is not there does nothing
		assert.NoError(t, s.DeleteAssetLocation(ctx, loc.Id))
		assert.NoError(t, s.DeleteAssetLocation(ctx, "not-a-uuid"))
		assert.NoError(t, s.DeleteAssetEvse(ctx, evse.Id))
		assert.NoError(t, s.DeleteAssetEvse(ctx, "not-a-uuid"))
		assert.NoError(t, s.DeleteAssetConnector(ctx, evse.Connectors[0].Id))
		assert.NoError(t, s.DeleteAssetConnector(ctx, "not-a-uuid"))
	})

	t.Run("list filters", func(t *testing.T) {
		s := newStore(t)
		add := func(n int, orgId, partyRef, name string, publish bool) {
			loc := testAssetLocation(name)
			loc.Id = testAssetId(n)
			loc.OrgId = orgId
			loc.PartyRef = partyRef
			loc.OcpiId = fmt.Sprintf("LOC-%d", n)
			loc.Publish = publish
			createAssetLocation(t, s, loc)
		}
		add(1, orgA, assetPartyA, "Plaza Mayor", true)
		add(2, orgA, assetPartyB, "Garage", false)
		add(3, orgB, assetPartyA, "Station Hall", true)
		add(4, orgC, assetPartyA, "Airport", true)
		hall := lookupAssetLocation(t, s, testAssetId(3))
		hall.Address = "Avenida 100%_Real"
		hall.City = "Sevilla"
		require.NoError(t, s.UpdateAssetLocation(ctx, hall))

		filter := func(orgIds ...string) store.AssetLocationFilter {
			return store.AssetLocationFilter{OrgIds: orgIds}
		}
		assert.Equal(t, []string{"Plaza Mayor", "Garage"}, listAssetLocationNames(t, s, filter(orgA)))
		assert.Equal(t, []string{"Station Hall"}, listAssetLocationNames(t, s, filter(strings.ToUpper(orgB))), "an org id is a UUID in any case")
		assert.Equal(t, []string{"Plaza Mayor", "Garage", "Station Hall"}, listAssetLocationNames(t, s, filter(orgB, orgA)))
		assert.Empty(t, listAssetLocationNames(t, s, filter()), "no org, no locations")
		_, _, err := s.ListAssetLocations(ctx, filter("not-a-uuid"))
		assert.Error(t, err)

		f := filter(orgA, orgB)
		f.PartyRef = ptr(assetPartyB)
		assert.Equal(t, []string{"Garage"}, listAssetLocationNames(t, s, f))
		f = filter(orgA, orgB)
		f.Publish = ptr(false)
		assert.Equal(t, []string{"Garage"}, listAssetLocationNames(t, s, f))
		f.Publish = ptr(true)
		assert.Equal(t, []string{"Plaza Mayor", "Station Hall"}, listAssetLocationNames(t, s, f))

		for query, want := range map[string][]string{
			"pLaZa":   {"Plaza Mayor"},           // name, other case
			"MADRID":  {"Plaza Mayor", "Garage"}, // city
			"sevilla": {"Station Hall"},          // city after an update
			"loc-2":   {"Garage"},                // OCPI id
			"100%_r":  {"Station Hall"},          // address; % and _ are plain characters
			"%":       {"Station Hall"},
			"_":       {"Station Hall"},
			"nowhere": {},
		} {
			f := filter(orgA, orgB)
			f.Query = ptr(query)
			assert.Equal(t, want, listAssetLocationNames(t, s, f), query)
		}
	})

	t.Run("list pages", func(t *testing.T) {
		s := newStore(t)
		const locations = 120
		for i := range locations {
			loc := testAssetLocation(fmt.Sprintf("Location %03d", i))
			if i%2 == 1 {
				loc.OrgId = orgB
			}
			createAssetLocation(t, s, loc)
		}
		createAssetLocation(t, s, func() *store.AssetLocation {
			loc := testAssetLocation("Other org")
			loc.OrgId = orgC
			return loc
		}())

		seen := map[string]bool{}
		var pageSizes []int
		var lastId string
		page := store.PageRequest{Limit: 50}
		for {
			items, next, err := s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA, orgB}, Page: page})
			require.NoError(t, err)
			pageSizes = append(pageSizes, len(items))
			for _, loc := range items {
				assert.False(t, seen[loc.Id], "listed twice: %s", loc.Id)
				assert.Greater(t, loc.Id, lastId, "ordered by id")
				seen[loc.Id] = true
				lastId = loc.Id
			}
			if next == "" {
				break
			}
			require.Less(t, len(pageSizes), 5, "too many pages")
			page.Cursor = next
		}
		assert.Equal(t, []int{50, 50, 20}, pageSizes)
		assert.Len(t, seen, locations)

		items, next, err := s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA, orgB}})
		require.NoError(t, err)
		assert.Len(t, items, store.DefaultPageLimit)
		assert.NotEmpty(t, next)
		items, next, err = s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA, orgB}, Page: store.PageRequest{Limit: 120}})
		require.NoError(t, err)
		assert.Len(t, items, 120)
		assert.Empty(t, next, "a page that ends exactly at the last location")

		_, _, err = s.ListAssetLocations(ctx, store.AssetLocationFilter{OrgIds: []string{orgA}, Page: store.PageRequest{Cursor: "not a cursor"}})
		assert.ErrorIs(t, err, store.ErrInvalidCursor)
	})

	t.Run("a charge station moves to a location with its EVSEs", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		garage := createAssetLocation(t, s, testAssetLocation("Garage"))
		first := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationA, 1, 1))
		second := createAssetEvse(t, s, testAssetEvse(garage.Id, assetStationA, 2, 1, 2))
		ofB := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationB, 1, 1))

		require.NoError(t, s.MoveChargeStationToLocation(ctx, assetStationA, garage.Id))

		cs, err := s.LookupChargeStation(ctx, assetStationA)
		require.NoError(t, err)
		require.NotNil(t, cs)
		assert.Equal(t, &garage.Id, cs.LocationId)
		moved := lookupAssetEvse(t, s, first.Id)
		assert.Equal(t, garage.Id, moved.LocationId)
		assert.Equal(t, int64(2), moved.Version, "a moved EVSE has a new version")
		assert.Equal(t, []int{1}, connectorIds(moved), "its connectors go with it")
		assert.Equal(t, second, lookupAssetEvse(t, s, second.Id), "an EVSE that is already there is left as it is")
		assert.Equal(t, ofB, lookupAssetEvse(t, s, ofB.Id), "the EVSEs of another station stay")
		assert.Equal(t, []string{ofB.Id}, evseUids(listAssetEvses(t, s, plaza.Id)))

		// again: nothing changes
		require.NoError(t, s.MoveChargeStationToLocation(ctx, assetStationA, garage.Id))
		assert.Equal(t, moved, lookupAssetEvse(t, s, first.Id))

		// the other station moves on its own
		require.NoError(t, s.MoveChargeStationToLocation(ctx, assetStationB, garage.Id))
		cs, err = s.LookupChargeStation(ctx, assetStationB)
		require.NoError(t, err)
		assert.Equal(t, &garage.Id, cs.LocationId)
		assert.Equal(t, garage.Id, lookupAssetEvse(t, s, ofB.Id).LocationId)

		assert.ErrorIs(t, s.MoveChargeStationToLocation(ctx, "k7f2.UNKNOWN", plaza.Id), store.ErrNotFound)
		assert.ErrorIs(t, s.MoveChargeStationToLocation(ctx, assetStationA, testAssetId(99)), store.ErrNotFound)
		assert.ErrorIs(t, s.MoveChargeStationToLocation(ctx, assetStationA, "not-a-uuid"), store.ErrNotFound)
		cs, err = s.LookupChargeStation(ctx, assetStationA)
		require.NoError(t, err)
		assert.Equal(t, &garage.Id, cs.LocationId, "a refused move changes nothing")
		assert.Equal(t, moved, lookupAssetEvse(t, s, first.Id))
	})

	t.Run("a decommissioned charge station has its EVSEs removed", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		plaza := createAssetLocation(t, s, testAssetLocation("Plaza"))
		active := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationA, 1, 1))
		planned := testAssetEvse(plaza.Id, assetStationA, 2, 1)
		planned.Status = store.AssetEvseStatusPlanned
		createAssetEvse(t, s, planned)
		removed := testAssetEvse(plaza.Id, assetStationA, 3, 1)
		removed.Status = store.AssetEvseStatusRemoved
		createAssetEvse(t, s, removed)
		ofB := createAssetEvse(t, s, testAssetEvse(plaza.Id, assetStationB, 1, 1))
		before, err := s.LookupChargeStation(ctx, assetStationA)
		require.NoError(t, err)

		require.NoError(t, s.DecommissionChargeStation(ctx, assetStationA, ptr("taken down")))

		cs, err := s.LookupChargeStation(ctx, assetStationA)
		require.NoError(t, err)
		require.NotNil(t, cs)
		assert.Equal(t, store.StationStateDecommissioned, cs.State)
		assert.Equal(t, ptr("taken down"), cs.StateReason)
		assert.False(t, cs.StateChangedAt.Before(before.StateChangedAt))
		assert.Equal(t, before.CreatedAt, cs.CreatedAt)
		for _, evse := range []*store.AssetEvse{active, planned} {
			got := lookupAssetEvse(t, s, evse.Id)
			assert.Equal(t, store.AssetEvseStatusRemoved, got.Status)
			assert.Equal(t, int64(2), got.Version)
			assert.Equal(t, plaza.Id, got.LocationId, "a removed EVSE stays at its location")
		}
		assert.Equal(t, removed, lookupAssetEvse(t, s, removed.Id), "an EVSE that was removed already is left as it is")
		assert.Equal(t, ofB, lookupAssetEvse(t, s, ofB.Id), "the EVSEs of another station stay")
		other, err := s.LookupChargeStation(ctx, assetStationB)
		require.NoError(t, err)
		assert.Equal(t, store.StationStateActive, other.State)

		// without a reason the reason is cleared
		require.NoError(t, s.DecommissionChargeStation(ctx, assetStationA, nil))
		cs, err = s.LookupChargeStation(ctx, assetStationA)
		require.NoError(t, err)
		assert.Equal(t, store.StationStateDecommissioned, cs.State)
		assert.Nil(t, cs.StateReason)
		assert.Equal(t, int64(2), lookupAssetEvse(t, s, active.Id).Version)

		assert.ErrorIs(t, s.DecommissionChargeStation(ctx, "k7f2.UNKNOWN", nil), store.ErrNotFound)
	})

	testStationTransfer(t, newStore)
}

func listAssetEvses(t *testing.T, s store.Engine, locationId string) []*store.AssetEvse {
	t.Helper()
	evses, err := s.ListAssetEvsesForLocation(context.Background(), locationId)
	require.NoError(t, err)
	return evses
}
