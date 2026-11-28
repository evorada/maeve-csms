// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// testTariff is a tariff of orgA without an id: the store generates it.
func testTariff(name string) *store.Tariff {
	return &store.Tariff{OrgId: orgA, PartyRef: assetPartyA, Name: name}
}

// testTariffVersion is a version with one price: energy at the price per kWh (in 1/10000),
// with 21% VAT.
func testTariffVersion(pricePerKwh money.Amount) *store.TariffVersion {
	return &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: pricePerKwh, Vat: ptr(money.Percent(2100)), StepSize: 1},
		}}},
		CreatedBy: "user:alice",
	}
}

// fullTariffVersion is a version with every field set.
func fullTariffVersion() *store.TariffVersion {
	vat := ptr(money.Percent(2100))
	return &store.TariffVersion{
		Currency: "EUR",
		Type:     ptr(store.TariffTypeRegular),
		AltText: []store.DisplayText{
			{Language: "en", Text: "0.30 per kWh; parking 0.50 per 15 minutes after the first 30 minutes"},
			{Language: "es", Text: "0,30 por kWh; aparcamiento 0,50 cada 15 minutos tras los primeros 30 minutos"},
		},
		AltUrl:        ptr("https://example.com/tariffs/standard"),
		MinPrice:      &money.Price{ExclVat: 5000, InclVat: 6050},
		MaxPrice:      &money.Price{ExclVat: 500000, InclVat: 605000},
		StartDateTime: ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		EndDateTime:   ptr(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)),
		Elements: []store.TariffElement{
			{PriceComponents: []store.PriceComponent{
				{Type: store.PriceComponentFlat, Price: 10000, Vat: vat, StepSize: 1},
				{Type: store.PriceComponentEnergy, Price: 3000, Vat: vat, StepSize: 1},
				{Type: store.PriceComponentTime, Price: 12000, Vat: ptr(money.Percent(1050)), StepSize: 300,
					GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(1000))},
			}},
			{
				PriceComponents: []store.PriceComponent{
					{Type: store.PriceComponentParkingTime, Price: 20000, StepSize: 900,
						GracePeriodS: ptr(int32(1800)), PricePerStep: ptr(money.Amount(5000))},
				},
				Restrictions: &store.TariffRestrictions{
					StartTime:   ptr("08:00"),
					EndTime:     ptr("20:00"),
					StartDate:   ptr("2026-01-01"),
					EndDate:     ptr("2026-12-31"),
					MinKwh:      ptr(1.5),
					MaxKwh:      ptr(80.0),
					MinCurrent:  ptr(6.0),
					MaxCurrent:  ptr(32.0),
					MinPower:    ptr(3.7),
					MaxPower:    ptr(22.0),
					MinDuration: ptr(int32(600)),
					MaxDuration: ptr(int32(86400)),
					DayOfWeek:   []store.DayOfWeek{store.Monday, store.Tuesday, store.Wednesday, store.Thursday, store.Friday},
					Reservation: ptr(store.ReservationRestrictionReservation),
				},
			},
		},
		EnergyMix:     json.RawMessage(`{"is_green_energy": true, "supplier_name": "Green Power", "energy_product_name": "Eco"}`),
		IdleDetection: &store.IdleDetection{ChargingPowerShare: ptr(0.05), MinChargingPowerW: ptr(150.0)},
		CreatedBy:     "user:alice",
	}
}

// createTariff creates the tariff with the version and returns the tariff.
func createTariff(t *testing.T, s store.Engine, tariff *store.Tariff, v *store.TariffVersion) *store.Tariff {
	t.Helper()
	require.NoError(t, s.CreateTariff(context.Background(), tariff, v))
	return tariff
}

// createTariffWithId creates a tariff of the org with the id, for the tests of the
// records that refer to a tariff.
func createTariffWithId(t *testing.T, s store.Engine, id, orgId string) *store.Tariff {
	t.Helper()
	tariff := testTariff("Tariff " + id)
	tariff.Id = id
	tariff.OrgId = orgId
	return createTariff(t, s, tariff, testTariffVersion(3000))
}

