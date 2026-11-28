// SPDX-License-Identifier: Apache-2.0

package rating_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockTest "k8s.io/utils/clock/testing"

	"github.com/thoughtworks/maeve-csms/manager/events/eventstest"
	"github.com/thoughtworks/maeve-csms/manager/money"
	"github.com/thoughtworks/maeve-csms/manager/rating"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
)

const (
	analysisOrg = "0193b2c4-0000-7000-8000-0000000000a1"
	// an OCPP 1.6 charge station: connector 1 has a highest power of 22 kW, connector 2
	// of 150 kW, connector 3 has none
	analysisStation = "aaaa.CP001"
)

// analysisTime is the time of the analysis: the next day.
var analysisTime = time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)

// analysisFixture is an Analyzer with the default settings on an in-memory store.
type analysisFixture struct {
	engine   *inmemory.Store
	analyzer *rating.Analyzer
	// site is the location of the charge station
	site *store.AssetLocation
}

func newAnalysisFixture(t *testing.T) *analysisFixture {
	t.Helper()
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(analysisTime)
	engine := inmemory.NewStore(clk)

	require.NoError(t, engine.SetChargeStationAuth(ctx, analysisStation, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: analysisOrg}))
	require.NoError(t, engine.SetChargeStationRuntimeDetails(ctx, analysisStation, &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
	site := &store.AssetLocation{
		OrgId: analysisOrg, PartyRef: "party-a", Address: "Calle Mayor 1", City: "Madrid", Country: "ESP",
		Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid",
	}
	require.NoError(t, engine.CreateAssetLocation(ctx, site))
	for ocppEvseId, maxElectricPower := range map[int]*int32{1: ptr(int32(22_000)), 2: ptr(int32(150_000)), 3: nil} {
		require.NoError(t, engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: analysisOrg, LocationId: site.Id, StationKey: analysisStation, OcppEvseId: ocppEvseId,
			Connectors: []*store.AssetConnector{{
				OcppConnectorId: 1, Standard: "IEC_62196_T2", Format: "SOCKET", PowerType: "AC_3_PHASE", MaxVoltage: 400, MaxAmperage: 32,
				MaxElectricPower: maxElectricPower,
			}},
		}))
	}

	return &analysisFixture{
		engine: engine,
		site:   site,
		analyzer: &rating.Analyzer{
			Transactions: engine,
			MeterValues:  engine,
			Assets:       engine,
			Registry:     engine,
			Tariffs:      engine,
			Defaults:     rating.DefaultIdleSettings(),
			Clock:        clk,
		},
	}
}

// session stores an ended OCPP 1.6 transaction of the station as its handlers do: the
// meter start with the start, the readings in between on their own, the meter stop with
// the stop. The readings are the minute of the session and its energy so far in kWh; the
// first one is the start and the last one the stop.
func (f *analysisFixture) session(t *testing.T, station, transactionId string, connectorId int, minutesAndKWh ...float64) {
	t.Helper()
	f.start(t, station, transactionId, connectorId, minutesAndKWh[1])
	points := readings(minutesAndKWh...)
	var sent []store.MeterValue
	for _, p := range points[1 : len(points)-1] {
		sent = append(sent, registerMeterValue(p, "Sample.Periodic"))
	}
	if len(sent) > 0 {
		require.NoError(t, f.engine.StoreMeterValues(context.Background(), station, connectorId, transactionId, sent))
	}
	f.stop(t, station, transactionId, points[len(points)-1])
}

func registerMeterValue(p rating.EnergyPoint, context string) store.MeterValue {
	return store.MeterValue{
		Timestamp: p.At.Format(time.RFC3339),
		SampledValues: []store.SampledValue{
			sample(p.RegisterWh, sampleContext(context), measurand("Energy.Active.Import.Register"), location("Outlet"), unit("Wh", 0)),
		},
	}
}

// start stores the start of an OCPP 1.6 transaction at the start of the session.
func (f *analysisFixture) start(t *testing.T, station, transactionId string, connectorId int, kWh float64) {
	t.Helper()
	ctx := context.Background()
	begin := reading(0, kWh)
	require.NoError(t, f.engine.CreateTransaction(ctx, station, transactionId, "RFID0001", "ISO14443",
		[]store.MeterValue{registerMeterValue(begin, "Transaction.Begin")}, 0, false))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station, transactionId, &store.TransactionDetails{
		ConnectorId: &connectorId, EvseId: &connectorId, StartedAt: &begin.At, MeterStartWh: ptr(int64(begin.RegisterWh)),
	}))
}

// stop stores the stop of an OCPP 1.6 transaction.
func (f *analysisFixture) stop(t *testing.T, station, transactionId string, end rating.EnergyPoint) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, f.engine.EndTransaction(ctx, station, transactionId, "RFID0001", "ISO14443",
		[]store.MeterValue{registerMeterValue(end, "Transaction.End")}, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station, transactionId, &store.TransactionDetails{
		StoppedAt: &end.At, MeterStopWh: ptr(int64(end.RegisterWh)),
	}))
}

func (f *analysisFixture) analyse(t *testing.T, station, transactionId string) *store.TransactionAnalysis {
	t.Helper()
	analysis, err := f.analyzer.AnalyseAndStore(context.Background(), station, transactionId)
	require.NoError(t, err)
	require.NotNil(t, analysis)
	assert.Equal(t, analysis, f.stored(t, station, transactionId), "what is returned is what is stored")
	return analysis
}

