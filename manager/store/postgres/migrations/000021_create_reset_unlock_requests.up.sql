-- Reset request tracking
CREATE TABLE IF NOT EXISTS reset_request (
    charge_station_id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unlock connector request tracking
CREATE TABLE IF NOT EXISTS unlock_connector_request (
    charge_station_id TEXT PRIMARY KEY,
    connector_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
