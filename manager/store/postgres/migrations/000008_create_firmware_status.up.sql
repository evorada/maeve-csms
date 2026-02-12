-- Firmware update status tracking
CREATE TABLE IF NOT EXISTS firmware_update_status (
    charge_station_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'Idle',
    location TEXT NOT NULL DEFAULT '',
    retrieve_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retry_count INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Diagnostics status tracking
CREATE TABLE IF NOT EXISTS diagnostics_status (
    charge_station_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'Idle',
    location TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
