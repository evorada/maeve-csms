-- Clear cache request tracking
CREATE TABLE IF NOT EXISTS charge_station_clear_cache (
    charge_station_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'Pending',
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
