// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostgresReadOnlyStorageConfig(t *testing.T) {
	t.Run("with read-only config", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: "postgres",
			PostgresStorage: &PostgresStorageConfig{
				Host:     "primary-host",
				Port:     5432,
				Database: "maeve",
				User:     "user",
				Password: "pass",
				SSLMode:  "disable",
			},
			PostgresReadOnlyStorage: &PostgresReadOnlyStorageConfig{
				Host: "replica-host",
				Port: 5433,
			},
		}

		assert.NotNil(t, cfg.PostgresReadOnlyStorage)
		assert.Equal(t, "replica-host", cfg.PostgresReadOnlyStorage.Host)
		assert.Equal(t, 5433, cfg.PostgresReadOnlyStorage.Port)
	})

	t.Run("without read-only config", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: "postgres",
			PostgresStorage: &PostgresStorageConfig{
				Host:     "primary-host",
				Port:     5432,
				Database: "maeve",
				User:     "user",
				Password: "pass",
				SSLMode:  "disable",
			},
		}

		assert.Nil(t, cfg.PostgresReadOnlyStorage)
	})

	t.Run("with read-only config but empty host", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: "postgres",
			PostgresStorage: &PostgresStorageConfig{
				Host:     "primary-host",
				Port:     5432,
				Database: "maeve",
				User:     "user",
				Password: "pass",
				SSLMode:  "disable",
			},
			PostgresReadOnlyStorage: &PostgresReadOnlyStorageConfig{
				Host: "",
				Port: 0,
			},
		}

		assert.NotNil(t, cfg.PostgresReadOnlyStorage)
		assert.Equal(t, "", cfg.PostgresReadOnlyStorage.Host)
		// Empty host means replica should not be used
	})
}
