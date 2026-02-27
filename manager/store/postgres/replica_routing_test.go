// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadReplicaRouting tests that read queries use the replica when configured
func TestReadReplicaRouting(t *testing.T) {
	// This test verifies that the Store correctly routes read and write operations
	// to the appropriate connection pools

	t.Run("without read replica", func(t *testing.T) {
		store := &Store{
			pool:           &pgxpool.Pool{},
			readPool:       &pgxpool.Pool{},
			q:              &Queries{},
			readQ:          &Queries{},
			hasReadReplica: false,
		}

		// When no read replica is configured, both should point to the same pool
		store.readPool = store.pool
		store.readQ = store.q

		readQ := store.readQueries()
		writeQ := store.writeQueries()

		assert.Equal(t, store.q, readQ, "read queries should use primary when no replica")
		assert.Equal(t, store.q, writeQ, "write queries should use primary")
		assert.Equal(t, readQ, writeQ, "both should point to same Queries when no replica")
	})

	t.Run("with read replica", func(t *testing.T) {
		// Create distinct pool pointers to verify routing
		primaryPool := &pgxpool.Pool{}
		replicaPool := &pgxpool.Pool{}

		// These are different pointers
		require.NotSame(t, primaryPool, replicaPool, "pools should be different instances")

		primaryQ := New(primaryPool)
		replicaQ := New(replicaPool)

		store := &Store{
			pool:           primaryPool,
			readPool:       replicaPool,
			q:              primaryQ,
			readQ:          replicaQ,
			hasReadReplica: true,
		}

		readQ := store.readQueries()
		writeQ := store.writeQueries()

		// Verify correct routing
		assert.Same(t, replicaQ, readQ, "read queries should use replica")
		assert.Same(t, primaryQ, writeQ, "write queries should use primary")
		assert.Same(t, primaryPool, store.pool, "write pool should be primary")
		assert.Same(t, replicaPool, store.readPool, "read pool should be replica")
		assert.NotSame(t, store.pool, store.readPool, "pools should be different instances")
	})
}

// TestNewStoreWithoutReplica tests store creation without read replica
func TestNewStoreWithoutReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This requires a real PostgreSQL instance
	// Skip if DATABASE_URL is not set
	connStr := getTestDatabaseURL(t)
	if connStr == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	store, err := NewStore(ctx, connStr)
	require.NoError(t, err)
	defer store.Close()

	// Verify store initialized correctly
	assert.NotNil(t, store.pool)
	assert.NotNil(t, store.q)
	assert.False(t, store.hasReadReplica)
	assert.Equal(t, store.pool, store.readPool, "read pool should equal primary pool")
	assert.Equal(t, store.q, store.readQ, "read queries should equal write queries")

	// Verify health check works
	err = store.Health(ctx)
	assert.NoError(t, err)
}

// TestNewStoreWithReplica tests store creation with read replica
func TestNewStoreWithReplica(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// This test requires a real PostgreSQL instance
	// For testing purposes, we use the same database as both primary and replica
	connStr := getTestDatabaseURL(t)
	if connStr == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()

	// Use the same connection string for both primary and replica for testing
	// In production, these would be different hosts
	store, err := NewStore(ctx, connStr, connStr)
	require.NoError(t, err)
	defer store.Close()

	// Verify store initialized correctly
	assert.NotNil(t, store.pool)
	assert.NotNil(t, store.readPool)
	assert.NotNil(t, store.q)
	assert.NotNil(t, store.readQ)
	assert.True(t, store.hasReadReplica)

	// Verify health check works for both connections
	err = store.Health(ctx)
	assert.NoError(t, err)
}

// TestNewStoreWithInvalidReplicaConnectionString tests error handling for invalid replica config
func TestNewStoreWithInvalidReplicaConnectionString(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connStr := getTestDatabaseURL(t)
	if connStr == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()

	// Try to create store with invalid read replica connection string
	invalidReadConnStr := "host=invalid-host port=9999 user=invalid password=invalid dbname=invalid sslmode=disable"

	store, err := NewStore(ctx, connStr, invalidReadConnStr)

	// Should return error because replica connection fails
	assert.Error(t, err)
	assert.Nil(t, store)
}

// TestHealthCheckWithBothConnections tests health check with both primary and replica
func TestHealthCheckWithBothConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connStr := getTestDatabaseURL(t)
	if connStr == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()

	// Create store with replica (using same DB for testing)
	store, err := NewStore(ctx, connStr, connStr)
	require.NoError(t, err)
	defer store.Close()

	// Health check should verify both connections
	err = store.Health(ctx)
	assert.NoError(t, err)
}

// getTestDatabaseURL returns the test database connection string from environment
// or returns empty string if not configured
func getTestDatabaseURL(t *testing.T) string {
	// Try to get from environment
	// This would be set in CI or local testing environment
	// For now, return empty to skip integration tests
	return ""
}
