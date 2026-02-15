-- Publish firmware status tracking (OCPP 2.0.1 PublishFirmware)
CREATE TABLE IF NOT EXISTS publish_firmware_status (
    charge_station_id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'Idle',
    location TEXT NOT NULL DEFAULT '',
    checksum TEXT NOT NULL DEFAULT '',
    request_id INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
