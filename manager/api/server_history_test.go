// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	"context"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/api"
	"github.com/thoughtworks/maeve-csms/manager/ocpi"
	"github.com/thoughtworks/maeve-csms/manager/store"
	"github.com/thoughtworks/maeve-csms/manager/store/inmemory"
	"github.com/thoughtworks/maeve-csms/manager/tenancy"
	clockTest "k8s.io/utils/clock/testing"
)

// The history of a charge station through the API: a session stays with the org that
// owned the charge station when it started, and the routes of a charge station show a
// caller only the sessions of the orgs that it may reach.

// The transactions of the history tests, and the times of their meter values.
const (
	historyTransactionOfA = "tx-of-a"
	historyTransactionOfB = "tx-of-b"

	historyTimeOfA          = "2026-01-02T01:00:00Z"
	historyTimeMainMeterOfA = "2026-01-02T01:10:00Z"
	historyTimeLateOfA      = "2026-01-02T02:00:00Z"
	historyTimeMainMeterOfB = "2026-01-02T02:10:00Z"
	historyTimeOfB          = "2026-01-02T03:00:00Z"
)

func storeHistoryMeterValue(t *testing.T, engine store.Engine, csId string, evseId int, transactionId, timestamp string) {
	t.Helper()
	require.NoError(t, engine.StoreMeterValues(context.Background(), csId, evseId, transactionId, []store.MeterValue{{
		Timestamp:     timestamp,
		SampledValues: []store.SampledValue{{Value: 1500}},
	}}))
}

// newTransferredStation is a multi mode manager whose charge station stationOfA moved
// from the tenant A to the tenant B while a transaction of A ran on it. A has a
// transaction with a meter value before the move and one after it, and a meter value of
// the main meter; B has a meter value of the main meter and a transaction with a meter
// value.
func newTransferredStation(t *testing.T) *chi.Mux {
	t.Helper()
	ctx := context.Background()
	m := newMultiTenant(t)

	require.NoError(t, m.engine.CreateTransaction(ctx, stationOfA, historyTransactionOfA, "TAGA", "ISO14443", nil, 0, false))
	storeHistoryMeterValue(t, m.engine, stationOfA, 1, historyTransactionOfA, historyTimeOfA)
	storeHistoryMeterValue(t, m.engine, stationOfA, 0, "", historyTimeMainMeterOfA)

	_, err := m.engine.TransferChargeStation(ctx, store.TransferParams{StationKey: stationOfA, FromOrgId: orgA, ToOrgId: orgB})
	require.NoError(t, err)

	storeHistoryMeterValue(t, m.engine, stationOfA, 1, historyTransactionOfA, historyTimeLateOfA)
	storeHistoryMeterValue(t, m.engine, stationOfA, 0, "", historyTimeMainMeterOfB)
	require.NoError(t, m.engine.CreateTransaction(ctx, stationOfA, historyTransactionOfB, "TAGB", "ISO14443", nil, 0, false))
	storeHistoryMeterValue(t, m.engine, stationOfA, 2, historyTransactionOfB, historyTimeOfB)

	return m.router(t)
}

