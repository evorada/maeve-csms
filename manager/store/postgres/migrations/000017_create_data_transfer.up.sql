-- Data transfer request tracking
CREATE TABLE IF NOT EXISTS charge_station_data_transfer (
    charge_station_id TEXT PRIMARY KEY,
    vendor_id TEXT NOT NULL,
    message_id TEXT,
    data TEXT,
    status TEXT NOT NULL DEFAULT 'Pending',
    response_data TEXT,
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
