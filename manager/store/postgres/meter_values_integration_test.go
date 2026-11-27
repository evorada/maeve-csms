// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

const (
	meterValuesTx1 = "00000000-0000-0000-0000-00000000002a"
	meterValuesTx2 = "00000000-0000-0000-0000-00000000002b"
)

// meterValueTimestamps returns the timestamps of the result, in result order.
func meterValueTimestamps(result *store.MeterValuesResult) []string {
	timestamps := make([]string, 0, len(result.MeterValues))
	for _, mv := range result.MeterValues {
		timestamps = append(timestamps, mv.MeterValue.Timestamp)
	}
	return timestamps
}

func TestMeterValues_StoreWithAndWithoutTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	withTransaction := store.MeterValue{
		Timestamp: "2026-11-26T10:01:00Z",
		SampledValues: []store.SampledValue{
			{
				Context:       ptr("Sample.Periodic"),
				Location:      ptr("Outlet"),
				Measurand:     ptr("Energy.Active.Import.Register"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "Wh", Multipler: 0},
				Value:         1234.5,
			},
			{
				Context:       ptr("Sample.Periodic"),
				Location:      ptr("Outlet"),
				Measurand:     ptr("Power.Active.Import"),
				Phase:         ptr("L1"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "W", Multipler: 0},
				Value:         7400,
			},
			{
				Context:       ptr("Sample.Periodic"),
				Location:      ptr("EV"),
				Measurand:     ptr("SoC"),
				UnitOfMeasure: &store.UnitOfMeasure{Unit: "Percent", Multipler: 0},
				Value:         55,
			},
		},
	}
	withoutTransaction := energyMeterValue("2026-11-26T10:00:00Z", 99)

	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{withTransaction}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 0, "", []store.MeterValue{withoutTransaction}))

	got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: "cs001", Limit: 100})
	require.NoError(t, err)

	// newest first
	want := []store.StoredMeterValue{
		{ChargeStationId: "cs001", EvseId: 1, TransactionId: meterValuesTx1, MeterValue: withTransaction},
		{ChargeStationId: "cs001", EvseId: 0, TransactionId: "", MeterValue: withoutTransaction},
	}
	assert.Equal(t, want, got.MeterValues)
	assert.Equal(t, 2, got.Total)
}

func TestMeterValues_StoreSeveralInOneCall(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{
		energyMeterValue("2026-11-26T10:00:00Z", 100),
		energyMeterValue("2026-11-26T10:01:00Z", 200),
	})
	require.NoError(t, err)

	got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: "cs001", Limit: 100})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-11-26T10:01:00Z", "2026-11-26T10:00:00Z"}, meterValueTimestamps(got))
	assert.Equal(t, 2, got.Total)
}

func TestMeterValues_QueryByConnector(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 0, "", []store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 1)}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{energyMeterValue("2026-11-26T10:01:00Z", 2)}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 2, meterValuesTx2, []store.MeterValue{energyMeterValue("2026-11-26T10:02:00Z", 3)}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs002", 1, "", []store.MeterValue{energyMeterValue("2026-11-26T10:03:00Z", 4)}))

	tests := []struct {
		name        string
		connectorId *int
		want        []string
	}{
		{name: "no connector filter returns every connector of the station", connectorId: nil, want: []string{"2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z", "2026-11-26T10:00:00Z"}},
		{name: "connector 0 is the main meter, not no filter", connectorId: ptr(0), want: []string{"2026-11-26T10:00:00Z"}},
		{name: "connector 1", connectorId: ptr(1), want: []string{"2026-11-26T10:01:00Z"}},
		{name: "connector 2", connectorId: ptr(2), want: []string{"2026-11-26T10:02:00Z"}},
		{name: "unknown connector", connectorId: ptr(9), want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{
				ChargeStationId: "cs001",
				ConnectorId:     tt.connectorId,
				Limit:           100,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, meterValueTimestamps(got))
			assert.Equal(t, len(tt.want), got.Total)
			for _, mv := range got.MeterValues {
				assert.Equal(t, "cs001", mv.ChargeStationId)
				if tt.connectorId != nil {
					assert.Equal(t, *tt.connectorId, mv.EvseId)
				}
			}
		})
	}
}

