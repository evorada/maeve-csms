#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
TEST_DB="maeve_migration_test_$$"
DB_USER="${POSTGRES_USER:-postgres}"
DB_HOST="${POSTGRES_HOST:-localhost}"
DB_PORT="${POSTGRES_PORT:-5432}"
MIGRATIONS_DIR="./migrations"

echo -e "${YELLOW}=== PostgreSQL Migration Rollback Test ===${NC}"
echo "Test database: $TEST_DB"
echo "PostgreSQL: $DB_USER@$DB_HOST:$DB_PORT"
echo ""

# Function to run psql command
run_psql() {
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$1" -c "$2" -t -A 2>/dev/null || echo "ERROR"
}

# Function to check if database exists
db_exists() {
    result=$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -lqt 2>/dev/null | cut -d \| -f 1 | grep -w "$1" | wc -l | tr -d ' ')
    [ "$result" -eq 1 ]
}

# Function to get table count
get_table_count() {
    run_psql "$TEST_DB" "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';"
}

# Function to check migration version
get_migration_version() {
    version=$(run_psql "$TEST_DB" "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;" 2>/dev/null)
    if [ "$version" = "ERROR" ] || [ -z "$version" ]; then
        echo "0"
    else
        echo "$version"
    fi
}

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    if db_exists "$TEST_DB"; then
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -c "DROP DATABASE IF EXISTS $TEST_DB;" postgres 2>/dev/null
        echo -e "${GREEN}✓ Dropped test database${NC}"
    fi
}

# Set trap to cleanup on exit
trap cleanup EXIT

# Step 1: Create test database
echo -e "${YELLOW}Step 1: Creating test database...${NC}"
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -c "DROP DATABASE IF EXISTS $TEST_DB;" postgres >/dev/null 2>&1
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -c "CREATE DATABASE $TEST_DB;" postgres >/dev/null 2>&1
if db_exists "$TEST_DB"; then
    echo -e "${GREEN}✓ Test database created${NC}"
else
    echo -e "${RED}✗ Failed to create test database${NC}"
    exit 1
fi

# Build connection string
DATABASE_URL="postgres://$DB_USER@$DB_HOST:$DB_PORT/$TEST_DB?sslmode=disable"

# Step 2: Check initial state
echo ""
echo -e "${YELLOW}Step 2: Checking initial state...${NC}"
initial_tables=$(get_table_count)
echo "Tables before migration: $initial_tables"
if [ "$initial_tables" = "0" ]; then
    echo -e "${GREEN}✓ Database is empty${NC}"
else
    echo -e "${RED}✗ Database should be empty${NC}"
    exit 1
fi

# Step 3: Run all migrations up
echo ""
echo -e "${YELLOW}Step 3: Running all migrations up...${NC}"
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" up 2>&1 | grep -v "no change" || true

tables_after_up=$(get_table_count)
migration_version=$(get_migration_version)
echo "Tables after migration: $tables_after_up"
echo "Migration version: $migration_version"

if [ "$tables_after_up" -gt "0" ]; then
    echo -e "${GREEN}✓ Migrations applied successfully${NC}"
else
    echo -e "${RED}✗ No tables created${NC}"
    exit 1
fi

# List all tables
echo ""
echo "Tables created:"
run_psql "$TEST_DB" "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name;" | while read table; do
    echo "  - $table"
done

# Step 4: Test rollback one step at a time
echo ""
echo -e "${YELLOW}Step 4: Testing incremental rollback...${NC}"

# Count migrations
migration_count=$(ls -1 "$MIGRATIONS_DIR"/*.up.sql | wc -l | tr -d ' ')
echo "Total migrations: $migration_count"

for i in $(seq 1 $migration_count); do
    current_version=$(get_migration_version)
    current_tables=$(get_table_count)
    
    echo ""
    echo -e "${YELLOW}Rollback $i/$migration_count (from version $current_version)...${NC}"
    
    # Run down migration
    migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" down 1 2>&1 | grep -v "no change" || true
    
    new_version=$(get_migration_version)
    new_tables=$(get_table_count)
    
    echo "  Version: $current_version → $new_version"
    echo "  Tables: $current_tables → $new_tables"
    
    if [ "$new_version" -lt "$current_version" ] || [ "$i" -eq "$migration_count" ]; then
        echo -e "  ${GREEN}✓ Rollback successful${NC}"
    else
        echo -e "  ${RED}✗ Rollback failed${NC}"
        exit 1
    fi
done

# Step 5: Verify database is clean
echo ""
echo -e "${YELLOW}Step 5: Verifying clean state...${NC}"
final_tables=$(get_table_count)
echo "Final table count: $final_tables"

if [ "$final_tables" = "0" ]; then
    echo -e "${GREEN}✓ All migrations rolled back successfully${NC}"
else
    echo -e "${RED}✗ Some tables remain after rollback${NC}"
    echo "Remaining tables:"
    run_psql "$TEST_DB" "SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE';" | while read table; do
        echo "  - $table"
    done
    exit 1
fi

# Step 6: Test full migration cycle again
echo ""
echo -e "${YELLOW}Step 6: Testing full migration cycle...${NC}"
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" up >/dev/null 2>&1
tables_after_reup=$(get_table_count)

if [ "$tables_after_reup" = "$tables_after_up" ]; then
    echo -e "${GREEN}✓ Re-migration successful (same table count)${NC}"
else
    echo -e "${RED}✗ Re-migration produced different result${NC}"
    exit 1
fi

# Step 7: Test complete rollback
echo ""
echo -e "${YELLOW}Step 7: Testing complete rollback...${NC}"
migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" down >/dev/null 2>&1
final_tables_2=$(get_table_count)

if [ "$final_tables_2" = "0" ]; then
    echo -e "${GREEN}✓ Complete rollback successful${NC}"
else
    echo -e "${RED}✗ Complete rollback failed${NC}"
    exit 1
fi

# Summary
echo ""
echo -e "${GREEN}=== All Migration Tests Passed! ===${NC}"
echo ""
echo "Summary:"
echo "  ✓ Created and cleaned test database"
echo "  ✓ Applied $migration_count migrations successfully"
echo "  ✓ Rolled back migrations incrementally"
echo "  ✓ Verified clean state after rollback"
echo "  ✓ Re-applied migrations successfully"
echo "  ✓ Complete rollback successful"
echo ""
echo -e "${GREEN}Migration rollback functionality is working correctly!${NC}"