// listedTransactionIds returns the ids of the transactions that the list of the charge
// station answers the caller with, sorted, and its total.
func listedTransactionIds(t *testing.T, r http.Handler, p *tenancy.Principal, csId, query string) ([]string, int64) {
	t.Helper()
	rr := serve(r, as(p, http.MethodGet, "/cs/"+csId+"/transactions"+query, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	list := decode[api.TransactionList](t, rr)
	ids := []string{}
	for _, transaction := range list.Transactions {
		ids = append(ids, transaction.TransactionId)
	}
	sort.Strings(ids)
	return ids, list.Total
}

// listedMeterValueTimes returns the times of the meter values that the charge station
// answers the caller with, newest first, and their total.
func listedMeterValueTimes(t *testing.T, r http.Handler, p *tenancy.Principal, csId, query string) ([]string, int) {
	t.Helper()
	rr := serve(r, as(p, http.MethodGet, "/cs/"+csId+"/meter-values"+query, ""))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	response := decode[api.MeterValuesResponse](t, rr)
	times := []string{}
	for _, mv := range response.MeterValues {
		times = append(times, mv.Timestamp.UTC().Format("2006-01-02T15:04:05Z"))
	}
	return times, response.Total
}

func TestStationHistoryShowsTheSessionsOfTheOrgsOfTheCaller(t *testing.T) {
	r := newTransferredStation(t)
	oldOwner := callerOf(orgA, tenancy.OrgScopeSelf)
	newOwner := callerOf(orgB, tenancy.OrgScopeSelf)
	partner := callerOf(orgPartner, tenancy.OrgScopeSubtree)
	transaction := func(p *tenancy.Principal, transactionId string) *http.Request {
		return as(p, http.MethodGet, "/cs/"+stationOfA+"/transaction/"+transactionId, "")
	}

	t.Run("the new owner sees its own transactions", func(t *testing.T) {
		ids, total := listedTransactionIds(t, r, newOwner, stationOfA, "")
		assert.Equal(t, []string{historyTransactionOfB}, ids)
		assert.Equal(t, int64(1), total, "the total counts what the caller may see")

		ids, total = listedTransactionIds(t, r, newOwner, stationOfA, "?status=active")
		assert.Equal(t, []string{historyTransactionOfB}, ids)
		assert.Equal(t, int64(1), total)
		ids, total = listedTransactionIds(t, r, newOwner, stationOfA, "?limit=1&offset=1")
		assert.Empty(t, ids)
		assert.Equal(t, int64(1), total)

		rr := serve(r, transaction(newOwner, historyTransactionOfB))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, historyTransactionOfB, decode[api.TransactionDetail](t, rr).TransactionId)
	})

	t.Run("a transaction of the old owner is answered like one that does not exist", func(t *testing.T) {
		unknown := serve(r, transaction(newOwner, "tx-unknown"))
		require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())
		ofOldOwner := serve(r, transaction(newOwner, historyTransactionOfA))
		assert.Equal(t, unknown.Code, ofOldOwner.Code)
		assert.Equal(t, unknown.Body.String(), ofOldOwner.Body.String())
		assert.Equal(t, unknown.Header(), ofOldOwner.Header())
	})

	t.Run("the new owner sees its own meter values", func(t *testing.T) {
		times, total := listedMeterValueTimes(t, r, newOwner, stationOfA, "")
		assert.Equal(t, []string{historyTimeOfB, historyTimeMainMeterOfB}, times)
		assert.Equal(t, 2, total, "the total counts what the caller may see")

		// the meter values of a transaction of the old owner, also the one that came
		// after the move, are not shown
		times, total = listedMeterValueTimes(t, r, newOwner, stationOfA, "?transactionId="+historyTransactionOfA)
		assert.Empty(t, times)
		assert.Zero(t, total)
		times, _ = listedMeterValueTimes(t, r, newOwner, stationOfA, "?connectorId=0")
		assert.Equal(t, []string{historyTimeMainMeterOfB}, times)
	})

	t.Run("the old owner does not reach the charge station any more", func(t *testing.T) {
		for _, target := range []string{"/transactions", "/transaction/" + historyTransactionOfA, "/meter-values"} {
			rr := serve(r, as(oldOwner, http.MethodGet, "/cs/"+stationOfA+target, ""))
			assert.Equal(t, http.StatusNotFound, rr.Code, target)
			assert.JSONEq(t, notFoundBody, rr.Body.String(), target)
		}
	})

	t.Run("a caller that reaches both orgs sees the sessions of both", func(t *testing.T) {
		ids, total := listedTransactionIds(t, r, partner, stationOfA, "")
		assert.Equal(t, []string{historyTransactionOfA, historyTransactionOfB}, ids)
		assert.Equal(t, int64(2), total)

		rr := serve(r, transaction(partner, historyTransactionOfA))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Equal(t, historyTransactionOfA, decode[api.TransactionDetail](t, rr).TransactionId)

		times, meterValues := listedMeterValueTimes(t, r, partner, stationOfA, "")
		assert.Equal(t, []string{historyTimeOfB, historyTimeMainMeterOfB, historyTimeLateOfA, historyTimeMainMeterOfA, historyTimeOfA}, times)
		assert.Equal(t, 5, meterValues)
	})
}

// A charge station that never moved has one history: its owner sees all of it.
func TestStationHistoryOfAChargeStationThatDidNotMove(t *testing.T) {
	ctx := context.Background()
	m := newMultiTenant(t)
	require.NoError(t, m.engine.CreateTransaction(ctx, stationOfB, "tx-1", "TAGB", "ISO14443", nil, 0, false))
	require.NoError(t, m.engine.CreateTransaction(ctx, stationOfB, "tx-2", "TAGB", "ISO14443", nil, 0, false))
	storeHistoryMeterValue(t, m.engine, stationOfB, 1, "tx-1", historyTimeOfB)
	storeHistoryMeterValue(t, m.engine, stationOfB, 0, "", historyTimeMainMeterOfB)
	r := m.router(t)
	owner := callerOf(orgB, tenancy.OrgScopeSelf)

	ids, total := listedTransactionIds(t, r, owner, stationOfB, "")
	assert.Equal(t, []string{"tx-1", "tx-2"}, ids)
	assert.Equal(t, int64(2), total)
	times, meterValues := listedMeterValueTimes(t, r, owner, stationOfB, "")
	assert.Equal(t, []string{historyTimeOfB, historyTimeMainMeterOfB}, times)
	assert.Equal(t, 2, meterValues)
}