func lookupTariff(t *testing.T, s store.Engine, id string) *store.Tariff {
	t.Helper()
	tariff, err := s.LookupTariff(context.Background(), id)
	require.NoError(t, err)
	return tariff
}

func lookupTariffVersion(t *testing.T, s store.Engine, id string, version int) *store.TariffVersion {
	t.Helper()
	v, err := s.LookupTariffVersion(context.Background(), id, version)
	require.NoError(t, err)
	return v
}

func listTariffVersions(t *testing.T, s store.Engine, id string) []*store.TariffVersion {
	t.Helper()
	versions, err := s.ListTariffVersions(context.Background(), id)
	require.NoError(t, err)
	return versions
}

// withoutEnergyMix returns a copy of the version without its energy mix, which a store
// may read back with another layout.
func withoutEnergyMix(v *store.TariffVersion) *store.TariffVersion {
	if v == nil {
		return nil
	}
	c := *v
	c.EnergyMix = nil
	return &c
}

// assertTariffVersion checks that got is the version want; the energy mix is compared as
// a JSON document.
func assertTariffVersion(t *testing.T, want, got *store.TariffVersion, msgAndArgs ...any) {
	t.Helper()
	if !assert.Equal(t, withoutEnergyMix(want), withoutEnergyMix(got), msgAndArgs...) || want == nil {
		return
	}
	if len(want.EnergyMix) == 0 || len(got.EnergyMix) == 0 {
		assert.Equal(t, string(want.EnergyMix), string(got.EnergyMix), msgAndArgs...)
		return
	}
	assert.JSONEq(t, string(want.EnergyMix), string(got.EnergyMix), msgAndArgs...)
}

// assertTariff checks that got is the tariff want, with its current version.
func assertTariff(t *testing.T, want, got *store.Tariff, msgAndArgs ...any) {
	t.Helper()
	if want == nil || got == nil {
		assert.Equal(t, want, got, msgAndArgs...)
		return
	}
	wantTariff, gotTariff := *want, *got
	wantTariff.Current, gotTariff.Current = nil, nil
	assert.Equal(t, wantTariff, gotTariff, msgAndArgs...)
	assertTariffVersion(t, want.Current, got.Current, msgAndArgs...)
}

func tariffNames(tariffs []*store.Tariff) []string {
	names := make([]string, len(tariffs))
	for i, tariff := range tariffs {
		names[i] = tariff.Name
	}
	return names
}

// listTariffNames lists the names of the matching tariffs (one page).
func listTariffNames(t *testing.T, s store.Engine, filter store.TariffFilter) []string {
	t.Helper()
	tariffs, _, err := s.ListTariffs(context.Background(), filter)
	require.NoError(t, err)
	return tariffNames(tariffs)
}

