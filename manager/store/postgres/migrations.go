// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations runs database migrations from the specified path
func RunMigrations(connString, migrationsPath string) error {
	// Default migrations path if not specified
	if migrationsPath == "" {
		migrationsPath = "file://./store/postgres/migrations"
	}

	m, err := migrate.New(migrationsPath, connString)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	defer m.Close()

	// Run all pending migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
