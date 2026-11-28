// SPDX-License-Identifier: Apache-2.0

package storetest

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// The transactions and the meter values of the history fixture. A meter value is named
// by its time.
const (
	historyEndedOfA   = "tx-a1"
	historyRunningOfA = "tx-a2"
	historyOfB        = "tx-b1"
	historyUnknown    = "tx-unknown"

	historyTimeEndedOfA        = "2026-11-28T09:10:00Z"
	historyTimeMainMeterOfA    = "2026-11-28T09:20:00Z"
	historyTimeRunningBefore   = "2026-11-28T10:10:00Z"
	historyTimeRunningAfter    = "2026-11-28T10:20:00Z"
	historyTimeMainMeterOfB    = "2026-11-28T10:30:00Z"
	historyTimeUnknownAfter    = "2026-11-28T10:40:00Z"
	historyTimeOfB             = "2026-11-28T11:10:00Z"
	historyTimeLateForRunning  = "2026-11-28T11:20:00Z"
	historyTimeLateForEndedOfA = "2026-11-28T11:30:00Z"
)

func historyMeterValue(timestamp string, wh float64) []store.MeterValue {
	measurand := "Energy.Active.Import.Register"
	return []store.MeterValue{{
		Timestamp:     timestamp,
		SampledValues: []store.SampledValue{{Measurand: &measurand, Value: wh}},
	}}
}

// newHistoryFixture stores the history of a charge station that moves from orgA to orgB
// while a transaction runs on it:
//
//   - before the move: a transaction that has ended and one that runs, each with a meter
//     value, and a meter value of the main meter (without a transaction);
//   - after the move: a meter value of the transaction that still runs, one of the main
//     meter, one of a transaction that is not known, a transaction of orgB with a meter
//     value, and one more meter value of each transaction of orgA.
func newHistoryFixture(t *testing.T, s store.Engine) {
	t.Helper()
	ctx := context.Background()
	storeMeterValue := func(evseId int, transactionId, timestamp string) {
		t.Helper()
		require.NoError(t, s.StoreMeterValues(ctx, assetStationA, evseId, transactionId, historyMeterValue(timestamp, 1500)))
	}

	registerStation(t, s, assetStationA, orgA)
	require.NoError(t, s.CreateTransaction(ctx, assetStationA, historyEndedOfA, "TAG1", "ISO14443", historyMeterValue("2026-11-28T09:00:00Z", 1000), 0, false))
	storeMeterValue(1, historyEndedOfA, historyTimeEndedOfA)
	require.NoError(t, s.EndTransaction(ctx, assetStationA, historyEndedOfA, "TAG1", "ISO14443", historyMeterValue("2026-11-28T09:30:00Z", 2000), 1))
	storeMeterValue(0, "", historyTimeMainMeterOfA)
	require.NoError(t, s.CreateTransaction(ctx, assetStationA, historyRunningOfA, "TAG1", "ISO14443", historyMeterValue("2026-11-28T10:00:00Z", 2000), 0, false))
	storeMeterValue(1, historyRunningOfA, historyTimeRunningBefore)

	_, err := s.TransferChargeStation(ctx, store.TransferParams{StationKey: assetStationA, FromOrgId: orgA, ToOrgId: orgB})
	require.NoError(t, err)

	storeMeterValue(1, historyRunningOfA, historyTimeRunningAfter)
	storeMeterValue(0, "", historyTimeMainMeterOfB)
	storeMeterValue(2, historyUnknown, historyTimeUnknownAfter)
	require.NoError(t, s.CreateTransaction(ctx, assetStationA, historyOfB, "TAG2", "ISO14443", historyMeterValue("2026-11-28T11:00:00Z", 3000), 0, false))
	storeMeterValue(2, historyOfB, historyTimeOfB)
	storeMeterValue(1, historyRunningOfA, historyTimeLateForRunning)
	storeMeterValue(1, historyEndedOfA, historyTimeLateForEndedOfA)
}