func TestMeterValues_QueryByTransaction(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 0, "", []store.MeterValue{energyMeterValue("2026-11-26T10:00:00Z", 1)}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{
		energyMeterValue("2026-11-26T10:01:00Z", 2),
		energyMeterValue("2026-11-26T10:02:00Z", 3),
	}))
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 2, meterValuesTx2, []store.MeterValue{energyMeterValue("2026-11-26T10:03:00Z", 4)}))
	// the same transaction id on another station is another transaction
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs002", 1, meterValuesTx1, []store.MeterValue{energyMeterValue("2026-11-26T10:04:00Z", 5)}))

	tests := []struct {
		name          string
		transactionId *string
		connectorId   *int
		want          []string
	}{
		{name: "transaction on connector 1", transactionId: ptr(meterValuesTx1), want: []string{"2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z"}},
		{name: "transaction on connector 2", transactionId: ptr(meterValuesTx2), want: []string{"2026-11-26T10:03:00Z"}},
		{name: "transaction and matching connector", transactionId: ptr(meterValuesTx1), connectorId: ptr(1), want: []string{"2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z"}},
		{name: "transaction and other connector", transactionId: ptr(meterValuesTx1), connectorId: ptr(2), want: []string{}},
		{name: "unknown transaction", transactionId: ptr("00000000-0000-0000-0000-0000000000ff"), want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{
				ChargeStationId: "cs001",
				ConnectorId:     tt.connectorId,
				TransactionId:   tt.transactionId,
				Limit:           100,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, meterValueTimestamps(got))
			assert.Equal(t, len(tt.want), got.Total)
			for _, mv := range got.MeterValues {
				assert.Equal(t, *tt.transactionId, mv.TransactionId)
			}
		})
	}
}

func TestMeterValues_QueryByTimeRange(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for minute := 0; minute < 5; minute++ {
		timestamp := fmt.Sprintf("2026-11-26T10:%02d:00Z", minute)
		require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{energyMeterValue(timestamp, float64(minute))}))
	}

	tests := []struct {
		name      string
		startTime *string
		endTime   *string
		want      []string
	}{
		{name: "start and end are inclusive", startTime: ptr("2026-11-26T10:01:00Z"), endTime: ptr("2026-11-26T10:03:00Z"), want: []string{"2026-11-26T10:03:00Z", "2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z"}},
		{name: "start only", startTime: ptr("2026-11-26T10:03:00Z"), want: []string{"2026-11-26T10:04:00Z", "2026-11-26T10:03:00Z"}},
		{name: "end only", endTime: ptr("2026-11-26T10:00:30Z"), want: []string{"2026-11-26T10:00:00Z"}},
		{name: "range with a UTC offset is compared as an instant", startTime: ptr("2026-11-26T12:01:00+02:00"), endTime: ptr("2026-11-26T12:02:00+02:00"), want: []string{"2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z"}},
		{name: "empty range", startTime: ptr("2026-11-26T11:00:00Z"), want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{
				ChargeStationId: "cs001",
				StartTime:       tt.startTime,
				EndTime:         tt.endTime,
				Limit:           100,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, meterValueTimestamps(got))
			assert.Equal(t, len(tt.want), got.Total)
		})
	}
}

func TestMeterValues_TotalIgnoresPagination(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	for minute := 0; minute < 5; minute++ {
		timestamp := fmt.Sprintf("2026-11-26T10:%02d:00Z", minute)
		require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, meterValuesTx1, []store.MeterValue{energyMeterValue(timestamp, float64(minute))}))
	}
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 2, "", []store.MeterValue{energyMeterValue("2026-11-26T09:00:00Z", 0)}))

	tests := []struct {
		name   string
		limit  int
		offset int
		want   []string
	}{
		{name: "first page", limit: 2, offset: 0, want: []string{"2026-11-26T10:04:00Z", "2026-11-26T10:03:00Z"}},
		{name: "second page", limit: 2, offset: 2, want: []string{"2026-11-26T10:02:00Z", "2026-11-26T10:01:00Z"}},
		{name: "last page is short", limit: 2, offset: 4, want: []string{"2026-11-26T10:00:00Z"}},
		{name: "offset past the end", limit: 2, offset: 10, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{
				ChargeStationId: "cs001",
				TransactionId:   ptr(meterValuesTx1),
				Limit:           tt.limit,
				Offset:          tt.offset,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, meterValueTimestamps(got))
			assert.Equal(t, 5, got.Total, "total counts every match of the filter, not the page")
		})
	}
}

func TestMeterValues_TimestampWithOffsetIsStoredAsInstant(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	// 12:00 at +02:00 is 10:00 UTC
	require.NoError(t, testStore.StoreMeterValues(ctx, "cs001", 1, "", []store.MeterValue{energyMeterValue("2026-11-26T12:00:00+02:00", 1)}))

	got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: "cs001", Limit: 100})
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-11-26T10:00:00Z"}, meterValueTimestamps(got))

	got, err = testStore.QueryMeterValues(ctx, store.MeterValuesFilter{
		ChargeStationId: "cs001",
		StartTime:       ptr("2026-11-26T09:59:00Z"),
		EndTime:         ptr("2026-11-26T10:01:00Z"),
		Limit:           100,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, got.Total)
}

func TestMeterValues_QueryUnknownStation(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	got, err := testStore.QueryMeterValues(ctx, store.MeterValuesFilter{ChargeStationId: "unknown", Limit: 100})
	require.NoError(t, err)
	assert.Empty(t, got.MeterValues)
	assert.Equal(t, 0, got.Total)
}