// stored returns the analysis that is stored with the transaction.
func (f *analysisFixture) stored(t *testing.T, station, transactionId string) *store.TransactionAnalysis {
	t.Helper()
	transaction, err := f.engine.FindTransaction(context.Background(), station, transactionId)
	require.NoError(t, err)
	require.NotNil(t, transaction)
	return transaction.Analysis
}

func TestAnalyseAndStore(t *testing.T) {
	for name, tc := range map[string]struct {
		session []float64
		want    store.TransactionAnalysis
	}{
		"an hour of charging and an hour and a half plugged in": {
			session: []float64{0, 0, 30, 5.5, 60, 11, 120, 11, 150, 11},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(60)), IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(11_000.0)},
		},
		"charged until the stop": {
			session: []float64{0, 0, 30, 5.5, 60, 11},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(60)), IdleS: 0, ChargingS: 3600, AveragePowerW: ptr(11_000.0)},
		},
		"a slow charger is not idle": {
			session: []float64{0, 0, 60, 3.7, 120, 7.4},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(120)), IdleS: 0, ChargingS: 7200, AveragePowerW: ptr(3700.0)},
		},
		"only the meter start and the meter stop": {
			session: []float64{0, 0, 180, 11},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(180)), IdleS: 0, ChargingS: 10_800, AveragePowerW: ptr(3666.7)},
		},
		"never charged": {
			session: []float64{0, 0, 30, 0, 62, 0},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(0)), IdleS: 3720, ChargingS: 0, AveragePowerW: ptr(0.0)},
		},
		"the average power to one decimal": {
			// 10 kWh in 70 minutes
			session: []float64{0, 0, 70, 10, 100, 10},
			want:    store.TransactionAnalysis{IdleStartedAt: ptr(minute(70)), IdleS: 1800, ChargingS: 4200, AveragePowerW: ptr(8571.4)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAnalysisFixture(t)
			f.session(t, analysisStation, "tx-1", 1, tc.session...)
			tc.want.AnalysedAt = analysisTime

			assert.Equal(t, &tc.want, f.analyse(t, analysisStation, "tx-1"))
		})
	}
}

// The meter start and the meter stop of a transaction are readings of its register, also
// when no meter value of the transaction is stored.
func TestAnalyseFromTheMeterStartAndTheMeterStopAlone(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	require.NoError(t, f.engine.CreateTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", nil, 0, false))
	require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", nil, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, analysisStation, "tx-1", &store.TransactionDetails{
		StartedAt: ptr(sessionStart), StoppedAt: ptr(minute(180)), MeterStartWh: ptr(int64(100_000)), MeterStopWh: ptr(int64(111_000)),
	}))

	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(180)), IdleS: 0, ChargingS: 10_800, AveragePowerW: ptr(3666.7), AnalysedAt: analysisTime,
	}, f.analyse(t, analysisStation, "tx-1"), "3.7 kW on average: charging until the stop")

	// the meter stop alone says nothing about charging
	require.NoError(t, f.engine.CreateTransaction(ctx, analysisStation, "tx-2", "RFID0001", "ISO14443", nil, 0, false))
	require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-2", "RFID0001", "ISO14443", nil, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, analysisStation, "tx-2", &store.TransactionDetails{
		StartedAt: ptr(sessionStart), StoppedAt: ptr(minute(180)), MeterStopWh: ptr(int64(111_000)),
	}))
	assert.Equal(t, &store.TransactionAnalysis{IdleStartedAt: ptr(sessionStart), IdleS: 10_800, AnalysedAt: analysisTime},
		f.analyse(t, analysisStation, "tx-2"))
}

// A transaction that ended without a reading has no idle time: there is nothing to tell
// from.
func TestAnalyseATransactionWithoutReadings(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	require.NoError(t, f.engine.CreateTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", nil, 0, false))
	require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", nil, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, analysisStation, "tx-1", &store.TransactionDetails{
		StartedAt: ptr(sessionStart), StoppedAt: ptr(minute(45)),
	}))

	assert.Equal(t, &store.TransactionAnalysis{ChargingS: 2700, AnalysedAt: analysisTime}, f.analyse(t, analysisStation, "tx-1"))
}

