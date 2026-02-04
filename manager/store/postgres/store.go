// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thoughtworks/maeve-csms/manager/store"
)

// Verify Store implements store.Engine at compile time
var _ store.Engine = (*Store)(nil)

// Store provides PostgreSQL implementation of the store.Engine interface
type Store struct {
	pool *pgxpool.Pool
	q    *Queries
}

// NewStore creates a new PostgreSQL store with the given connection string
func NewStore(ctx context.Context, connString string) (*Store, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	config.MaxConns = 25
	config.MinConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Store{
		pool: pool,
		q:    New(pool),
	}, nil
}

// Close closes the database connection pool
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Health checks the database connection health
func (s *Store) Health(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
