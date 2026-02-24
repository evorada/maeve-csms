-- Log upload status tracking (OCPP 2.0.1 GetLog)
CREATE TABLE IF NOT EXISTS log_status (
    charge_station_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'Idle',
    request_id INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