// A transaction charges from the higher of the minimum charging power and 1 % of the
// highest power of its connector: the highest power of the connector in the asset model,
// else the capacity of the charge station.
func TestAnalyseWithTheHighestPowerOfTheConnector(t *testing.T) {
	ctx := context.Background()
	// an hour at 11 kW, then an hour at 1 kW
	trickle := []float64{0, 0, 60, 11, 120, 12}
	charging := &store.TransactionAnalysis{IdleStartedAt: ptr(minute(120)), IdleS: 0, ChargingS: 7200, AveragePowerW: ptr(6000.0), AnalysedAt: analysisTime}
	idle := &store.TransactionAnalysis{IdleStartedAt: ptr(minute(60)), IdleS: 3600, ChargingS: 3600, AveragePowerW: ptr(6000.0), AnalysedAt: analysisTime}
	setCapacity := func(t *testing.T, f *analysisFixture, kW *float64) {
		t.Helper()
		station, err := f.engine.LookupChargeStation(ctx, analysisStation)
		require.NoError(t, err)
		station.MaxCapacityKw = kW
		require.NoError(t, f.engine.UpdateChargeStation(ctx, station))
	}

	t.Run("a connector of 22 kW: 1 kW is charging", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.session(t, analysisStation, "tx-1", 1, trickle...)
		assert.Equal(t, charging, f.analyse(t, analysisStation, "tx-1"))
	})

	t.Run("a connector of 150 kW: 1 kW is idle", func(t *testing.T) {
		f := newAnalysisFixture(t)
		// the capacity of the charge station does not count when the connector has a power
		setCapacity(t, f, ptr(22.0))
		f.session(t, analysisStation, "tx-1", 2, trickle...)
		assert.Equal(t, idle, f.analyse(t, analysisStation, "tx-1"))
	})

	t.Run("a connector without a highest power: the capacity of the charge station", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.session(t, analysisStation, "tx-1", 3, trickle...)
		assert.Equal(t, charging, f.analyse(t, analysisStation, "tx-1"), "nothing known: the minimum of 200 W")

		setCapacity(t, f, ptr(150.0))
		assert.Equal(t, idle, f.analyse(t, analysisStation, "tx-1"))
	})

	t.Run("a connector that is not in the asset model: the capacity of the charge station", func(t *testing.T) {
		f := newAnalysisFixture(t)
		setCapacity(t, f, ptr(150.0))
		f.session(t, analysisStation, "tx-1", 4, trickle...)
		assert.Equal(t, idle, f.analyse(t, analysisStation, "tx-1"))
	})

	t.Run("a charge station whose OCPP version is not known: its capacity", func(t *testing.T) {
		f := newAnalysisFixture(t)
		// the charge station has not booted: the ids of its transaction cannot be read
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, "aaaa.CP002", &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: analysisOrg}))
		require.NoError(t, f.engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: analysisOrg, LocationId: f.site.Id, StationKey: "aaaa.CP002", OcppEvseId: 2,
			Connectors: []*store.AssetConnector{{
				OcppConnectorId: 1, Standard: "IEC_62196_T2_COMBO", Format: "CABLE", PowerType: "DC", MaxVoltage: 920, MaxAmperage: 200,
				MaxElectricPower: ptr(int32(150_000)),
			}},
		}))
		f.session(t, "aaaa.CP002", "tx-1", 2, trickle...)
		assert.Equal(t, charging, f.analyse(t, "aaaa.CP002", "tx-1"), "no capacity either: the minimum of 200 W")

		// a version whose rules for the ids of a transaction are not known
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, "aaaa.CP002", &store.ChargeStationRuntimeDetails{OcppVersion: "2.1"}))
		assert.Equal(t, charging, f.analyse(t, "aaaa.CP002", "tx-1"))

		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, "aaaa.CP002", &store.ChargeStationRuntimeDetails{OcppVersion: "1.6"}))
		assert.Equal(t, idle, f.analyse(t, "aaaa.CP002", "tx-1"), "once it has booted with OCPP 1.6, its connector of 150 kW is found")
	})

	t.Run("a charge station that is not registered", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.session(t, "zzzz.CP009", "tx-1", 2, trickle...)
		assert.Equal(t, charging, f.analyse(t, "zzzz.CP009", "tx-1"))
	})

	t.Run("an OCPP 2.0.1 charge station names the EVSE and the connector", func(t *testing.T) {
		f := newAnalysisFixture(t)
		const station201 = "aaaa.CP201"
		require.NoError(t, f.engine.SetChargeStationAuth(ctx, station201, &store.ChargeStationAuth{SecurityProfile: store.TLSWithBasicAuth, OrgId: analysisOrg}))
		require.NoError(t, f.engine.SetChargeStationRuntimeDetails(ctx, station201, &store.ChargeStationRuntimeDetails{OcppVersion: "2.0.1"}))
		site := &store.AssetLocation{
			OrgId: analysisOrg, PartyRef: "party-a", Address: "Calle Mayor 2", City: "Madrid", Country: "ESP",
			Latitude: "40.416775", Longitude: "-3.703790", TimeZone: "Europe/Madrid",
		}
		require.NoError(t, f.engine.CreateAssetLocation(ctx, site))
		connector := func(id int, maxElectricPower int32) *store.AssetConnector {
			return &store.AssetConnector{
				OcppConnectorId: id, Standard: "IEC_62196_T2_COMBO", Format: "CABLE", PowerType: "DC", MaxVoltage: 920, MaxAmperage: 200,
				MaxElectricPower: &maxElectricPower,
			}
		}
		require.NoError(t, f.engine.CreateAssetEvse(ctx, &store.AssetEvse{
			OrgId: analysisOrg, LocationId: site.Id, StationKey: station201, OcppEvseId: 1,
			Connectors: []*store.AssetConnector{connector(1, 22_000), connector(2, 150_000)},
		}))

		// the transaction ran at connector 2 of EVSE 1
		f.session(t, station201, "tx-1", 1, trickle...)
		require.NoError(t, f.engine.UpdateTransactionDetails(ctx, station201, "tx-1", &store.TransactionDetails{EvseId: ptr(1), ConnectorId: ptr(2)}))
		assert.Equal(t, idle, f.analyse(t, station201, "tx-1"))

		f.session(t, station201, "tx-2", 1, trickle...)
		assert.Equal(t, charging, f.analyse(t, station201, "tx-2"), "connector 1 of EVSE 1")
	})

	t.Run("a transaction that does not say where it ran", func(t *testing.T) {
		f := newAnalysisFixture(t)
		begin, end := reading(0, 0), reading(120, 12)
		require.NoError(t, f.engine.CreateTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443",
			[]store.MeterValue{registerMeterValue(begin, "Transaction.Begin"), registerMeterValue(reading(60, 11), "Sample.Periodic")}, 0, false))
		require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443",
			[]store.MeterValue{registerMeterValue(end, "Transaction.End")}, 1))
		require.NoError(t, f.engine.UpdateTransactionDetails(ctx, analysisStation, "tx-1", &store.TransactionDetails{StartedAt: &begin.At, StoppedAt: &end.At}))

		assert.Equal(t, charging, f.analyse(t, analysisStation, "tx-1"))
		setCapacity(t, f, ptr(150.0))
		assert.Equal(t, idle, f.analyse(t, analysisStation, "tx-1"))
	})
}

