// SPDX-License-Identifier: Apache-2.0

package postgres

import (
	"context"
	"fmt"
	"log/slog"

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
	slog.Info("initializing PostgreSQL store")

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		slog.Error("failed to parse connection string", "error", err)
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	config.MaxConns = 25
	config.MinConns = 5

	slog.Info("creating connection pool",
		"max_conns", config.MaxConns,
		"min_conns", config.MinConns,
		"host", config.ConnConfig.Host,
		"port", config.ConnConfig.Port,
		"database", config.ConnConfig.Database)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		slog.Error("failed to create connection pool", "error", err)
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		slog.Error("failed to ping database", "error", err)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("PostgreSQL store initialized successfully")

	return &Store{
		pool: pool,
		q:    New(pool),
	}, nil
}

// Close closes the database connection pool
func (s *Store) Close() {
	if s.pool != nil {
		slog.Info("closing PostgreSQL connection pool")
		s.pool.Close()
	}
}

// Health checks the database connection health
func (s *Store) Health(ctx context.Context) error {
	err := s.pool.Ping(ctx)
	if err != nil {
		slog.Error("database health check failed", "error", err)
		return err
	}

	// Log connection pool stats
	stats := s.pool.Stat()
	slog.Debug("database health check",
		"acquired_conns", stats.AcquiredConns(),
		"idle_conns", stats.IdleConns(),
		"total_conns", stats.TotalConns(),
		"max_conns", stats.MaxConns())

	return nil
}
