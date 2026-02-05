# PostgreSQL Storage Implementation

This package provides a PostgreSQL implementation of the MaEVE CSMS storage interfaces, offering a production-ready, open-source, self-hostable database option as an alternative to Firestore.

## Features

- ✅ Complete implementation of `store.Engine` interface
- ✅ Type-safe SQL queries via [sqlc](https://sqlc.dev/)
- ✅ High-performance [pgx/v5](https://github.com/jackc/pgx) driver
- ✅ Database migrations with [golang-migrate](https://github.com/golang-migrate/migrate)
- ✅ Connection pooling for optimal performance
- ✅ JSONB support for flexible schema (settings, meter values)

## Prerequisites

- **PostgreSQL 13+** (tested with PostgreSQL 15)
- **Go 1.21+**
- **golang-migrate CLI** (for running migrations)
- **sqlc CLI** (only needed for development/regenerating queries)

### Install Development Tools

```bash
# Install sqlc (for query generation)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Install golang-migrate (for migrations)
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Verify installations
sqlc version
migrate -version
```

## Database Setup

### 1. Create Database and User

```sql
-- Connect to PostgreSQL as superuser
psql -U postgres

-- Create database
CREATE DATABASE maeve_csms;

-- Create user
CREATE USER maeve WITH PASSWORD 'your_secure_password';

-- Grant privileges
GRANT ALL PRIVILEGES ON DATABASE maeve_csms TO maeve;

-- Connect to the new database
\c maeve_csms

-- Grant schema privileges (PostgreSQL 15+)
GRANT ALL ON SCHEMA public TO maeve;
```

### 2. Run Migrations

```bash
# Set connection string
export DATABASE_URL="postgres://maeve:your_secure_password@localhost:5432/maeve_csms?sslmode=disable"

# Run migrations
cd ~/Projects/Personal/Go/maeve-csms/manager
make postgres-migrate-up

# To rollback (if needed)
make postgres-migrate-down
```

**Manual migration (without Make):**

```bash
migrate -path store/postgres/migrations -database "$DATABASE_URL" up
```

## Configuration

### Manager Configuration

Add to your `config/manager/config.toml`:

```toml
[storage]
type = "postgres"

[storage.postgres]
host = "localhost"
port = 5432
database = "maeve_csms"
user = "maeve"
password = "your_secure_password"
ssl_mode = "disable"  # Use "require" or "verify-full" in production
```

### Environment Variables (Alternative)

You can also use environment variables:

```bash
export STORAGE_TYPE="postgres"
export STORAGE_POSTGRES_HOST="localhost"
export STORAGE_POSTGRES_PORT="5432"
export STORAGE_POSTGRES_DATABASE="maeve_csms"
export STORAGE_POSTGRES_USER="maeve"
export STORAGE_POSTGRES_PASSWORD="your_secure_password"
export STORAGE_POSTGRES_SSL_MODE="disable"
```

### SSL Modes

| Mode | Description |
|------|-------------|
| `disable` | No SSL (development only) |
| `require` | SSL required, no verification |
| `verify-ca` | SSL required, verify CA |
| `verify-full` | SSL required, verify CA and hostname |

**⚠️ Production:** Always use `require`, `verify-ca`, or `verify-full` in production.

## Connection Pooling

The PostgreSQL store uses pgxpool for connection pooling with the following defaults:

- **Max Connections:** 25
- **Min Connections:** 5
- **Connection Timeout:** 30s
- **Idle Timeout:** 10m

These can be tuned via the connection string:

```go
connStr := "postgres://user:pass@host:5432/db?sslmode=disable&pool_max_conns=50&pool_min_conns=10"
```

## Development

### Directory Structure

```
store/postgres/
├── migrations/           # SQL migration files
│   ├── 000001_create_tokens_table.up.sql
│   ├── 000001_create_tokens_table.down.sql
│   ├── 000002_create_charge_stations.up.sql
│   ├── 000002_create_charge_stations.down.sql
│   └── ...
├── queries/             # SQL query files for sqlc
│   ├── tokens.sql
│   ├── charge_stations.sql
│   ├── transactions.sql
│   ├── certificates.sql
│   ├── ocpi.sql
│   └── locations.sql
├── sqlc.yaml           # sqlc configuration
├── db.go               # Generated sqlc code
├── models.go           # Generated sqlc models
├── querier.go          # Generated sqlc interface
├── store.go            # Store implementation
├── tokens.go           # TokenStore implementation
├── charge_stations.go  # Charge station stores
├── transactions.go     # TransactionStore implementation
├── certificates.go     # CertificateStore implementation
├── ocpi.go            # OcpiStore implementation
├── locations.go       # LocationStore implementation
└── *_test.go          # Tests
```

### Regenerate sqlc Code

After modifying SQL queries or schema:

```bash
cd ~/Projects/Personal/Go/maeve-csms/manager
make postgres-generate

# Or directly:
cd store/postgres
sqlc generate
```

### Create New Migration

```bash
cd ~/Projects/Personal/Go/maeve-csms/manager/store/postgres

# Create a new migration
migrate create -ext sql -dir migrations -seq add_new_feature

# This creates:
# migrations/NNNNNN_add_new_feature.up.sql
# migrations/NNNNNN_add_new_feature.down.sql
```

### Run Tests

```bash
cd ~/Projects/Personal/Go/maeve-csms/manager
make postgres-test

# With coverage:
go test -v -cover ./store/postgres/...

# With race detection:
go test -v -race ./store/postgres/...
```

Tests use [testcontainers-go](https://github.com/testcontainers/testcontainers-go) to spin up a PostgreSQL container automatically.

## Performance Considerations

### Indexes

The schema includes indexes for common query patterns:

- **Tokens:** `uid`, `contract_id`, `valid`, `cache_mode`
- **Charge Stations:** `charge_station_id`
- **Transactions:** `charge_station_id`, `token_uid`, `start_timestamp`, `stop_timestamp`
- **Meter Values:** `transaction_id`, `timestamp`

### JSONB Columns

Settings and meter values are stored as JSONB for flexibility:

- Efficient storage and querying
- GIN indexes available if needed
- Supports partial updates

### Connection Pooling

Tune pool settings based on your workload:

```toml
# For high-traffic deployments
[storage.postgres]
# ... other settings ...
connection_string = "postgres://...?pool_max_conns=100&pool_min_conns=20"
```

### Partitioning (Future)

For large deployments, consider partitioning the `transactions` table by date:

```sql
-- Example: partition by month
CREATE TABLE transactions_2026_01 PARTITION OF transactions
FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
```

## Troubleshooting

### Connection Issues

**Error:** `FATAL: password authentication failed for user "maeve"`

**Solution:** Check credentials and `pg_hba.conf` settings.

```bash
# Allow local connections (pg_hba.conf)
host    maeve_csms    maeve    127.0.0.1/32    md5
```

### Migration Issues

**Error:** `Dirty database version`

**Solution:** Force version and retry:

```bash
migrate -path store/postgres/migrations -database "$DATABASE_URL" force 1
migrate -path store/postgres/migrations -database "$DATABASE_URL" up
```

### Test Container Issues

**Error:** Tests hang at testcontainer startup

**Solution:** Check Docker is running:

```bash
docker ps
# Ensure Docker daemon is accessible
```

## Comparison with Firestore

| Feature | PostgreSQL | Firestore |
|---------|-----------|-----------|
| **Self-hosted** | ✅ Yes | ❌ No (GCP only) |
| **Open source** | ✅ Yes | ❌ No |
| **Cost** | Infrastructure only | Per-operation + storage |
| **Transactions** | ACID | Limited |
| **Query flexibility** | SQL | Limited |
| **Scalability** | Vertical + horizontal | Auto-scale |
| **Latency** | Low (local) | Variable (network) |
| **Backup** | Standard tools | GCP snapshots |

## Production Deployment

### Docker Compose

See `docker-compose.yml` in the repository root for a complete example.

```yaml
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: maeve_csms
      POSTGRES_USER: maeve
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U maeve"]
      interval: 10s
      timeout: 5s
      retries: 5

  manager:
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      STORAGE_TYPE: postgres
      STORAGE_POSTGRES_HOST: postgres
      # ... other config
```

### Kubernetes

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: postgres-credentials
stringData:
  password: your-secure-password

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: maeve-manager
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: STORAGE_TYPE
          value: "postgres"
        - name: STORAGE_POSTGRES_HOST
          value: "postgres-service"
        - name: STORAGE_POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: postgres-credentials
              key: password
```

### Backup & Restore

```bash
# Backup
pg_dump -U maeve -h localhost maeve_csms > backup.sql

# Restore
psql -U maeve -h localhost maeve_csms < backup.sql

# Backup with compression
pg_dump -U maeve -h localhost -Fc maeve_csms > backup.dump

# Restore compressed
pg_restore -U maeve -h localhost -d maeve_csms backup.dump
```

## License

This implementation follows the same Apache-2.0 license as the main MaEVE CSMS project.

## Support

For issues or questions:
- Open an issue on GitHub
- Check the main MaEVE CSMS documentation
- Review PostgreSQL best practices: https://wiki.postgresql.org/wiki/Don%27t_Do_This