// The idle settings of the tariff version that the transaction is charged with come
// before the settings of the manager, each on its own.
func TestAnalyseWithTheIdleSettingsOfTheTariff(t *testing.T) {
	ctx := context.Background()
	// an hour at 11 kW, then an hour at 1 kW, at the connector of 22 kW
	trickle := []float64{0, 0, 60, 11, 120, 12}
	// pinned analyses such a session that is charged with the version of a tariff whose
	// versions have these idle settings, with these settings of the manager, and returns
	// the minute from which the session is idle: 60 when 1 kW is not charging, 120 when
	// it is.
	pinned := func(t *testing.T, defaults rating.IdleSettings, version int, versions ...*store.IdleDetection) time.Time {
		t.Helper()
		f := newAnalysisFixture(t)
		f.analyzer.Defaults = defaults
		tariff := &store.Tariff{OrgId: analysisOrg, PartyRef: "party-a", Name: "Regular"}
		for i, idle := range versions {
			v := tariffVersionWith(idle)
			if i == 0 {
				require.NoError(t, f.engine.CreateTariff(ctx, tariff, v))
				continue
			}
			require.NoError(t, f.engine.AddTariffVersion(ctx, tariff.Id, i, v))
		}
		f.session(t, analysisStation, "tx-1", 1, trickle...)
		require.NoError(t, f.engine.SetTransactionTariff(ctx, analysisStation, "tx-1", &tariff.Id, &version, "connector"))
		analysis := f.analyse(t, analysisStation, "tx-1")
		require.NotNil(t, analysis.IdleStartedAt)
		return *analysis.IdleStartedAt
	}
	defaults := rating.DefaultIdleSettings()

	assert.Equal(t, minute(120), pinned(t, defaults, 1, nil), "a version without idle settings: the 220 W of the manager")
	assert.Equal(t, minute(120), pinned(t, defaults, 1, &store.IdleDetection{}))
	assert.Equal(t, minute(60), pinned(t, defaults, 1, &store.IdleDetection{MinChargingPowerW: ptr(1500.0)}), "1 kW is below 1.5 kW")
	assert.Equal(t, minute(60), pinned(t, defaults, 1, &store.IdleDetection{ChargingPowerShare: ptr(0.05)}), "1 kW is below 5 % of 22 kW")

	// the version that the transaction is charged with, not the newest one
	olderAndNewer := []*store.IdleDetection{nil, {MinChargingPowerW: ptr(1500.0)}, nil}
	assert.Equal(t, minute(120), pinned(t, defaults, 1, olderAndNewer...))
	assert.Equal(t, minute(60), pinned(t, defaults, 2, olderAndNewer...))
	assert.Equal(t, minute(120), pinned(t, defaults, 3, olderAndNewer...))
	assert.Equal(t, minute(120), pinned(t, defaults, 9, olderAndNewer...), "a version that is not there: the settings of the manager")

	// a manager for which a transaction charges from 60 % of the power of the connector
	most := defaults
	most.ChargingPowerShare = 0.6
	assert.Equal(t, minute(0), pinned(t, most, 1, nil), "11 kW is below 60 % of 22 kW: never charged")
	assert.Equal(t, minute(120), pinned(t, most, 1, &store.IdleDetection{ChargingPowerShare: ptr(0.01)}), "the share of the version")
	// a manager with a minimum of 1.2 kW
	highMinimum := defaults
	highMinimum.MinChargingPowerW = 1200
	assert.Equal(t, minute(60), pinned(t, highMinimum, 1, &store.IdleDetection{ChargingPowerShare: ptr(0.02)}),
		"the version sets the share only: the minimum of the manager stays")
	assert.Equal(t, minute(120), pinned(t, highMinimum, 1, &store.IdleDetection{MinChargingPowerW: ptr(0.0)}),
		"a minimum of 0 W is a setting")

	t.Run("a transaction without a tariff", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.session(t, analysisStation, "tx-1", 1, trickle...)
		require.NoError(t, f.engine.SetTransactionTariff(ctx, analysisStation, "tx-1", nil, nil, "none"))
		assert.Equal(t, ptr(minute(120)), f.analyse(t, analysisStation, "tx-1").IdleStartedAt)
	})

	// the average power is that of the intervals above the minimum charging power that
	// applies to the transaction
	t.Run("the average power", func(t *testing.T) {
		average := func(idle *store.IdleDetection) float64 {
			f := newAnalysisFixture(t)
			tariff := &store.Tariff{OrgId: analysisOrg, PartyRef: "party-a", Name: "Regular"}
			require.NoError(t, f.engine.CreateTariff(ctx, tariff, tariffVersionWith(idle)))
			f.session(t, analysisStation, "tx-1", 1, trickle...)
			require.NoError(t, f.engine.SetTransactionTariff(ctx, analysisStation, "tx-1", &tariff.Id, ptr(1), "connector"))
			analysis := f.analyse(t, analysisStation, "tx-1")
			require.NotNil(t, analysis.AveragePowerW)
			return *analysis.AveragePowerW
		}
		assert.Equal(t, 6000.0, average(nil), "both hours are above 200 W")
		assert.Equal(t, 11_000.0, average(&store.IdleDetection{MinChargingPowerW: ptr(1500.0)}), "the hour at 1 kW is not above 1.5 kW")
		assert.Equal(t, 6000.0, average(&store.IdleDetection{ChargingPowerShare: ptr(0.05)}), "the share does not count for the average")
	})
}