// In single mode there is one org: the caller sees the whole history of a charge
// station, also what was stored before the charge station was registered (the legacy
// org, whose records belong to the default org).
func TestStationHistoryInSingleModeIsTheWholeHistory(t *testing.T) {
	server, r, engine, _ := setupServer(t)
	defer server.Close()
	ctx := context.Background()

	require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx-legacy", "TAG", "ISO14443", nil, 0, false))
	storeHistoryMeterValue(t, engine, "cs001", 1, "tx-legacy", historyTimeOfA)
	storeHistoryMeterValue(t, engine, "cs001", 0, "", historyTimeMainMeterOfA)
	registerStation(t, engine, "cs001")
	require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx-default", "TAG", "ISO14443", nil, 0, false))
	storeHistoryMeterValue(t, engine, "cs001", 1, "tx-default", historyTimeOfB)
	storeHistoryMeterValue(t, engine, "cs001", 0, "", historyTimeMainMeterOfB)

	stored, err := engine.FindTransaction(ctx, "cs001", "tx-legacy")
	require.NoError(t, err)
	require.Equal(t, store.LegacyOrgId, stored.OrgId)
	stored, err = engine.FindTransaction(ctx, "cs001", "tx-default")
	require.NoError(t, err)
	require.Equal(t, testOrgId, stored.OrgId)

	list := new(api.TransactionList)
	getJson(t, r, "/cs/cs001/transactions", list)
	assert.Equal(t, int64(2), list.Total)
	assert.Len(t, list.Transactions, 2)
	for _, transactionId := range []string{"tx-legacy", "tx-default"} {
		detail := new(api.TransactionDetail)
		getJson(t, r, "/cs/cs001/transaction/"+transactionId, detail)
		assert.Equal(t, transactionId, detail.TransactionId)
	}
	meterValues := new(api.MeterValuesResponse)
	getJson(t, r, "/cs/cs001/meter-values", meterValues)
	assert.Equal(t, 4, meterValues.Total)
	assert.Len(t, meterValues.MeterValues, 4)
}

// orglessStore reads transactions without their org and lists the transactions of every
// org, as a store that does not keep the org of a transaction does (Firestore).
type orglessStore struct {
	store.Engine
}

func (s orglessStore) FindTransaction(ctx context.Context, chargeStationId, transactionId string) (*store.Transaction, error) {
	found, err := s.Engine.FindTransaction(ctx, chargeStationId, transactionId)
	if err != nil || found == nil {
		return found, err
	}
	withoutOrg := *found
	withoutOrg.OrgId = ""
	return &withoutOrg, nil
}

func (s orglessStore) ListTransactionsForChargeStation(ctx context.Context, chargeStationId, status string, startDate, endDate *time.Time, limit, offset int, _ []string) ([]*store.Transaction, int64, error) {
	return s.Engine.ListTransactionsForChargeStation(ctx, chargeStationId, status, startDate, endDate, limit, offset, nil)
}

// A store that does not keep the org of a transaction has one history for a charge
// station: every caller that reaches the charge station reads all of it.
func TestStationHistoryOfAStoreWithoutOrgs(t *testing.T) {
	ctx := context.Background()
	c := clockTest.NewFakePassiveClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	engine := inmemory.NewStore(c)
	registerStation(t, engine, "cs001")
	require.NoError(t, engine.CreateTransaction(ctx, "cs001", "tx-1", "TAG", "ISO14443", nil, 0, false))
	r := newSingleTenantRouter(t, orglessStore{Engine: engine}, c, ocpi.NewOCPI(engine, nil, "GB", "TWK", testOrgId))

	list := new(api.TransactionList)
	getJson(t, r, "/cs/cs001/transactions", list)
	assert.Equal(t, int64(1), list.Total)
	detail := new(api.TransactionDetail)
	getJson(t, r, "/cs/cs001/transaction/tx-1", detail)
	assert.Equal(t, "tx-1", detail.TransactionId)
}
