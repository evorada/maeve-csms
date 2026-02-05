package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
)

type testDB struct {
	container testcontainers.Container
	connStr   string
	store     *postgres.Store
}

func setupTestDB(t *testing.T) *testDB {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_USER":     "test",
			"POSTGRES_DB":       "maeve_test",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)

	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	connStr := fmt.Sprintf("postgres://test:test@%s:%s/maeve_test?sslmode=disable", host, port.Port())

	// Run migrations
	m, err := migrate.New(
		"file://./migrations",
		connStr,
	)
	require.NoError(t, err)

	err = m.Up()
	require.NoError(t, err)

	// Close migrate instance to release connection
	sourceErr, dbErr := m.Close()
	require.NoError(t, sourceErr)
	require.NoError(t, dbErr)

	// Create store
	store, err := postgres.NewStore(ctx, connStr)
	require.NoError(t, err)

	return &testDB{
		container: container,
		connStr:   connStr,
		store:     store,
	}
}

func (db *testDB) Teardown(t *testing.T) {
	if db.store != nil {
		db.store.Close()
	}
	if db.container != nil {
		require.NoError(t, db.container.Terminate(context.Background()))
	}
}

// Test that the test infrastructure works
func TestTestInfrastructure(t *testing.T) {
	db := setupTestDB(t)
	defer db.Teardown(t)

	ctx := context.Background()

	// Test that we can ping the database
	err := db.store.Health(ctx)
	require.NoError(t, err)
}