func tariffVersionWith(idle *store.IdleDetection) *store.TariffVersion {
	return &store.TariffVersion{
		Currency: "EUR",
		Elements: []store.TariffElement{{PriceComponents: []store.PriceComponent{
			{Type: store.PriceComponentEnergy, Price: money.Amount(3000), StepSize: 1},
		}}},
		IdleDetection: idle,
		CreatedBy:     "user:alice",
	}
}

// A store that does not keep the times of a transaction (in memory, for OCPP 2.0.1) leaves
// the times of its first and its last meter value.
func TestAnalyseATransactionWithoutStoredTimes(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	value := func(p rating.EnergyPoint) []store.MeterValue {
		// as an OCPP 2.0.1 charge station sends it: only the value
		return []store.MeterValue{{Timestamp: p.At.Format(time.RFC3339), SampledValues: []store.SampledValue{sample(p.RegisterWh)}}}
	}
	require.NoError(t, f.engine.CreateTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", value(reading(0, 0)), 0, false))
	require.NoError(t, f.engine.UpdateTransaction(ctx, analysisStation, "tx-1", value(reading(30, 5.5))))
	require.NoError(t, f.engine.UpdateTransaction(ctx, analysisStation, "tx-1", value(reading(60, 11))))

	_, err := f.analyzer.AnalyseAndStore(ctx, analysisStation, "tx-1")
	require.ErrorIs(t, err, store.ErrInvalidState, "it runs")

	require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-1", "RFID0001", "ISO14443", value(reading(150, 11)), 3))

	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(60)), IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(11_000.0), AnalysedAt: analysisTime,
	}, f.analyse(t, analysisStation, "tx-1"))
}

func TestAnalyseAgainReplacesTheAnalysis(t *testing.T) {
	ctx := context.Background()
	clk := clockTest.NewFakePassiveClock(analysisTime)
	f := newAnalysisFixture(t)
	f.analyzer.Clock = clk
	// the charge station was offline in the second hour: its stop says 16.5 kWh
	f.session(t, analysisStation, "tx-1", 1, 0, 0, 60, 11, 120, 16.5)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(120)), IdleS: 0, ChargingS: 7200, AveragePowerW: ptr(8250.0), AnalysedAt: analysisTime,
	}, f.analyse(t, analysisStation, "tx-1"), "5.5 kW on average in the second hour")

	// a meter value of the transaction that arrives late: the register was at its end
	// after an hour and a half
	late := registerMeterValue(reading(90, 16.5), "Sample.Periodic")
	require.NoError(t, f.engine.StoreMeterValues(ctx, analysisStation, 1, "tx-1", []store.MeterValue{late}))
	clk.SetTime(analysisTime.Add(time.Hour))

	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(90)), IdleS: 1800, ChargingS: 5400, AveragePowerW: ptr(11_000.0), AnalysedAt: analysisTime.Add(time.Hour),
	}, f.analyse(t, analysisStation, "tx-1"))
}