// listedTransactions lists the transactions of the charge station of the fixture with the
// status that belong to the orgs (every org without one) and returns their ids, sorted,
// and the total.
func listedTransactions(t *testing.T, s store.Engine, status string, orgIds ...string) ([]string, int64) {
	t.Helper()
	transactions, total, err := s.ListTransactionsForChargeStation(context.Background(), assetStationA, status, nil, nil, 50, 0, orgIds)
	require.NoError(t, err)
	ids := []string{}
	for _, transaction := range transactions {
		ids = append(ids, transaction.TransactionId)
	}
	sort.Strings(ids)
	return ids, total
}

// queriedMeterValues returns the times of the meter values that the filter selects, in
// the order of the result (newest first), and the total.
func queriedMeterValues(t *testing.T, s store.Engine, filter store.MeterValuesFilter) ([]string, int) {
	t.Helper()
	filter.ChargeStationId = assetStationA
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	result, err := s.QueryMeterValues(context.Background(), filter)
	require.NoError(t, err)
	times := []string{}
	for _, mv := range result.MeterValues {
		times = append(times, mv.MeterValue.Timestamp)
	}
	return times, result.Total
}

// TestHistoryPerOrg is the contract of the org of the history of a charge station. A
// transaction belongs to the org that owned the charge station when it was created, a
// meter value to the org of its transaction or, without a known transaction, to the org
// that owned the charge station when it was stored. They keep that org when the charge
// station moves to another org, and the lists can be narrowed to the records of orgs.
func TestHistoryPerOrg(t *testing.T, newStore func(t *testing.T) store.Engine) {
	ctx := context.Background()

	t.Run("a transaction keeps its org when the charge station moves", func(t *testing.T) {
		s := newStore(t)
		newHistoryFixture(t, s)

		for transactionId, orgId := range map[string]string{historyEndedOfA: orgA, historyRunningOfA: orgA, historyOfB: orgB} {
			transaction, err := s.FindTransaction(ctx, assetStationA, transactionId)
			require.NoError(t, err)
			require.NotNil(t, transaction, transactionId)
			assert.Equal(t, orgId, transaction.OrgId, transactionId)
		}

		// ending the transaction and writing its details do not change its org
		stopReason := "Remote"
		require.NoError(t, s.EndTransaction(ctx, assetStationA, historyRunningOfA, "TAG1", "ISO14443", historyMeterValue("2026-11-28T11:40:00Z", 4000), 1))
		require.NoError(t, s.UpdateTransactionDetails(ctx, assetStationA, historyRunningOfA, &store.TransactionDetails{StopReason: &stopReason}))
		transaction, err := s.FindTransaction(ctx, assetStationA, historyRunningOfA)
		require.NoError(t, err)
		require.NotNil(t, transaction)
		assert.Equal(t, orgA, transaction.OrgId)
		assert.Equal(t, &stopReason, transaction.StopReason)
	})

	t.Run("the transactions of a charge station are listed for orgs", func(t *testing.T) {
		s := newStore(t)
		newHistoryFixture(t, s)

		for name, tc := range map[string]struct {
			status string
			orgIds []string
			want   []string
		}{
			"every org":                   {"all", nil, []string{historyEndedOfA, historyRunningOfA, historyOfB}},
			"no org is every org":         {"", []string{}, []string{historyEndedOfA, historyRunningOfA, historyOfB}},
			"the old owner":               {"all", []string{orgA}, []string{historyEndedOfA, historyRunningOfA}},
			"the new owner":               {"all", []string{orgB}, []string{historyOfB}},
			"both":                        {"", []string{orgB, orgA}, []string{historyEndedOfA, historyRunningOfA, historyOfB}},
			"an org without transactions": {"all", []string{orgC}, []string{}},
			"an org id in upper case":     {"all", []string{"0193B2C4-0000-7000-8000-0000000000A1"}, []string{historyEndedOfA, historyRunningOfA}},
			"active, the old owner":       {"active", []string{orgA}, []string{historyRunningOfA}},
			"completed, the old owner":    {"completed", []string{orgA}, []string{historyEndedOfA}},
			"active, the new owner":       {"active", []string{orgB}, []string{historyOfB}},
			"completed, the new owner":    {"completed", []string{orgB}, []string{}},
			"the legacy org":              {"all", []string{store.LegacyOrgId}, []string{}},
		} {
			ids, total := listedTransactions(t, s, tc.status, tc.orgIds...)
			assert.Equal(t, tc.want, ids, name)
			assert.Equal(t, int64(len(tc.want)), total, "the total counts the transactions of the orgs: %s", name)
		}

		_, _, err := s.ListTransactionsForChargeStation(ctx, assetStationA, "all", nil, nil, 50, 0, []string{"not-a-uuid"})
		assert.Error(t, err, "an org id is a UUID")
	})

	t.Run("a page of the transactions of an org", func(t *testing.T) {
		s := newStore(t)
		newHistoryFixture(t, s)

		// the two transactions of the old owner, one on a page: the transaction of the
		// new owner is on no page and is not counted
		for offset, wantLen := range []int{1, 1, 0} {
			page, total, err := s.ListTransactionsForChargeStation(ctx, assetStationA, "all", nil, nil, 1, offset, []string{orgA})
			require.NoError(t, err)
			assert.Equal(t, int64(2), total, "the total does not depend on the page")
			require.Len(t, page, wantLen, "offset %d", offset)
			for _, transaction := range page {
				assert.Equal(t, orgA, transaction.OrgId)
				assert.Contains(t, []string{historyEndedOfA, historyRunningOfA}, transaction.TransactionId)
			}
		}

		page, total, err := s.ListTransactionsForChargeStation(ctx, assetStationA, "all", nil, nil, 50, 0, []string{orgB})
		require.NoError(t, err)
		assert.Equal(t, int64(1), total)
		require.Len(t, page, 1)
		assert.Equal(t, historyOfB, page[0].TransactionId)
		assert.Equal(t, orgB, page[0].OrgId)
	})

	t.Run("a meter value belongs to the org of its transaction, or of the charge station", func(t *testing.T) {
		s := newStore(t)
		newHistoryFixture(t, s)
		ofA := []string{historyTimeLateForEndedOfA, historyTimeLateForRunning, historyTimeRunningAfter, historyTimeRunningBefore, historyTimeMainMeterOfA, historyTimeEndedOfA}
		ofB := []string{historyTimeOfB, historyTimeUnknownAfter, historyTimeMainMeterOfB}
		all := []string{
			historyTimeLateForEndedOfA, historyTimeLateForRunning, historyTimeOfB, historyTimeUnknownAfter, historyTimeMainMeterOfB,
			historyTimeRunningAfter, historyTimeRunningBefore, historyTimeMainMeterOfA, historyTimeEndedOfA,
		}
		mainMeter := 0

		for name, tc := range map[string]struct {
			filter store.MeterValuesFilter
			want   []string
		}{
			"every org":           {store.MeterValuesFilter{}, all},
			"no org is every org": {store.MeterValuesFilter{OrgIds: []string{}}, all},
			// the meter values of a transaction follow the transaction, also the ones
			// that arrive after the charge station has moved
			"the old owner": {store.MeterValuesFilter{OrgIds: []string{orgA}}, ofA},
			// a meter value without a known transaction belongs to the owner of the
			// charge station at that moment
			"the new owner":           {store.MeterValuesFilter{OrgIds: []string{orgB}}, ofB},
			"both":                    {store.MeterValuesFilter{OrgIds: []string{orgA, orgB}}, all},
			"another org":             {store.MeterValuesFilter{OrgIds: []string{orgC}}, []string{}},
			"an org id in upper case": {store.MeterValuesFilter{OrgIds: []string{"0193B2C4-0000-7000-8000-0000000000B2"}}, ofB},
			"the main meter, the old owner": {
				store.MeterValuesFilter{OrgIds: []string{orgA}, ConnectorId: &mainMeter}, []string{historyTimeMainMeterOfA},
			},
			"the main meter, the new owner": {
				store.MeterValuesFilter{OrgIds: []string{orgB}, ConnectorId: &mainMeter}, []string{historyTimeMainMeterOfB},
			},
			"a transaction of the old owner, asked by the old owner": {
				store.MeterValuesFilter{OrgIds: []string{orgA}, TransactionId: ptr(historyRunningOfA)},
				[]string{historyTimeLateForRunning, historyTimeRunningAfter, historyTimeRunningBefore},
			},
			"a transaction of the old owner, asked by the new owner": {
				store.MeterValuesFilter{OrgIds: []string{orgB}, TransactionId: ptr(historyRunningOfA)}, []string{},
			},
			"a transaction that is not known, asked by the old owner": {
				store.MeterValuesFilter{OrgIds: []string{orgA}, TransactionId: ptr(historyUnknown)}, []string{},
			},
			"a time range, the new owner": {
				store.MeterValuesFilter{OrgIds: []string{orgB}, StartTime: ptr(historyTimeRunningAfter), EndTime: ptr(historyTimeUnknownAfter)},
				[]string{historyTimeUnknownAfter, historyTimeMainMeterOfB},
			},
		} {
			times, total := queriedMeterValues(t, s, tc.filter)
			assert.Equal(t, tc.want, times, name)
			assert.Equal(t, len(tc.want), total, "the total counts the meter values of the orgs: %s", name)
		}

		// a page of the meter values of an org
		times, total := queriedMeterValues(t, s, store.MeterValuesFilter{OrgIds: []string{orgA}, Limit: 2, Offset: 4})
		assert.Equal(t, ofA[4:], times)
		assert.Equal(t, len(ofA), total)

		_, err := s.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: assetStationA, OrgIds: []string{"not-a-uuid"}, Limit: 50})
		assert.Error(t, err, "an org id is a UUID")
	})

	t.Run("the history of a charge station that is not registered belongs to the legacy org", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.CreateTransaction(ctx, assetStationA, "tx-1", "TAG1", "ISO14443", nil, 0, false))
		require.NoError(t, s.StoreMeterValues(ctx, assetStationA, 1, "tx-1", historyMeterValue(historyTimeEndedOfA, 1500)))
		require.NoError(t, s.StoreMeterValues(ctx, assetStationA, 0, "", historyMeterValue(historyTimeMainMeterOfA, 1500)))
		// once the charge station is registered, what is stored without a transaction is
		// of its org; the meter values of the transaction stay with the transaction
		registerStation(t, s, assetStationA, orgA)
		require.NoError(t, s.StoreMeterValues(ctx, assetStationA, 1, "tx-1", historyMeterValue(historyTimeRunningBefore, 1500)))
		require.NoError(t, s.StoreMeterValues(ctx, assetStationA, 0, "", historyMeterValue(historyTimeRunningAfter, 1500)))

		ids, total := listedTransactions(t, s, "all", store.LegacyOrgId)
		assert.Equal(t, []string{"tx-1"}, ids)
		assert.Equal(t, int64(1), total)
		ids, _ = listedTransactions(t, s, "all", orgA)
		assert.Empty(t, ids)

		times, _ := queriedMeterValues(t, s, store.MeterValuesFilter{OrgIds: []string{store.LegacyOrgId}})
		assert.Equal(t, []string{historyTimeRunningBefore, historyTimeMainMeterOfA, historyTimeEndedOfA}, times)
		times, _ = queriedMeterValues(t, s, store.MeterValuesFilter{OrgIds: []string{orgA}})
		assert.Equal(t, []string{historyTimeRunningAfter}, times)
	})
}
