-- Firmware update request tracking
CREATE TABLE IF NOT EXISTS firmware_update_request (
    charge_station_id TEXT PRIMARY KEY,
    location TEXT NOT NULL,
    retrieve_date TIMESTAMPTZ,
    retries INTEGER,
    retry_interval INTEGER,
    signature TEXT,
    signing_certificate TEXT,
    status TEXT NOT NULL DEFAULT 'Pending',
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
