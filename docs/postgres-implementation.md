# PostgreSQL Storage Implementation

**Status:** 🚧 In Progress  
**Started:** 2026-02-04  
**Target Completion:** 2026-03-04  
**Owner:** @suda

## Overview

Implement a PostgreSQL storage adapter as an alternative to Firestore, providing a production-ready, open-source, self-hostable database option for MaEVe CSMS.

## Goals

- ✅ Provide a complete implementation of `store.Engine` interface using PostgreSQL
- ✅ Use modern Go database tools (sqlc for type-safe queries, pgx for driver)
- ✅ Implement proper database migrations
- ✅ Achieve feature parity with Firestore implementation
- ✅ Maintain high test coverage with integration tests
- ✅ Document setup and usage

## Technology Stack

- **Database Driver:** [pgx/v5](https://github.com/jackc/pgx) - High-performance PostgreSQL driver
- **SQL Codegen:** [sqlc](https://sqlc.dev/) - Generate type-safe Go code from SQL
- **Migrations:** [golang-migrate](https://github.com/golang-migrate/migrate) - Database migration tool
- **Testing:** [testcontainers-go](https://github.com/testcontainers/testcontainers-go) with PostgreSQL container
- **Connection Pooling:** pgxpool (built into pgx)

## Architecture

```
manager/
└── store/
    └── postgres/
        ├── migrations/           # SQL migration files
        │   ├── 000001_init.up.sql
        │   ├── 000001_init.down.sql
        │   └── ...
        ├── queries/             # SQL query files for sqlc
        │   ├── tokens.sql
        │   ├── charge_stations.sql
        │   ├── transactions.sql
        │   └── ...
        ├── sqlc.yaml           # sqlc configuration
        ├── db.go               # Generated sqlc code
        ├── models.go           # Generated sqlc models
        ├── querier.go          # Generated sqlc interface
        ├── store.go            # Store implementation
        ├── store_test.go       # Integration tests
        ├── tokens.go           # Token store implementation
        ├── tokens_test.go      # Token store tests
        ├── transactions.go     # Transaction store implementation
        ├── transactions_test.go
        ├── charge_stations.go  # Charge station store implementation
        ├── charge_stations_test.go
        └── ... (one file per store interface)
```

## Prerequisites

### Install Tools

```bash
# Install sqlc
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Install golang-migrate
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Verify installations
sqlc version
migrate -version
```

### Add Dependencies

```bash
cd manager/
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/postgres
go get github.com/golang-migrate/migrate/v4/source/file
```

---

## Implementation Tasks

### Phase 1: Project Setup & Infrastructure ✅

#### Task 1.1: Create Directory Structure
- [ ] Create `manager/store/postgres/` directory
- [ ] Create `manager/store/postgres/migrations/` directory
- [ ] Create `manager/store/postgres/queries/` directory
- [ ] Create `manager/store/postgres/testdata/` directory (for test fixtures)

**Commands:**
```bash
cd ~/Projects/Personal/Go/maeve-csms/manager/store
mkdir -p postgres/{migrations,queries,testdata}
```

#### Task 1.2: Configure sqlc
- [ ] Create `manager/store/postgres/sqlc.yaml` configuration file
- [ ] Set up sqlc to generate code in the postgres package
- [ ] Configure naming conventions and output settings

**File: `manager/store/postgres/sqlc.yaml`**
```yaml
version: "2"
sql:
  - schema: "migrations"
    queries: "queries"
    engine: "postgresql"
    gen:
      go:
        package: "postgres"
        out: "."
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_db_tags: true
        emit_prepared_queries: false
        emit_interface: true
        emit_exact_table_names: false
        emit_empty_slices: true
        emit_exported_queries: true
        output_models_file_name: "models.go"
        output_db_file_name: "db.go"
        output_querier_file_name: "querier.go"
```

#### Task 1.3: Add Make Targets
- [ ] Add `make postgres-generate` target to regenerate sqlc code
- [ ] Add `make postgres-migrate-up` target to run migrations
- [ ] Add `make postgres-migrate-down` target to rollback migrations
- [ ] Add `make postgres-test` target to run tests with test container

**File: `manager/Makefile` (create if doesn't exist)**
```makefile
.PHONY: postgres-generate
postgres-generate:
	cd store/postgres && sqlc generate

.PHONY: postgres-migrate-up
postgres-migrate-up:
	migrate -path store/postgres/migrations -database "$(DATABASE_URL)" up

.PHONY: postgres-migrate-down
postgres-migrate-down:
	migrate -path store/postgres/migrations -database "$(DATABASE_URL)" down

.PHONY: postgres-test
postgres-test:
	go test -v -race ./store/postgres/...
```

---

### Phase 2: Database Schema & Migrations ⏳

#### Task 2.1: Design Database Schema
- [ ] Document table structure for all entities
- [ ] Define primary keys, foreign keys, and indexes
- [ ] Plan JSON columns for flexible data (e.g., settings, meter values)
- [ ] Consider partitioning strategy for transactions table

**Schema Overview:**

```sql
-- Core tables
- tokens                    -- TokenStore
- charge_stations           -- ChargeStationAuthStore
- charge_station_settings   -- ChargeStationSettingsStore
- charge_station_runtime    -- ChargeStationRuntimeDetailsStore
- charge_station_certificates -- ChargeStationInstallCertificatesStore
- charge_station_triggers   -- ChargeStationTriggerMessageStore
- transactions             -- TransactionStore
- transaction_meter_values -- MeterValues for transactions
- certificates             -- CertificateStore
- ocpi_registrations       -- OcpiStore
- locations                -- LocationStore
```

#### Task 2.2: Create Initial Migration (Tokens Table)
- [ ] Create `000001_create_tokens_table.up.sql`
- [ ] Create `000001_create_tokens_table.down.sql`
- [ ] Include indexes for common queries (uid, contract_id)
- [ ] Add created_at and updated_at timestamps

**File: `migrations/000001_create_tokens_table.up.sql`**
```sql
CREATE TABLE tokens (
    id BIGSERIAL PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    type VARCHAR(50) NOT NULL,
    uid VARCHAR(36) NOT NULL,
    contract_id VARCHAR(255) NOT NULL,
    visual_number VARCHAR(64),
    issuer VARCHAR(255) NOT NULL,
    group_id VARCHAR(36),
    valid BOOLEAN NOT NULL DEFAULT true,
    language_code VARCHAR(2),
    cache_mode VARCHAR(20) NOT NULL,
    last_updated TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    CONSTRAINT tokens_uid_unique UNIQUE (uid)
);

CREATE INDEX idx_tokens_contract_id ON tokens(contract_id);
CREATE INDEX idx_tokens_valid ON tokens(valid);
CREATE INDEX idx_tokens_cache_mode ON tokens(cache_mode);
CREATE INDEX idx_tokens_last_updated ON tokens(last_updated);
```

**File: `migrations/000001_create_tokens_table.down.sql`**
```sql
DROP TABLE IF EXISTS tokens;
```

#### Task 2.3: Create Charge Stations Migration
- [x] Create `000002_create_charge_stations.up.sql`
- [x] Create `000002_create_charge_stations.down.sql`
- [x] Support both auth tables (auth, settings, runtime, certificates, triggers)

**File: `migrations/000002_create_charge_stations.up.sql`**
```sql
-- Charge station authentication
CREATE TABLE charge_stations (
    charge_station_id VARCHAR(48) PRIMARY KEY,
    security_profile INT NOT NULL,
    base64_sha256_password VARCHAR(255),
    invalid_username_allowed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station settings
CREATE TABLE charge_station_settings (
    charge_station_id VARCHAR(48) PRIMARY KEY REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    settings JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station runtime details
CREATE TABLE charge_station_runtime (
    charge_station_id VARCHAR(48) PRIMARY KEY REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    ocpp_version VARCHAR(10) NOT NULL,
    vendor VARCHAR(255),
    model VARCHAR(255),
    serial_number VARCHAR(255),
    firmware_version VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station certificates to install
CREATE TABLE charge_station_certificates (
    id BIGSERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    certificate_type VARCHAR(50) NOT NULL,
    certificate TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cs_certificates_station_id ON charge_station_certificates(charge_station_id);

-- Charge station trigger messages
CREATE TABLE charge_station_triggers (
    id BIGSERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    message_type VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cs_triggers_station_id ON charge_station_triggers(charge_station_id);
```

#### Task 2.4: Create Transactions Migration
- [x] Create `000003_create_transactions.up.sql`
- [x] Create `000003_create_transactions.down.sql`
- [x] Consider partitioning by date for large deployments

**File: `migrations/000003_create_transactions.up.sql`**
```sql
CREATE TABLE transactions (
    id VARCHAR(36) PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL,
    token_uid VARCHAR(36) NOT NULL,
    token_type VARCHAR(50) NOT NULL,
    meter_start INT NOT NULL,
    meter_stop INT,
    start_timestamp TIMESTAMP NOT NULL,
    stop_timestamp TIMESTAMP,
    stopped_reason VARCHAR(100),
    updated_seq_no INT NOT NULL DEFAULT 0,
    offline BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_station_id ON transactions(charge_station_id);
CREATE INDEX idx_transactions_token_uid ON transactions(token_uid);
CREATE INDEX idx_transactions_start_time ON transactions(start_timestamp);
CREATE INDEX idx_transactions_stop_time ON transactions(stop_timestamp);

-- Meter values for transactions
CREATE TABLE transaction_meter_values (
    id BIGSERIAL PRIMARY KEY,
    transaction_id VARCHAR(36) NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    timestamp TIMESTAMP NOT NULL,
    sampled_values JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_meter_values_transaction_id ON transaction_meter_values(transaction_id);
CREATE INDEX idx_meter_values_timestamp ON transaction_meter_values(timestamp);
```

#### Task 2.5: Create Certificates Migration
- [x] Create `000004_create_certificates.up.sql`
- [x] Create `000004_create_certificates.down.sql`

**File: `migrations/000004_create_certificates.up.sql`**
```sql
CREATE TABLE certificates (
    certificate_hash VARCHAR(255) PRIMARY KEY,
    certificate_type VARCHAR(50) NOT NULL,
    certificate_data TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

#### Task 2.6: Create OCPI & Locations Migration
- [x] Create `000005_create_ocpi_locations.up.sql`
- [x] Create `000005_create_ocpi_locations.down.sql`

**File: `migrations/000005_create_ocpi_locations.up.sql`**
```sql
-- OCPI registrations
CREATE TABLE ocpi_registrations (
    id BIGSERIAL PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    status VARCHAR(50) NOT NULL,
    token VARCHAR(255) NOT NULL,
    url VARCHAR(1024) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    CONSTRAINT ocpi_country_party_unique UNIQUE (country_code, party_id)
);

-- Locations
CREATE TABLE locations (
    id VARCHAR(48) PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    location_data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_locations_country_party ON locations(country_code, party_id);
```

---

### Phase 3: SQL Queries (sqlc) 📝

#### Task 3.1: Token Queries
- [ ] Create `queries/tokens.sql` with CRUD operations
- [ ] Include pagination support for ListTokens

**File: `queries/tokens.sql`**
```sql
-- name: GetToken :one
SELECT * FROM tokens
WHERE uid = $1 LIMIT 1;

-- name: ListTokens :many
SELECT * FROM tokens
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CreateToken :one
INSERT INTO tokens (
    country_code, party_id, type, uid, contract_id,
    visual_number, issuer, group_id, valid, language_code,
    cache_mode, last_updated
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: UpdateToken :one
UPDATE tokens
SET 
    country_code = $2,
    party_id = $3,
    type = $4,
    contract_id = $5,
    visual_number = $6,
    issuer = $7,
    group_id = $8,
    valid = $9,
    language_code = $10,
    cache_mode = $11,
    last_updated = $12,
    updated_at = NOW()
WHERE uid = $1
RETURNING *;

-- name: DeleteToken :exec
DELETE FROM tokens WHERE uid = $1;
```

#### Task 3.2: Charge Station Queries
- [ ] Create `queries/charge_stations.sql`
- [ ] Include queries for all charge station tables

**File: `queries/charge_stations.sql`**
```sql
-- Auth
-- name: GetChargeStationAuth :one
SELECT * FROM charge_stations WHERE charge_station_id = $1;

-- name: SetChargeStationAuth :one
INSERT INTO charge_stations (
    charge_station_id, security_profile, base64_sha256_password, invalid_username_allowed
) VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE
SET security_profile = EXCLUDED.security_profile,
    base64_sha256_password = EXCLUDED.base64_sha256_password,
    invalid_username_allowed = EXCLUDED.invalid_username_allowed,
    updated_at = NOW()
RETURNING *;

-- Settings
-- name: GetChargeStationSettings :one
SELECT * FROM charge_station_settings WHERE charge_station_id = $1;

-- name: SetChargeStationSettings :one
INSERT INTO charge_station_settings (charge_station_id, settings)
VALUES ($1, $2)
ON CONFLICT (charge_station_id) DO UPDATE
SET settings = EXCLUDED.settings, updated_at = NOW()
RETURNING *;

-- Runtime
-- name: GetChargeStationRuntime :one
SELECT * FROM charge_station_runtime WHERE charge_station_id = $1;

-- name: SetChargeStationRuntime :one
INSERT INTO charge_station_runtime (charge_station_id, ocpp_version, vendor, model, serial_number, firmware_version)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET ocpp_version = EXCLUDED.ocpp_version,
    vendor = EXCLUDED.vendor,
    model = EXCLUDED.model,
    serial_number = EXCLUDED.serial_number,
    firmware_version = EXCLUDED.firmware_version,
    updated_at = NOW()
RETURNING *;

-- Certificates
-- name: GetChargeStationCertificates :many
SELECT * FROM charge_station_certificates 
WHERE charge_station_id = $1
ORDER BY created_at DESC;

-- name: AddChargeStationCertificate :one
INSERT INTO charge_station_certificates (charge_station_id, certificate_type, certificate)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteChargeStationCertificates :exec
DELETE FROM charge_station_certificates WHERE charge_station_id = $1;

-- Triggers
-- name: GetChargeStationTriggers :many
SELECT * FROM charge_station_triggers
WHERE charge_station_id = $1
ORDER BY created_at ASC;

-- name: AddChargeStationTrigger :one
INSERT INTO charge_station_triggers (charge_station_id, message_type)
VALUES ($1, $2)
RETURNING *;

-- name: DeleteChargeStationTriggers :exec
DELETE FROM charge_station_triggers WHERE charge_station_id = $1;
```

#### Task 3.3: Transaction Queries
- [ ] Create `queries/transactions.sql`
- [ ] Include meter values queries

**File: `queries/transactions.sql`**
```sql
-- name: GetTransaction :one
SELECT * FROM transactions WHERE id = $1;

-- name: FindActiveTransaction :one
SELECT * FROM transactions 
WHERE charge_station_id = $1 AND stop_timestamp IS NULL
ORDER BY start_timestamp DESC
LIMIT 1;

-- name: CreateTransaction :one
INSERT INTO transactions (
    id, charge_station_id, token_uid, token_type,
    meter_start, start_timestamp, offline
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateTransaction :one
UPDATE transactions
SET meter_stop = $2,
    stop_timestamp = $3,
    stopped_reason = $4,
    updated_seq_no = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AddMeterValues :exec
INSERT INTO transaction_meter_values (transaction_id, timestamp, sampled_values)
VALUES ($1, $2, $3);

-- name: GetMeterValues :many
SELECT * FROM transaction_meter_values
WHERE transaction_id = $1
ORDER BY timestamp ASC;
```

#### Task 3.4: Certificate, OCPI, and Location Queries
- [ ] Create `queries/certificates.sql`
- [ ] Create `queries/ocpi.sql`
- [ ] Create `queries/locations.sql`

#### Task 3.5: Generate sqlc Code
- [ ] Run `sqlc generate` to create Go code
- [ ] Verify generated files (db.go, models.go, querier.go, etc.)

**Command:**
```bash
cd ~/Projects/Personal/Go/maeve-csms/manager/store/postgres
sqlc generate
```

---

### Phase 4: Store Implementation 🔧

#### Task 4.1: Create Base Store Structure
- [ ] Create `store.go` with Store struct
- [ ] Implement connection pooling with pgxpool
- [ ] Add health check method
- [ ] Implement context handling

**File: `store.go`**
```go
package postgres

import (
    "context"
    "fmt"
    
    "github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
    pool *pgxpool.Pool
    q    *Queries
}

func NewStore(ctx context.Context, connString string) (*Store, error) {
    config, err := pgxpool.ParseConfig(connString)
    if err != nil {
        return nil, fmt.Errorf("failed to parse connection string: %w", err)
    }
    
    // Configure pool
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

func (s *Store) Close() {
    s.pool.Close()
}

func (s *Store) Health(ctx context.Context) error {
    return s.pool.Ping(ctx)
}
```

#### Task 4.2: Implement TokenStore Interface
- [ ] Create `tokens.go` with TokenStore methods
- [ ] Map between store.Token and generated Token model
- [ ] Handle pointer fields correctly
- [ ] Add error wrapping with context

**File: `tokens.go`**
```go
package postgres

import (
    "context"
    "database/sql"
    "fmt"
    
    "github.com/thoughtworks/maeve-csms/manager/store"
)

func (s *Store) SetToken(ctx context.Context, token *store.Token) error {
    params := CreateTokenParams{
        CountryCode:  token.CountryCode,
        PartyId:      token.PartyId,
        Type:         token.Type,
        Uid:          token.Uid,
        ContractId:   token.ContractId,
        VisualNumber: toNullString(token.VisualNumber),
        Issuer:       token.Issuer,
        GroupId:      toNullString(token.GroupId),
        Valid:        token.Valid,
        LanguageCode: toNullString(token.LanguageCode),
        CacheMode:    token.CacheMode,
        LastUpdated:  token.LastUpdated,
    }
    
    _, err := s.q.CreateToken(ctx, params)
    if err != nil {
        return fmt.Errorf("failed to create token: %w", err)
    }
    
    return nil
}

func (s *Store) LookupToken(ctx context.Context, tokenUid string) (*store.Token, error) {
    token, err := s.q.GetToken(ctx, tokenUid)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, nil
        }
        return nil, fmt.Errorf("failed to lookup token: %w", err)
    }
    
    return toStoreToken(&token), nil
}

func (s *Store) ListTokens(ctx context.Context, offset int, limit int) ([]*store.Token, error) {
    tokens, err := s.q.ListTokens(ctx, ListTokensParams{
        Limit:  int32(limit),
        Offset: int32(offset),
    })
    if err != nil {
        return nil, fmt.Errorf("failed to list tokens: %w", err)
    }
    
    result := make([]*store.Token, len(tokens))
    for i, t := range tokens {
        result[i] = toStoreToken(&t)
    }
    
    return result, nil
}

// Helper functions
func toStoreToken(t *Token) *store.Token {
    return &store.Token{
        CountryCode:  t.CountryCode,
        PartyId:      t.PartyId,
        Type:         t.Type,
        Uid:          t.Uid,
        ContractId:   t.ContractId,
        VisualNumber: fromNullString(t.VisualNumber),
        Issuer:       t.Issuer,
        GroupId:      fromNullString(t.GroupId),
        Valid:        t.Valid,
        LanguageCode: fromNullString(t.LanguageCode),
        CacheMode:    t.CacheMode,
        LastUpdated:  t.LastUpdated,
    }
}

func toNullString(s *string) sql.NullString {
    if s == nil {
        return sql.NullString{Valid: false}
    }
    return sql.NullString{String: *s, Valid: true}
}

func fromNullString(ns sql.NullString) *string {
    if !ns.Valid {
        return nil
    }
    return &ns.String
}
```

#### Task 4.3: Implement ChargeStationAuthStore Interface
- [ ] Create `charge_stations.go`
- [ ] Implement all charge station store methods

#### Task 4.4: Implement TransactionStore Interface
- [ ] Create `transactions.go`
- [ ] Handle MeterValues as JSONB
- [ ] Implement proper transaction ID conversion

#### Task 4.5: Implement Remaining Store Interfaces
- [ ] Implement CertificateStore
- [ ] Implement OcpiStore
- [ ] Implement LocationStore

#### Task 4.6: Implement store.Engine Interface
- [ ] Ensure Store implements all required interfaces
- [ ] Add interface assertion in store.go

**Add to `store.go`:**
```go
// Verify Store implements store.Engine
var _ store.Engine = (*Store)(nil)
```

---

### Phase 5: Testing 🧪

#### Task 5.1: Create Test Infrastructure
- [ ] Create `store_test.go` with test setup helpers
- [ ] Implement testcontainer PostgreSQL setup
- [ ] Add migration runner for tests
- [ ] Create test data fixtures

**File: `store_test.go`**
```go
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
```

#### Task 5.2: Write Token Store Tests
- [ ] Create `tokens_test.go`
- [ ] Test SetToken
- [ ] Test LookupToken (found and not found)
- [ ] Test ListTokens with pagination
- [ ] Test token with nil pointer fields

**File: `tokens_test.go`**
```go
package postgres_test

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    
    "github.com/thoughtworks/maeve-csms/manager/store"
)

func TestTokenStore_SetAndLookup(t *testing.T) {
    db := setupTestDB(t)
    defer db.Teardown(t)
    
    ctx := context.Background()
    
    token := &store.Token{
        CountryCode: "GB",
        PartyId:     "TWK",
        Type:        "RFID",
        Uid:         "DEADBEEF",
        ContractId:  "GBTWK012345678V",
        Issuer:      "Thoughtworks",
        Valid:       true,
        CacheMode:   "ALWAYS",
        LastUpdated: "2026-02-04T23:00:00Z",
    }
    
    // Test SetToken
    err := db.store.SetToken(ctx, token)
    require.NoError(t, err)
    
    // Test LookupToken - found
    foundToken, err := db.store.LookupToken(ctx, "DEADBEEF")
    require.NoError(t, err)
    require.NotNil(t, foundToken)
    assert.Equal(t, token.Uid, foundToken.Uid)
    assert.Equal(t, token.ContractId, foundToken.ContractId)
    
    // Test LookupToken - not found
    notFound, err := db.store.LookupToken(ctx, "NOTEXIST")
    require.NoError(t, err)
    assert.Nil(t, notFound)
}

func TestTokenStore_ListTokens(t *testing.T) {
    db := setupTestDB(t)
    defer db.Teardown(t)
    
    ctx := context.Background()
    
    // Create multiple tokens
    for i := 0; i < 5; i++ {
        token := &store.Token{
            CountryCode: "GB",
            PartyId:     "TWK",
            Type:        "RFID",
            Uid:         fmt.Sprintf("TOKEN%03d", i),
            ContractId:  fmt.Sprintf("GBTWK%010dV", i),
            Issuer:      "Thoughtworks",
            Valid:       true,
            CacheMode:   "ALWAYS",
            LastUpdated: "2026-02-04T23:00:00Z",
        }
        require.NoError(t, db.store.SetToken(ctx, token))
    }
    
    // Test pagination
    tokens, err := db.store.ListTokens(ctx, 0, 3)
    require.NoError(t, err)
    assert.Len(t, tokens, 3)
    
    tokens, err = db.store.ListTokens(ctx, 3, 3)
    require.NoError(t, err)
    assert.Len(t, tokens, 2)
}
```

#### Task 5.3: Write Charge Station Store Tests
- [ ] Create `charge_stations_test.go`
- [ ] Test all ChargeStationAuthStore methods
- [ ] Test all ChargeStationSettingsStore methods
- [ ] Test all ChargeStationRuntimeDetailsStore methods
- [ ] Test certificate and trigger methods

#### Task 5.4: Write Transaction Store Tests
- [ ] Create `transactions_test.go`
- [ ] Test CreateTransaction
- [ ] Test UpdateTransaction
- [ ] Test FindActiveTransaction
- [ ] Test MeterValues handling

#### Task 5.5: Write Integration Tests
- [ ] Test cross-store operations (e.g., transaction with token lookup)
- [ ] Test concurrent operations
- [ ] Test connection pool behavior
- [ ] Test error scenarios (connection loss, constraint violations)

#### Task 5.6: Run Test Suite
- [ ] Run all tests: `make postgres-test`
- [ ] Check test coverage: `go test -cover ./store/postgres/...`
- [ ] Target: >80% coverage

---

### Phase 6: Documentation & Integration 📚

#### Task 6.1: Add PostgreSQL Configuration
- [ ] Update manager config to support PostgreSQL
- [ ] Add connection string configuration
- [ ] Document environment variables

**Update: `manager/config/config.go`**
```go
type StorageConfig struct {
    Type       string `toml:"type"` // "firestore", "postgres", "inmemory"
    Postgres   PostgresConfig `toml:"postgres,omitempty"`
    // ... existing fields
}

type PostgresConfig struct {
    Host     string `toml:"host"`
    Port     int    `toml:"port"`
    Database string `toml:"database"`
    User     string `toml:"user"`
    Password string `toml:"password"`
    SSLMode  string `toml:"ssl_mode"`
}
```

#### Task 6.2: Update Store Factory
- [ ] Add PostgreSQL case to store initialization
- [ ] Run migrations on startup if configured

**Update: `manager/cmd/serve.go` or similar**
```go
func createStore(ctx context.Context, cfg *config.Config) (store.Engine, error) {
    switch cfg.Storage.Type {
    case "postgres":
        connStr := fmt.Sprintf(
            "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
            cfg.Storage.Postgres.Host,
            cfg.Storage.Postgres.Port,
            cfg.Storage.Postgres.User,
            cfg.Storage.Postgres.Password,
            cfg.Storage.Postgres.Database,
            cfg.Storage.Postgres.SSLMode,
        )
        return postgres.NewStore(ctx, connStr)
    // ... existing cases
    }
}
```

#### Task 6.3: Write README
- [ ] Create `manager/store/postgres/README.md`
- [ ] Document setup instructions
- [ ] Add configuration examples
- [ ] Document migration workflow

**File: `manager/store/postgres/README.md`**
```markdown
# PostgreSQL Storage Implementation

This package provides a PostgreSQL implementation of the MaEVe CSMS storage interfaces.

## Setup

### Prerequisites
- PostgreSQL 13+
- golang-migrate CLI tool
- sqlc CLI tool

### Database Setup
1. Create database:
   ```sql
   CREATE DATABASE maeve_csms;
   CREATE USER maeve WITH PASSWORD 'your_password';
   GRANT ALL PRIVILEGES ON DATABASE maeve_csms TO maeve;
   ```

2. Run migrations:
   ```bash
   export DATABASE_URL="postgres://maeve:your_password@localhost:5432/maeve_csms?sslmode=disable"
   make postgres-migrate-up
   ```

### Configuration
Add to `config/manager/config.toml`:
```toml
[storage]
type = "postgres"

[storage.postgres]
host = "localhost"
port = 5432
database = "maeve_csms"
user = "maeve"
password = "your_password"
ssl_mode = "disable"  # Use "require" in production
```

## Development

### Regenerate Code
After modifying SQL queries or schema:
```bash
make postgres-generate
```

### Create New Migration
```bash
migrate create -ext sql -dir manager/store/postgres/migrations -seq description_here
```

### Run Tests
```bash
make postgres-test
```
```

#### Task 6.4: Update Docker Compose
- [ ] Add PostgreSQL service to docker-compose.yml
- [ ] Update manager service to use PostgreSQL
- [ ] Add migration init container

#### Task 6.5: Update Main Documentation
- [ ] Update README.md to mention PostgreSQL support
- [ ] Update docs/design.md if needed
- [ ] Add PostgreSQL to DEVELOPMENT_PLAN.md as completed

---

## Progress Tracking

### Current Phase: Phase 3 - SQL Queries (sqlc) 📝

**Last Updated:** 2026-02-05 01:39 GMT+1

### Completed Tasks: 24 / ~60 total

#### Phase 1: ✅ COMPLETE
- ✅ Task 1.1: Create Directory Structure  
- ✅ Task 1.2: Configure sqlc  
- ✅ Task 1.3: Add Make Targets  

#### Phase 2: ✅ COMPLETE
- ✅ Task 2.2: Create Initial Migration (Tokens Table)
- ✅ Task 2.3: Create Charge Stations Migration
- ✅ Task 2.4: Create Transactions Migration
- ✅ Task 2.5: Create Certificates Migration
- ✅ Task 2.6: Create OCPI & Locations Migration

#### Phase 3: ⏳ IN PROGRESS
- ✅ Task 3.1: Token Queries  
- ✅ Task 3.2: Charge Station Queries
- ✅ Task 3.3: Transaction Queries
- ⏳ Task 3.4: Certificate, OCPI, and Location Queries (NEXT)
- ⬜ Task 3.5: Generate sqlc Code  

#### Phase 4: ⏳ IN PROGRESS
- ✅ Task 4.1: Create Base Store Structure
- ✅ Task 4.2: Implement TokenStore Interface
- ⏳ Task 4.3-4.6: Remaining store implementations (stubbed, ready for development)

### Blockers
- None currently

### Notes
- Started 2026-02-04
- Using sqlc v1.25.0
- Using golang-migrate v4.17.0

---

## Testing Checklist

Before marking this feature as complete:

- [ ] All store interfaces implemented
- [ ] Test coverage >80%
- [ ] Integration tests pass
- [ ] Load testing performed (benchmark against Firestore)
- [ ] Documentation complete
- [ ] Docker compose setup working
- [ ] Migration rollback tested
- [ ] Connection pool tuning documented
- [ ] Error handling comprehensive
- [ ] Logging added for debugging

---

## Success Criteria

✅ All `store.Engine` interfaces implemented  
✅ Feature parity with Firestore implementation  
✅ Test coverage >80%  
✅ Integration tests using testcontainers  
✅ Complete documentation (setup, config, migrations)  
✅ Docker compose working with PostgreSQL  
✅ Performance comparable to Firestore (within 20%)  

---

## Future Enhancements

After initial implementation:

- [ ] Add read replicas support
- [ ] Implement database connection retry logic
- [ ] Add query performance monitoring
- [ ] Implement prepared statement caching
- [ ] Add transaction isolation level configuration
- [ ] Implement soft deletes for audit trail
- [ ] Add database health metrics
- [ ] Create database backup/restore scripts
- [ ] Implement connection pool metrics

---

## References

- [sqlc Documentation](https://docs.sqlc.dev/)
- [pgx Documentation](https://pkg.go.dev/github.com/jackc/pgx/v5)
- [golang-migrate Documentation](https://github.com/golang-migrate/migrate)
- [PostgreSQL Best Practices](https://wiki.postgresql.org/wiki/Don%27t_Do_This)
- [testcontainers-go PostgreSQL](https://golang.testcontainers.org/modules/postgres/)
