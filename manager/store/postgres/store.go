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
	pool         *pgxpool.Pool
	readPool     *pgxpool.Pool
	q            *Queries
	readQ        *Queries
	hasReadReplica bool
}

// NewStore creates a new PostgreSQL store with the given connection string
// and optional read-only replica connection string
func NewStore(ctx context.Context, connString string, readOnlyConnString ...string) (*Store, error) {
	slog.Info("initializing PostgreSQL store")

	// Setup primary connection pool
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		slog.Error("failed to parse connection string", "error", err)
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Configure connection pool
	config.MaxConns = 25
	config.MinConns = 5

	slog.Info("creating primary connection pool",
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

	store := &Store{
		pool:         pool,
		readPool:     pool,
		q:            New(pool),
		readQ:        New(pool),
		hasReadReplica: false,
	}

	// Setup read replica if provided
	if len(readOnlyConnString) > 0 && readOnlyConnString[0] != "" {
		readConfig, err := pgxpool.ParseConfig(readOnlyConnString[0])
		if err != nil {
			pool.Close()
			slog.Error("failed to parse read-only connection string", "error", err)
			return nil, fmt.Errorf("failed to parse read-only connection string: %w", err)
		}

		// Configure read pool
		readConfig.MaxConns = 25
		readConfig.MinConns = 5

		slog.Info("creating read-only connection pool",
			"max_conns", readConfig.MaxConns,
			"min_conns", readConfig.MinConns,
			"host", readConfig.ConnConfig.Host,
			"port", readConfig.ConnConfig.Port,
			"database", readConfig.ConnConfig.Database)

		readPool, err := pgxpool.NewWithConfig(ctx, readConfig)
		if err != nil {
			pool.Close()
			slog.Error("failed to create read-only connection pool", "error", err)
			return nil, fmt.Errorf("failed to create read-only connection pool: %w", err)
		}

		// Test read connection
		if err := readPool.Ping(ctx); err != nil {
			pool.Close()
			readPool.Close()
			slog.Error("failed to ping read-only database", "error", err)
			return nil, fmt.Errorf("failed to ping read-only database: %w", err)
		}

		store.readPool = readPool
		store.readQ = New(readPool)
		store.hasReadReplica = true

		slog.Info("PostgreSQL store initialized successfully with read replica")
	} else {
		slog.Info("PostgreSQL store initialized successfully (read replica not configured)")
	}

	return store, nil
}

// Close closes the database connection pool(s)
func (s *Store) Close() {
	if s.pool != nil {
		slog.Info("closing PostgreSQL primary connection pool")
		s.pool.Close()
	}
	
	if s.hasReadReplica && s.readPool != nil && s.readPool != s.pool {
		slog.Info("closing PostgreSQL read-only connection pool")
		s.readPool.Close()
	}
}

// Health checks the database connection health for both primary and replica
func (s *Store) Health(ctx context.Context) error {
	// Check primary connection
	err := s.pool.Ping(ctx)
	if err != nil {
		slog.Error("primary database health check failed", "error", err)
		return fmt.Errorf("primary database health check failed: %w", err)
	}

	// Log primary connection pool stats
	stats := s.pool.Stat()
	slog.Debug("primary database health check",
		"acquired_conns", stats.AcquiredConns(),
		"idle_conns", stats.IdleConns(),
		"total_conns", stats.TotalConns(),
		"max_conns", stats.MaxConns())

	// Check read replica if configured
	if s.hasReadReplica && s.readPool != s.pool {
		err := s.readPool.Ping(ctx)
		if err != nil {
			slog.Error("read-only database health check failed", "error", err)
			return fmt.Errorf("read-only database health check failed: %w", err)
		}

		// Log read pool stats
		readStats := s.readPool.Stat()
		slog.Debug("read-only database health check",
			"acquired_conns", readStats.AcquiredConns(),
			"idle_conns", readStats.IdleConns(),
			"total_conns", readStats.TotalConns(),
			"max_conns", readStats.MaxConns())
	}

	return nil
}

// readQueries returns the Queries instance for read operations
// Uses read replica if configured, otherwise falls back to primary
func (s *Store) readQueries() *Queries {
	return s.readQ
}

// writeQueries returns the Queries instance for write operations
// Always uses the primary connection
func (s *Store) writeQueries() *Queries {
	return s.q
}

// writePool returns the connection pool for write operations
// Always returns the primary pool
func (s *Store) writePool() *pgxpool.Pool {
	return s.pool
}