// Readings that give a duration or a power that cannot be are left out of the analysis,
// which is stored all the same.
func TestAnalyseReadingsThatAreWrong(t *testing.T) {
	ctx := context.Background()

	t.Run("an end before the start", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.start(t, analysisStation, "tx-1", 1, 0)
		f.stop(t, analysisStation, "tx-1", reading(-30, 5))

		assert.Equal(t, &store.TransactionAnalysis{IdleStartedAt: ptr(minute(0)), AveragePowerW: ptr(0.0), AnalysedAt: analysisTime},
			f.analyse(t, analysisStation, "tx-1"), "no idle time and no charging time")
	})

	t.Run("a meter value of a clock that was wrong", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.session(t, analysisStation, "tx-1", 1, 0, 0, 60, 11, 120, 11)
		// timed eight hours after the stop, with 39 kWh more on the register
		late := registerMeterValue(reading(600, 50), "Sample.Periodic")
		require.NoError(t, f.engine.StoreMeterValues(ctx, analysisStation, 1, "tx-1", []store.MeterValue{late}))

		assert.Equal(t, &store.TransactionAnalysis{
			IdleStartedAt: ptr(minute(60)), IdleS: 3600, ChargingS: 3600, AveragePowerW: ptr(11_000.0), AnalysedAt: analysisTime,
		}, f.analyse(t, analysisStation, "tx-1"), "it counts neither for the idle time nor for the average power")
	})

	t.Run("a register that jumps", func(t *testing.T) {
		f := newAnalysisFixture(t)
		f.start(t, analysisStation, "tx-1", 1, 0)
		// 2000 GWh in a second
		jump := store.MeterValue{Timestamp: sessionStart.Add(time.Second).Format(time.RFC3339), SampledValues: []store.SampledValue{sample(2e12)}}
		require.NoError(t, f.engine.StoreMeterValues(ctx, analysisStation, 1, "tx-1", []store.MeterValue{jump}))
		f.stop(t, analysisStation, "tx-1", reading(60, 11))

		assert.Equal(t, &store.TransactionAnalysis{IdleStartedAt: ptr(sessionStart.Add(time.Second)), IdleS: 3599, ChargingS: 1, AnalysedAt: analysisTime},
			f.analyse(t, analysisStation, "tx-1"), "no average power")
	})
}

func TestAnalyseWhatCannotBeAnalysed(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)

	analysis, err := f.analyzer.AnalyseAndStore(ctx, analysisStation, "tx-unknown")
	require.ErrorIs(t, err, store.ErrNotFound)
	assert.Nil(t, analysis)

	f.session(t, analysisStation, "tx-1", 1, 0, 0, 60, 11)
	analysis, err = f.analyzer.AnalyseAndStore(ctx, "aaaa.CP002", "tx-1")
	require.ErrorIs(t, err, store.ErrNotFound, "the transaction of another charge station")
	assert.Nil(t, analysis)

	f.start(t, analysisStation, "tx-2", 1, 0)
	analysis, err = f.analyzer.AnalyseAndStore(ctx, analysisStation, "tx-2")
	require.ErrorIs(t, err, store.ErrInvalidState, "a transaction that runs")
	assert.Nil(t, analysis)
	assert.Nil(t, f.stored(t, analysisStation, "tx-2"))

	// ended, but neither its start nor a meter value is known
	require.NoError(t, f.engine.EndTransaction(ctx, analysisStation, "tx-3", "RFID0001", "ISO14443", nil, 1))
	require.NoError(t, f.engine.UpdateTransactionDetails(ctx, analysisStation, "tx-3", &store.TransactionDetails{StoppedAt: ptr(minute(60))}))
	analysis, err = f.analyzer.AnalyseAndStore(ctx, analysisStation, "tx-3")
	require.ErrorIs(t, err, store.ErrInvalidState)
	assert.Nil(t, analysis)
	assert.Nil(t, f.stored(t, analysisStation, "tx-3"))
}

// failingBackend is a store whose calls fail with the errors that are set.
type failingBackend struct {
	store.Engine
	find, query, tariff, station, evse, set error
	setCalls                                int
}

func (s *failingBackend) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	if s.find != nil {
		return nil, s.find
	}
	return s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
}

func (s *failingBackend) QueryMeterValues(ctx context.Context, filter store.MeterValuesFilter) (*store.MeterValuesResult, error) {
	if s.query != nil {
		return nil, s.query
	}
	return s.Engine.QueryMeterValues(ctx, filter)
}

func (s *failingBackend) LookupTariffVersion(ctx context.Context, tariffId string, version int) (*store.TariffVersion, error) {
	if s.tariff != nil {
		return nil, s.tariff
	}
	return s.Engine.LookupTariffVersion(ctx, tariffId, version)
}

func (s *failingBackend) LookupChargeStation(ctx context.Context, stationKey string) (*store.ChargeStation, error) {
	if s.station != nil {
		return nil, s.station
	}
	return s.Engine.LookupChargeStation(ctx, stationKey)
}

func (s *failingBackend) LookupAssetEvseByOcpp(ctx context.Context, stationKey string, ocppEvseId int) (*store.AssetEvse, error) {
	if s.evse != nil {
		return nil, s.evse
	}
	return s.Engine.LookupAssetEvseByOcpp(ctx, stationKey, ocppEvseId)
}

func (s *failingBackend) SetTransactionAnalysis(ctx context.Context, chargeStationId, transactionId string, analysis store.TransactionAnalysis) error {
	s.setCalls++
	if s.set != nil {
		return s.set
	}
	return s.Engine.SetTransactionAnalysis(ctx, chargeStationId, transactionId, analysis)
}

// on returns an Analyzer of the fixture that works on the backend.
func (f *analysisFixture) on(backend store.Engine) *rating.Analyzer {
	analyzer := *f.analyzer
	analyzer.Transactions, analyzer.MeterValues, analyzer.Assets, analyzer.Registry, analyzer.Tariffs = backend, backend, backend, backend, backend
	return &analyzer
}

