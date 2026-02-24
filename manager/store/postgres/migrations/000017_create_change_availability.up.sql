-- Change availability request tracking
CREATE TABLE IF NOT EXISTS charge_station_change_availability (
    charge_station_id TEXT PRIMARY KEY,
    connector_id INTEGER,
    evse_id INTEGER,
    availability_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'Pending',
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
