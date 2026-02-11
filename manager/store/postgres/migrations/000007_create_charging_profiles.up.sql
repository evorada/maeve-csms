CREATE TABLE IF NOT EXISTS charging_profiles (
    id SERIAL PRIMARY KEY,
    charge_station_id TEXT NOT NULL,
    connector_id INTEGER NOT NULL DEFAULT 0,
    charging_profile_id INTEGER NOT NULL,
    transaction_id INTEGER,
    stack_level INTEGER NOT NULL DEFAULT 0,
    charging_profile_purpose TEXT NOT NULL,
    charging_profile_kind TEXT NOT NULL,
    recurrency_kind TEXT,
    valid_from TIMESTAMP,
    valid_to TIMESTAMP,
    charging_rate_unit TEXT NOT NULL,
    duration INTEGER,
    start_schedule TIMESTAMP,
    min_charging_rate DOUBLE PRECISION,
    charging_schedule_periods JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(charge_station_id, charging_profile_id)
);

CREATE INDEX idx_charging_profiles_station ON charging_profiles(charge_station_id);
CREATE INDEX idx_charging_profiles_station_connector ON charging_profiles(charge_station_id, connector_id);
CREATE INDEX idx_charging_profiles_purpose ON charging_profiles(charge_station_id, charging_profile_purpose);