// layeredSession is a session in which every store has a say: it drew 11 kW, 2 kW, 1 kW
// and 400 W for an hour each and then nothing for an hour, at the connector of 150 kW of
// a charge station with a capacity of 30 kW, and it is charged with a tariff version for
// which a transaction charges from 2 % of the highest power.
//
// With all of that it charged in the first hour only (2 % of 150 kW is 3 kW). Without
// the tariff it is 1 % of 150 kW (the second hour charges too), without the connector 2 %
// of the 30 kW of the charge station (the third hour too), and without the registry the
// minimum of 200 W (the fourth hour too). Without the meter values that were sent on
// their own only its start and its stop are known, 2.88 kW on average: below the 3 kW
// (it never charged), and above the 200 W of a backend with none of the stores (it
// charged throughout).
func layeredSession(t *testing.T, f *analysisFixture) {
	t.Helper()
	ctx := context.Background()
	tariff := &store.Tariff{OrgId: analysisOrg, PartyRef: "party-a", Name: "Regular"}
	require.NoError(t, f.engine.CreateTariff(ctx, tariff, tariffVersionWith(&store.IdleDetection{ChargingPowerShare: ptr(0.02)})))
	station, err := f.engine.LookupChargeStation(ctx, analysisStation)
	require.NoError(t, err)
	station.MaxCapacityKw = ptr(30.0)
	require.NoError(t, f.engine.UpdateChargeStation(ctx, station))

	f.session(t, analysisStation, "tx-1", 2, 0, 0, 60, 11, 120, 13, 180, 14, 240, 14.4, 300, 14.4)
	require.NoError(t, f.engine.SetTransactionTariff(ctx, analysisStation, "tx-1", &tariff.Id, ptr(1), "connector"))
}

// chargedFor is the analysis of a layeredSession that charged in its first hours.
func chargedFor(hours int64, averagePowerW float64) *store.TransactionAnalysis {
	return &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(float64(60 * hours))),
		IdleS:         (5 - hours) * 3600,
		ChargingS:     hours * 3600,
		AveragePowerW: &averagePowerW,
		AnalysedAt:    analysisTime,
	}
}

// When a store fails, the transaction gets no analysis: not one that was worked out
// without what could not be read.
func TestAnalyseStoresNothingWhenAStoreFails(t *testing.T) {
	errDown := errors.New("the store is down")

	for name, backend := range map[string]*failingBackend{
		"the transaction":           {find: errDown},
		"the meter values":          {query: errDown},
		"the tariff version":        {tariff: errDown},
		"the charge station":        {station: errDown},
		"the EVSE":                  {evse: errDown},
		"the write of the analysis": {set: errDown},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAnalysisFixture(t)
			layeredSession(t, f)
			backend.Engine = f.engine

			analysis, err := f.on(backend).AnalyseAndStore(context.Background(), analysisStation, "tx-1")

			require.ErrorIs(t, err, errDown)
			assert.ErrorContains(t, err, "analyse transaction aaaa.CP001/tx-1")
			assert.Nil(t, analysis)
			assert.Nil(t, f.stored(t, analysisStation, "tx-1"))
			if backend.set == nil {
				assert.Zero(t, backend.setCalls, "nothing is written")
			}
		})
	}
}

// A storage backend without one of the stores leaves what that store holds unknown: no
// meter values on their own, the settings of the manager, no highest power.
func TestAnalyseWithABackendWithoutSomeStores(t *testing.T) {
	notImplemented := fmt.Errorf("stub: %w", store.ErrNotImplemented)

	for name, tc := range map[string]struct {
		backend *failingBackend
		want    *store.TransactionAnalysis
	}{
		"everything is there":          {&failingBackend{}, chargedFor(1, 3600)},
		"no tariffs":                   {&failingBackend{tariff: notImplemented}, chargedFor(2, 3600)},
		"no asset model":               {&failingBackend{evse: notImplemented}, chargedFor(3, 3600)},
		"no registry":                  {&failingBackend{station: notImplemented}, chargedFor(4, 3600)},
		"no meter values on their own": {&failingBackend{query: notImplemented}, chargedFor(0, 2880)},
		"none of them": {
			&failingBackend{query: notImplemented, tariff: notImplemented, station: notImplemented, evse: notImplemented},
			chargedFor(5, 2880),
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAnalysisFixture(t)
			layeredSession(t, f)
			tc.backend.Engine = f.engine

			analysis, err := f.on(tc.backend).AnalyseAndStore(context.Background(), analysisStation, "tx-1")

			require.NoError(t, err)
			assert.Equal(t, tc.want, analysis)
			assert.Equal(t, tc.want, f.stored(t, analysisStation, "tx-1"))
		})
	}

	t.Run("no stores at all but the transactions", func(t *testing.T) {
		f := newAnalysisFixture(t)
		layeredSession(t, f)
		analyzer := &rating.Analyzer{Transactions: f.engine, Defaults: rating.DefaultIdleSettings()}

		analysis, err := analyzer.AnalyseAndStore(context.Background(), analysisStation, "tx-1")

		require.NoError(t, err)
		require.NotNil(t, analysis)
		assert.WithinDuration(t, time.Now(), analysis.AnalysedAt, time.Minute, "the real clock")
		want := chargedFor(5, 2880)
		want.AnalysedAt = analysis.AnalysedAt
		assert.Equal(t, want, analysis)
	})

	t.Run("a backend that keeps no analysis", func(t *testing.T) {
		f := newAnalysisFixture(t)
		layeredSession(t, f)
		backend := &failingBackend{Engine: f.engine, set: fmt.Errorf("SetTransactionAnalysis: %w", store.ErrNotImplemented)}

		analysis, err := f.on(backend).AnalyseAndStore(context.Background(), analysisStation, "tx-1")

		require.NoError(t, err)
		assert.Nil(t, analysis, "no analysis, and no error")
		assert.Equal(t, 1, backend.setCalls)
		assert.Nil(t, f.stored(t, analysisStation, "tx-1"))
	})
}

