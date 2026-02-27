// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

func TestDiagnosticsRequest_SetGetDelete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	request := &store.DiagnosticsRequest{
		ChargeStationId: "cs001",
		Location:        "ftp://example.com/diag",
		SendAfter:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetDiagnosticsRequest(ctx, "cs001", request)
	require.NoError(t, err)

	got, err := testStore.GetDiagnosticsRequest(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "ftp://example.com/diag", got.Location)

	err = testStore.DeleteDiagnosticsRequest(ctx, "cs001")
	require.NoError(t, err)

	got, err = testStore.GetDiagnosticsRequest(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestLogRequest_SetGetDelete(t *testing.T) {
	defer truncateAll(t)
	ctx := context.Background()

	err := testStore.SetChargeStationAuth(ctx, "cs001", &store.ChargeStationAuth{
		SecurityProfile: store.UnsecuredTransportWithBasicAuth,
	})
	require.NoError(t, err)

	request := &store.LogRequest{
		ChargeStationId: "cs001",
		LogType:         "DiagnosticsLog",
		RequestId:       1,
		RemoteLocation:  "https://example.com/logs",
		SendAfter:       time.Now().UTC().Truncate(time.Second),
	}

	err = testStore.SetLogRequest(ctx, "cs001", request)
	require.NoError(t, err)

	got, err := testStore.GetLogRequest(ctx, "cs001")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "DiagnosticsLog", got.LogType)

	err = testStore.DeleteLogRequest(ctx, "cs001")
	require.NoError(t, err)

	got, err = testStore.GetLogRequest(ctx, "cs001")
	require.NoError(t, err)
	assert.Nil(t, got)
}