// TestTariffStore is the contract of store.TariffStore. newStore returns an empty store.
func TestTariffStore(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("tariff round trip with every field of a version", func(t *testing.T) {
		s := newStore(t)
		tariff := testTariff("Standard")
		v := fullTariffVersion()
		createTariff(t, s, tariff, v)

		assertUUIDv7(t, tariff.Id)
		assert.Equal(t, tariff.Id, tariff.OcpiId, "the OCPI id defaults to the id")
		assert.Equal(t, 1, tariff.CurrentVersion)
		assert.Nil(t, tariff.DeletedAt)
		assert.False(t, tariff.CreatedAt.IsZero())
		assert.Equal(t, tariff.CreatedAt, tariff.UpdatedAt)
		assert.Equal(t, tariff.Id, v.TariffId)
		assert.Equal(t, 1, v.Version)
		assert.False(t, v.CreatedAt.IsZero())
		assertTariffVersion(t, v, tariff.Current)

		// what was stored is what was given
		want := fullTariffVersion()
		want.TariffId, want.Version, want.CreatedAt = tariff.Id, 1, v.CreatedAt
		assertTariffVersion(t, want, v)

		got := lookupTariff(t, s, tariff.Id)
		assertTariff(t, tariff, got)
		assertTariff(t, tariff, lookupTariff(t, s, strings.ToUpper(tariff.Id)), "an id is a UUID in any case")
		byOcpiId, err := s.LookupTariffByOcpiId(ctx, assetPartyA, tariff.Id)
		require.NoError(t, err)
		assertTariff(t, tariff, byOcpiId)
		assertTariffVersion(t, v, lookupTariffVersion(t, s, tariff.Id, 1))
		versions := listTariffVersions(t, s, tariff.Id)
		require.Len(t, versions, 1)
		assertTariffVersion(t, v, versions[0])

		// the JSON of a version says all of it
		document, err := json.Marshal(got.Current)
		require.NoError(t, err)
		fromJSON := &store.TariffVersion{}
		require.NoError(t, json.Unmarshal(document, fromJSON))
		assertTariffVersion(t, got.Current, fromJSON)

		// the store keeps its own copy
		got.Name = "changed"
		got.Current.Currency = "USD"
		got.Current.Elements[0].PriceComponents[1].Price = 9999
		*got.Current.Elements[0].PriceComponents[2].GracePeriodS = 1
		*got.Current.Elements[1].Restrictions.StartTime = "00:00"
		got.Current.Elements[1].Restrictions.DayOfWeek[0] = store.Sunday
		got.Current.AltText[0].Text = "changed"
		got.Current.MinPrice.ExclVat = 1
		*got.Current.IdleDetection.ChargingPowerShare = 1
		v.Elements[0].PriceComponents[0].Price = 1
		assertTariffVersion(t, want, lookupTariffVersion(t, s, tariff.Id, 1))
		assertTariffVersion(t, want, lookupTariff(t, s, tariff.Id).Current)
		assert.Equal(t, "Standard", lookupTariff(t, s, tariff.Id).Name)
	})

	t.Run("tariff with given id and OCPI id, a version without the optional fields", func(t *testing.T) {
		s := newStore(t)
		tariff := testTariff("Given")
		tariff.Id = strings.ToUpper(testAssetId(1))
		tariff.OrgId = strings.ToUpper(orgA)
		tariff.OcpiId = "TARIFF-1"
		v := &store.TariffVersion{
			Currency:  "GBP",
			Elements:  []store.TariffElement{{PriceComponents: []store.PriceComponent{{Type: store.PriceComponentEnergy, StepSize: 1}}}},
			CreatedBy: "svc:importer",
		}
		createTariff(t, s, tariff, v)

		assert.Equal(t, testAssetId(1), tariff.Id, "canonical form")
		assert.Equal(t, orgA, tariff.OrgId, "canonical form")
		assert.Equal(t, "TARIFF-1", tariff.OcpiId)
		assert.Equal(t, testAssetId(1), v.TariffId)
		assert.Equal(t, []store.DisplayText{}, v.AltText, "empty, not nil")
		assert.Nil(t, v.Type)
		assert.Nil(t, v.AltUrl)
		assert.Nil(t, v.MinPrice)
		assert.Nil(t, v.MaxPrice)
		assert.Nil(t, v.StartDateTime)
		assert.Nil(t, v.EndDateTime)
		assert.Empty(t, v.EnergyMix)
		assert.Nil(t, v.IdleDetection)
		component := v.Elements[0].PriceComponents[0]
		assert.Equal(t, store.PriceComponent{Type: store.PriceComponentEnergy, StepSize: 1}, component, "a price of 0 and no VAT")
		assert.Nil(t, v.Elements[0].Restrictions)

		got := lookupTariff(t, s, testAssetId(1))
		assertTariff(t, tariff, got)
		byOcpiId, err := s.LookupTariffByOcpiId(ctx, assetPartyA, "TARIFF-1")
		require.NoError(t, err)
		assertTariff(t, tariff, byOcpiId)
	})

	t.Run("times of a version are read back in UTC", func(t *testing.T) {
		s := newStore(t)
		madrid, err := time.LoadLocation("Europe/Madrid")
		require.NoError(t, err)
		v := testTariffVersion(3000)
		v.StartDateTime = ptr(time.Date(2026, 3, 1, 0, 0, 0, 0, madrid))
		v.EndDateTime = ptr(time.Date(2026, 10, 1, 0, 0, 0, 0, madrid))
		tariff := createTariff(t, s, testTariff("Summer"), v)

		assert.Equal(t, ptr(time.Date(2026, 2, 28, 23, 0, 0, 0, time.UTC)), v.StartDateTime)
		assert.Equal(t, ptr(time.Date(2026, 9, 30, 22, 0, 0, 0, time.UTC)), v.EndDateTime)
		assertTariffVersion(t, v, lookupTariff(t, s, tariff.Id).Current)
	})

	t.Run("lookups of what is not there", func(t *testing.T) {
		s := newStore(t)
		tariff := createTariff(t, s, testTariff("Standard"), testTariffVersion(3000))

		assert.Nil(t, lookupTariff(t, s, testAssetId(99)))
		assert.Nil(t, lookupTariff(t, s, "not-a-uuid"))
		got, err := s.LookupTariffByOcpiId(ctx, assetPartyB, tariff.OcpiId)
		require.NoError(t, err)
		assert.Nil(t, got, "the OCPI id of a tariff of another party")
		got, err = s.LookupTariffByOcpiId(ctx, assetPartyA, "TARIFF-9")
		require.NoError(t, err)
		assert.Nil(t, got)

		assert.Nil(t, lookupTariffVersion(t, s, tariff.Id, 2))
		assert.Nil(t, lookupTariffVersion(t, s, tariff.Id, 0))
		assert.Nil(t, lookupTariffVersion(t, s, tariff.Id, -1))
		assert.Nil(t, lookupTariffVersion(t, s, tariff.Id, 1<<40))
		assert.Nil(t, lookupTariffVersion(t, s, testAssetId(99), 1))
		assert.Nil(t, lookupTariffVersion(t, s, "not-a-uuid", 1))
		assert.Empty(t, listTariffVersions(t, s, testAssetId(99)))
		assert.Empty(t, listTariffVersions(t, s, "not-a-uuid"))
	})

	t.Run("unique keys", func(t *testing.T) {
		s := newStore(t)
		tariff := testTariff("Standard")
		tariff.OcpiId = "TARIFF-1"
		createTariff(t, s, tariff, testTariffVersion(3000))

		dup := testTariff("Other")
		dup.Id = testAssetId(2)
		dup.OcpiId = "TARIFF-1"
		assert.ErrorIs(t, s.CreateTariff(ctx, dup, testTariffVersion(3500)), store.ErrAlreadyExists, "same party and OCPI id")
		assert.Nil(t, lookupTariff(t, s, testAssetId(2)), "a tariff that is refused is not stored")
		assert.Empty(t, listTariffVersions(t, s, testAssetId(2)), "nor is its version")

		otherParty := testTariff("Other party")
		otherParty.OcpiId = "TARIFF-1"
		otherParty.PartyRef = assetPartyB
		assert.NoError(t, s.CreateTariff(ctx, otherParty, testTariffVersion(3000)), "same OCPI id, other party")

		sameId := testTariff("Same id")
		sameId.Id = tariff.Id
		assert.ErrorIs(t, s.CreateTariff(ctx, sameId, testTariffVersion(3000)), store.ErrAlreadyExists, "same id")
		assert.Equal(t, "Standard", lookupTariff(t, s, tariff.Id).Name)
		assert.Len(t, listTariffVersions(t, s, tariff.Id), 1)

		// a deleted tariff keeps its OCPI id
		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))
		again := testTariff("Again")
		again.OcpiId = "TARIFF-1"
		assert.ErrorIs(t, s.CreateTariff(ctx, again, testTariffVersion(3000)), store.ErrAlreadyExists, "OCPI id of a deleted tariff")

		noOrg := testTariff("No org")
		noOrg.OrgId = "not-a-uuid"
		assert.Error(t, s.CreateTariff(ctx, noOrg, testTariffVersion(3000)))
		badId := testTariff("Bad id")
		badId.Id = "not-a-uuid"
		assert.Error(t, s.CreateTariff(ctx, badId, testTariffVersion(3000)))
	})

	t.Run("a new version needs the current version and leaves the others as they are", func(t *testing.T) {
		s := newStore(t)
		first := fullTariffVersion()
		tariff := createTariff(t, s, testTariff("Standard"), first)
		created := *tariff

		second := testTariffVersion(3500)
		second.Type = ptr(store.TariffTypeAdHocPayment)
		second.CreatedBy = "user:bob"
		require.NoError(t, s.AddTariffVersion(ctx, strings.ToUpper(tariff.Id), 1, second))
		assert.Equal(t, tariff.Id, second.TariffId)
		assert.Equal(t, 2, second.Version)
		assert.False(t, second.CreatedAt.Before(first.CreatedAt))
		assert.Equal(t, "user:bob", second.CreatedBy)
		assert.Equal(t, []store.DisplayText{}, second.AltText, "empty, not nil")

		got := lookupTariff(t, s, tariff.Id)
		assert.Equal(t, 2, got.CurrentVersion)
		assertTariffVersion(t, second, got.Current)
		assert.Equal(t, created.CreatedAt, got.CreatedAt)
		assert.False(t, got.UpdatedAt.Before(created.UpdatedAt))
		assert.Equal(t, "Standard", got.Name)

		// the first version is as it was
		assertTariffVersion(t, first, lookupTariffVersion(t, s, tariff.Id, 1))
		assertTariffVersion(t, second, lookupTariffVersion(t, s, tariff.Id, 2))
		versions := listTariffVersions(t, s, tariff.Id)
		require.Len(t, versions, 2)
		assertTariffVersion(t, second, versions[0], "the newest first")
		assertTariffVersion(t, first, versions[1])

		// a caller that saw another version than the current one is refused
		for _, expected := range []int{1, 3, 0, -1, 1 << 40} {
			stale := testTariffVersion(9900)
			err := s.AddTariffVersion(ctx, tariff.Id, expected, stale)
			assert.ErrorIs(t, err, store.ErrVersionConflict, "expected version %d", expected)
			assert.Zero(t, stale.Version, "a version that is refused is not filled in")
		}
		assertTariff(t, got, lookupTariff(t, s, tariff.Id), "a refused version changes nothing")
		assert.Len(t, listTariffVersions(t, s, tariff.Id), 2)

		third := testTariffVersion(4000)
		require.NoError(t, s.AddTariffVersion(ctx, tariff.Id, 2, third))
		assert.Equal(t, 3, third.Version)
		assert.Equal(t, 3, lookupTariff(t, s, tariff.Id).CurrentVersion)
		assertTariffVersion(t, first, lookupTariffVersion(t, s, tariff.Id, 1))
		assertTariffVersion(t, second, lookupTariffVersion(t, s, tariff.Id, 2))

		// the store keeps its own copy of a version
		second.Elements[0].PriceComponents[0].Price = 1
		read := lookupTariffVersion(t, s, tariff.Id, 2)
		assert.Equal(t, money.Amount(3500), read.Elements[0].PriceComponents[0].Price)
		read.Elements[0].PriceComponents[0].Price = 2
		assert.Equal(t, money.Amount(3500), lookupTariffVersion(t, s, tariff.Id, 2).Elements[0].PriceComponents[0].Price)

		assert.ErrorIs(t, s.AddTariffVersion(ctx, testAssetId(99), 1, testTariffVersion(3000)), store.ErrNotFound)
		assert.ErrorIs(t, s.AddTariffVersion(ctx, "not-a-uuid", 1, testTariffVersion(3000)), store.ErrNotFound)
	})

	t.Run("a new name is not a new version", func(t *testing.T) {
		s := newStore(t)
		v := testTariffVersion(3000)
		tariff := createTariff(t, s, testTariff("Standard"), v)

		require.NoError(t, s.RenameTariff(ctx, tariff.Id, "Standard 2027"))
		got := lookupTariff(t, s, tariff.Id)
		assert.Equal(t, "Standard 2027", got.Name)
		assert.Equal(t, 1, got.CurrentVersion)
		assertTariffVersion(t, v, got.Current)
		assert.Equal(t, tariff.CreatedAt, got.CreatedAt)
		assert.False(t, got.UpdatedAt.Before(tariff.UpdatedAt))
		assert.Len(t, listTariffVersions(t, s, tariff.Id), 1)

		assert.ErrorIs(t, s.RenameTariff(ctx, testAssetId(99), "Unknown"), store.ErrNotFound)
		assert.ErrorIs(t, s.RenameTariff(ctx, "not-a-uuid", "Unknown"), store.ErrNotFound)
	})

	t.Run("a tariff that a connector refers to is not deleted", func(t *testing.T) {
		s := newStore(t)
		registerAssetStations(t, s)
		loc := createAssetLocation(t, s, testAssetLocation("Plaza"))
		regular := createTariff(t, s, testTariff("Regular"), testTariffVersion(3000))
		adhoc := createTariff(t, s, testTariff("Ad hoc"), testTariffVersion(4500))
		unused := createTariff(t, s, testTariff("Unused"), testTariffVersion(2500))
		evse := testAssetEvse(loc.Id, assetStationA, 1, 1, 2)
		evse.Connectors[0].TariffRef = &regular.Id
		evse.Connectors[1].AdhocTariffRef = &adhoc.Id
		createAssetEvse(t, s, evse)

		assert.ErrorIs(t, s.DeleteTariff(ctx, regular.Id), store.ErrInUse, "the tariff of a connector")
		assert.ErrorIs(t, s.DeleteTariff(ctx, strings.ToUpper(adhoc.Id)), store.ErrInUse, "the ad hoc tariff of a connector")
		assert.Nil(t, lookupTariff(t, s, regular.Id).DeletedAt)
		assert.Nil(t, lookupTariff(t, s, adhoc.Id).DeletedAt)
		require.NoError(t, s.DeleteTariff(ctx, unused.Id))
		assert.NotNil(t, lookupTariff(t, s, unused.Id).DeletedAt)

		// a connector that lets go of the tariff frees it
		first := evse.Connectors[0]
		first.TariffRef = nil
		require.NoError(t, s.UpdateAssetConnector(ctx, first))
		require.NoError(t, s.DeleteTariff(ctx, regular.Id))
		assert.NotNil(t, lookupTariff(t, s, regular.Id).DeletedAt)
		// so does a connector that is deleted
		require.NoError(t, s.DeleteAssetConnector(ctx, evse.Connectors[1].Id))
		require.NoError(t, s.DeleteTariff(ctx, adhoc.Id))
		assert.NotNil(t, lookupTariff(t, s, adhoc.Id).DeletedAt)
	})

	t.Run("a deleted tariff stays readable and takes no changes", func(t *testing.T) {
		s := newStore(t)
		v := testTariffVersion(3000)
		tariff := createTariff(t, s, testTariff("Standard"), v)
		kept := createTariff(t, s, testTariff("Kept"), testTariffVersion(3500))
		before := time.Now().Add(-time.Minute)

		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))

		deleted := lookupTariff(t, s, tariff.Id)
		require.NotNil(t, deleted, "a deleted tariff is still looked up")
		require.NotNil(t, deleted.DeletedAt)
		assert.True(t, deleted.DeletedAt.After(before))
		assert.Equal(t, time.UTC, deleted.DeletedAt.Location())
		assert.False(t, deleted.UpdatedAt.Before(tariff.UpdatedAt))
		assert.Equal(t, 1, deleted.CurrentVersion)
		assertTariffVersion(t, v, deleted.Current)
		assertTariffVersion(t, v, lookupTariffVersion(t, s, tariff.Id, 1))
		assert.Len(t, listTariffVersions(t, s, tariff.Id), 1)
		byOcpiId, err := s.LookupTariffByOcpiId(ctx, assetPartyA, tariff.OcpiId)
		require.NoError(t, err)
		assertTariff(t, deleted, byOcpiId)

		assert.Equal(t, []string{"Kept"}, listTariffNames(t, s, store.TariffFilter{OrgIds: []string{orgA}}))
		assert.Equal(t, []string{"Standard", "Kept"}, listTariffNames(t, s, store.TariffFilter{OrgIds: []string{orgA}, IncludeDeleted: true}))

		assert.ErrorIs(t, s.AddTariffVersion(ctx, tariff.Id, 1, testTariffVersion(9900)), store.ErrNotFound)
		assert.ErrorIs(t, s.RenameTariff(ctx, tariff.Id, "Renamed"), store.ErrNotFound)
		assertTariff(t, deleted, lookupTariff(t, s, tariff.Id), "a refused change changes nothing")

		// deleting again, or what is not there, does nothing
		require.NoError(t, s.DeleteTariff(ctx, tariff.Id))
		assertTariff(t, deleted, lookupTariff(t, s, tariff.Id))
		assert.NoError(t, s.DeleteTariff(ctx, testAssetId(99)))
		assert.NoError(t, s.DeleteTariff(ctx, "not-a-uuid"))
		assert.Nil(t, lookupTariff(t, s, kept.Id).DeletedAt)
	})

	t.Run("list filters", func(t *testing.T) {
		s := newStore(t)
		add := func(n int, orgId, partyRef, name string, tariffType *store.TariffType) *store.Tariff {
			tariff := testTariff(name)
			tariff.Id = testAssetId(n)
			tariff.OrgId = orgId
			tariff.PartyRef = partyRef
			tariff.OcpiId = fmt.Sprintf("TARIFF-%d", n)
			v := testTariffVersion(3000)
			v.Type = tariffType
			return createTariff(t, s, tariff, v)
		}
		add(1, orgA, assetPartyA, "Standard", ptr(store.TariffTypeRegular))
		add(2, orgA, assetPartyB, "Ad hoc", ptr(store.TariffTypeAdHocPayment))
		add(3, orgB, assetPartyA, "Night 100%_off", nil)
		add(4, orgC, assetPartyA, "Other org", ptr(store.TariffTypeRegular))
		add(5, orgA, assetPartyA, "Old", ptr(store.TariffTypeRegular))
		require.NoError(t, s.DeleteTariff(ctx, testAssetId(5)))
		// the type of a tariff is the type of its current version
		changed := add(6, orgB, assetPartyB, "Changed", ptr(store.TariffTypeRegular))
		adhoc := testTariffVersion(4500)
		adhoc.Type = ptr(store.TariffTypeAdHocPayment)
		require.NoError(t, s.AddTariffVersion(ctx, changed.Id, 1, adhoc))

		filter := func(orgIds ...string) store.TariffFilter {
			return store.TariffFilter{OrgIds: orgIds}
		}
		assert.Equal(t, []string{"Standard", "Ad hoc"}, listTariffNames(t, s, filter(orgA)))
		assert.Equal(t, []string{"Night 100%_off", "Changed"}, listTariffNames(t, s, filter(strings.ToUpper(orgB))), "an org id is a UUID in any case")
		assert.Equal(t, []string{"Standard", "Ad hoc", "Night 100%_off", "Changed"}, listTariffNames(t, s, filter(orgB, orgA)))
		assert.Empty(t, listTariffNames(t, s, filter()), "no org, no tariffs")
		_, _, err := s.ListTariffs(ctx, filter("not-a-uuid"))
		assert.Error(t, err)

		f := filter(orgA, orgB)
		f.IncludeDeleted = true
		assert.Equal(t, []string{"Standard", "Ad hoc", "Night 100%_off", "Old", "Changed"}, listTariffNames(t, s, f))
		f = filter(orgA, orgB)
		f.PartyRef = ptr(assetPartyB)
		assert.Equal(t, []string{"Ad hoc", "Changed"}, listTariffNames(t, s, f))
		f = filter(orgA, orgB)
		f.Type = ptr(store.TariffTypeAdHocPayment)
		assert.Equal(t, []string{"Ad hoc", "Changed"}, listTariffNames(t, s, f))
		f.Type = ptr(store.TariffTypeRegular)
		assert.Equal(t, []string{"Standard"}, listTariffNames(t, s, f))
		f.IncludeDeleted = true
		assert.Equal(t, []string{"Standard", "Old"}, listTariffNames(t, s, f))
		f.Type = ptr(store.TariffTypeProfileGreen)
		assert.Empty(t, listTariffNames(t, s, f))

		for query, want := range map[string][]string{
			"STAND":    {"Standard"},       // name, other case
			"tariff-2": {"Ad hoc"},         // OCPI id, other case
			"100%_o":   {"Night 100%_off"}, // % and _ are plain characters
			"%":        {"Night 100%_off"},
			"_":        {"Night 100%_off"},
			"d":        {"Standard", "Ad hoc", "Changed"},
			"nothing":  {},
		} {
			f := filter(orgA, orgB)
			f.Query = ptr(query)
			assert.Equal(t, want, listTariffNames(t, s, f), query)
		}

		// a listed tariff comes with its current version
		tariffs, _, err := s.ListTariffs(ctx, filter(orgB))
		require.NoError(t, err)
		require.Len(t, tariffs, 2)
		assertTariff(t, lookupTariff(t, s, testAssetId(3)), tariffs[0])
		assertTariff(t, lookupTariff(t, s, changed.Id), tariffs[1])
		assert.Equal(t, 2, tariffs[1].Current.Version)
	})

	t.Run("list pages", func(t *testing.T) {
		s := newStore(t)
		const tariffs = 120
		for i := range tariffs {
			tariff := testTariff(fmt.Sprintf("Tariff %03d", i))
			if i%2 == 1 {
				tariff.OrgId = orgB
			}
			createTariff(t, s, tariff, testTariffVersion(3000))
		}
		other := testTariff("Other org")
		other.OrgId = orgC
		createTariff(t, s, other, testTariffVersion(3000))

		seen := map[string]bool{}
		var pageSizes []int
		var lastId string
		page := store.PageRequest{Limit: 50}
		for {
			items, next, err := s.ListTariffs(ctx, store.TariffFilter{OrgIds: []string{orgA, orgB}, Page: page})
			require.NoError(t, err)
			pageSizes = append(pageSizes, len(items))
			for _, tariff := range items {
				assert.False(t, seen[tariff.Id], "listed twice: %s", tariff.Id)
				assert.Greater(t, tariff.Id, lastId, "ordered by id")
				seen[tariff.Id] = true
				lastId = tariff.Id
			}
			if next == "" {
				break
			}
			require.Less(t, len(pageSizes), 5, "too many pages")
			page.Cursor = next
		}
		assert.Equal(t, []int{50, 50, 20}, pageSizes)
		assert.Len(t, seen, tariffs)

		items, next, err := s.ListTariffs(ctx, store.TariffFilter{OrgIds: []string{orgA, orgB}})
		require.NoError(t, err)
		assert.Len(t, items, store.DefaultPageLimit)
		assert.NotEmpty(t, next)
		items, next, err = s.ListTariffs(ctx, store.TariffFilter{OrgIds: []string{orgA, orgB}, Page: store.PageRequest{Limit: 120}})
		require.NoError(t, err)
		assert.Len(t, items, 120)
		assert.Empty(t, next, "a page that ends exactly at the last tariff")

		_, _, err = s.ListTariffs(ctx, store.TariffFilter{OrgIds: []string{orgA}, Page: store.PageRequest{Cursor: "not a cursor"}})
		assert.ErrorIs(t, err, store.ErrInvalidCursor)
	})
}