// Analyse works the analysis out without storing it.
func TestAnalyseDoesNotStore(t *testing.T) {
	ctx := context.Background()
	f := newAnalysisFixture(t)
	f.session(t, analysisStation, "tx-1", 1, 0, 0, 60, 11, 150, 11)
	transaction, err := f.engine.FindTransaction(ctx, analysisStation, "tx-1")
	require.NoError(t, err)

	analysis, err := f.analyzer.Analyse(ctx, transaction)

	require.NoError(t, err)
	assert.Equal(t, &store.TransactionAnalysis{
		IdleStartedAt: ptr(minute(60)), IdleS: 5400, ChargingS: 3600, AveragePowerW: ptr(11_000.0), AnalysedAt: analysisTime,
	}, analysis)
	assert.Nil(t, f.stored(t, analysisStation, "tx-1"))
}

// countingTx is a transactor that counts its database transactions.
type countingTx struct {
	store.Transactor
	runs int
}

func (c *countingTx) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	c.runs++
	return c.Transactor.RunInTx(ctx, fn)
}

// inTxReads is a store that counts the reads of a transaction inside and outside a
// database transaction.
type inTxReads struct {
	store.Engine
	inTx, outsideTx int
}

func (s *inTxReads) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	if eventstest.InTx(ctx) {
		s.inTx++
	} else {
		s.outsideTx++
	}
	return s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
}

// The analysis at the stop of a transaction reads and writes in one database transaction:
// on the primary, where the stop is, also while a read replica is behind.
func TestAnalyseAtStopRunsInADatabaseTransaction(t *testing.T) {
	f := newAnalysisFixture(t)
	f.session(t, analysisStation, "tx-1", 1, 0, 0, 60, 11, 150, 11)
	fixture := eventstest.New(f.engine, analysisOrg, clockTest.NewFakePassiveClock(analysisTime))
	tx := &countingTx{Transactor: fixture.Tx}
	backend := &inTxReads{Engine: f.engine}

	analysis, err := f.on(backend).AnalyseAtStop(context.Background(), tx, analysisStation, "tx-1")

	require.NoError(t, err)
	require.NotNil(t, analysis)
	assert.Equal(t, int64(5400), analysis.IdleS)
	assert.Equal(t, analysis, f.stored(t, analysisStation, "tx-1"))
	assert.Equal(t, 1, tx.runs)
	assert.Equal(t, 1, backend.inTx, "the transaction is read once, with its meter values")
	assert.Zero(t, backend.outsideTx)

	_, err = f.on(backend).AnalyseAtStop(context.Background(), tx, analysisStation, "tx-unknown")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// panickingTariffs is a tariff store whose lookup panics.
type panickingTariffs struct {
	store.TariffStore
}

func (panickingTariffs) LookupTariffVersion(context.Context, string, int) (*store.TariffVersion, error) {
	panic("tariff store broke")
}

// An analysis that panics is an analysis that failed: the handler that asked for it goes
// on.
func TestAnalyseAtStopSurvivesAPanic(t *testing.T) {
	f := newAnalysisFixture(t)
	layeredSession(t, f)
	f.analyzer.Tariffs = panickingTariffs{}

	analysis, err := f.analyzer.AnalyseAtStop(context.Background(), f.engine, analysisStation, "tx-1")

	require.ErrorContains(t, err, "analyse transaction aaaa.CP001/tx-1: panic: tariff store broke")
	assert.Nil(t, analysis)
	assert.Nil(t, f.stored(t, analysisStation, "tx-1"))
}

// slowTariffs is a tariff store whose lookup takes until the caller gives up.
type slowTariffs struct {
	store.TariffStore
}

func (slowTariffs) LookupTariffVersion(ctx context.Context, _ string, _ int) (*store.TariffVersion, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// The answer to a charge station does not wait long for the analysis of its transaction.
func TestAnalyseAtStopDoesNotWait(t *testing.T) {
	f := newAnalysisFixture(t)
	layeredSession(t, f)
	f.analyzer.Tariffs = slowTariffs{}

	begin := time.Now()
	analysis, err := f.analyzer.AnalyseAtStop(context.Background(), f.engine, analysisStation, "tx-1")

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, analysis)
	assert.Nil(t, f.stored(t, analysisStation, "tx-1"))
	assert.GreaterOrEqual(t, time.Since(begin), rating.AnalysisTimeout)
	assert.Less(t, time.Since(begin), rating.AnalysisTimeout+5*time.Second)

	// the caller that waits is not held up by it
	begin = time.Now()
	_, err = f.analyzer.AnalyseAndStore(contextThatEnds(t, 50*time.Millisecond), analysisStation, "tx-1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(begin), rating.AnalysisTimeout)
}

func contextThatEnds(t *testing.T, after time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), after)
	t.Cleanup(cancel)
	return ctx
}
