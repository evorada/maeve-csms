// SPDX-License-Identifier: Apache-2.0

package config

type InMemoryStorageConfig struct{}

type FirestoreStorageConfig struct {
	ProjectId string `mapstructure:"project_id" toml:"project_id" validate:"required"`
}

type PostgresStorageConfig struct {
	Host           string `mapstructure:"host" toml:"host" validate:"required"`
	Port           int    `mapstructure:"port" toml:"port" validate:"required,min=1,max=65535"`
	Database       string `mapstructure:"database" toml:"database" validate:"required"`
	User           string `mapstructure:"user" toml:"user" validate:"required"`
	Password       string `mapstructure:"password" toml:"password" validate:"required"`
	SSLMode        string `mapstructure:"ssl_mode" toml:"ssl_mode" validate:"required,oneof=disable require verify-ca verify-full"`
	RunMigrations  bool   `mapstructure:"run_migrations" toml:"run_migrations"`
	MigrationsPath string `mapstructure:"migrations_path" toml:"migrations_path"`
}

type StorageConfig struct {
	Type             string                  `mapstructure:"type" toml:"type" validate:"required,oneof=firestore in_memory postgres"`
	FirestoreStorage *FirestoreStorageConfig `mapstructure:"firestore,omitempty" toml:"firestore,omitempty" validate:"required_if=Type firestore"`
	InMemoryStorage  *InMemoryStorageConfig  `mapstructure:"in_memory,omitempty" toml:"in_memory,omitempty"`
	PostgresStorage  *PostgresStorageConfig  `mapstructure:"postgres,omitempty" toml:"postgres,omitempty" validate:"required_if=Type postgres"`
}
