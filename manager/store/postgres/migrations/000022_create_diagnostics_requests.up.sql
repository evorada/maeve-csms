-- Diagnostics upload requests (OCPP 1.6 GetDiagnostics)
CREATE TABLE IF NOT EXISTS diagnostics_request (
    charge_station_id TEXT PRIMARY KEY,
    location TEXT NOT NULL,
    start_time TIMESTAMPTZ,
    stop_time TIMESTAMPTZ,
    retries INTEGER,
    retry_interval INTEGER,
    status TEXT NOT NULL DEFAULT 'Pending',
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Log upload requests (OCPP 2.0.1 GetLog)
CREATE TABLE IF NOT EXISTS log_request (
    charge_station_id TEXT PRIMARY KEY,
    log_type TEXT NOT NULL,
    request_id INTEGER NOT NULL,
    remote_location TEXT NOT NULL,
    oldest_timestamp TIMESTAMPTZ,
    latest_timestamp TIMESTAMPTZ,
    retries INTEGER,
    retry_interval INTEGER,
    status TEXT NOT NULL DEFAULT 'Pending',
    send_after TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
