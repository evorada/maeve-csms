// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/thoughtworks/maeve-csms/manager/store/postgres"
)

var (
	testStore  *postgres.Store
	testPool   *pgxpool.Pool
	connString string
)

func setup() func() {
	ctx := context.Background()

	// If TEST_POSTGRES_CONN is set, use it directly (CI with service container)
	if conn := os.Getenv("TEST_POSTGRES_CONN"); conn != "" {
		connString = conn
		return setupWithConnString(ctx)
	}

	// Otherwise, start a container via testcontainers (local dev)
	return setupWithTestcontainers(ctx)
}

func setupWithConnString(ctx context.Context) func() {
	// Run migrations
	err := postgres.RunMigrations(connString, "file://./migrations")
	if err != nil {
		log.Fatalf("failed to run migrations: %s", err)
	}

	// Create store
	testStore, err = postgres.NewStore(ctx, connString)
	if err != nil {
		log.Fatalf("failed to create store: %s", err)
	}

	// Create pool for cleanup
	testPool, err = pgxpool.New(ctx, connString)
	if err != nil {
		log.Fatalf("failed to create pool: %s", err)
	}

	return func() {
		testStore.Close()
		testPool.Close()
	}
}

func setupWithTestcontainers(ctx context.Context) func() {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "maeve_test",
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		log.Fatalf("failed to start postgres container: %s", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		log.Fatalf("failed to get container host: %s", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		log.Fatalf("failed to get container port: %s", err)
	}

	connString = fmt.Sprintf("postgres://test:test@%s:%s/maeve_test?sslmode=disable", host, port.Port())

	teardownConn := setupWithConnString(ctx)

	return func() {
		teardownConn()
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	}
}

func TestMain(m *testing.M) {
	teardown := setup()
	exitVal := m.Run()
	teardown()
	os.Exit(exitVal)
}

// truncateAll removes all data from all tables (preserving schema)
func truncateAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		DO $$ DECLARE
			r RECORD;
		BEGIN
			FOR r IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename != 'schema_migrations') LOOP
				EXECUTE 'TRUNCATE TABLE ' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END $$;
	`)
	if err != nil {
		t.Fatalf("failed to truncate tables: %s", err)
	}
}
