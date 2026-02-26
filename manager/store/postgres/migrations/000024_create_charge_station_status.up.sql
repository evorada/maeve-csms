CREATE TABLE IF NOT EXISTS charge_station_status (
    charge_station_id VARCHAR(36) PRIMARY KEY,
    connected BOOLEAN NOT NULL DEFAULT true,
    last_heartbeat TIMESTAMP WITH TIME ZONE,
    firmware_version TEXT,
    model TEXT,
    vendor TEXT,
    serial_number TEXT,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_charge_station_status_updated ON charge_station_status(updated_at);
CREATE INDEX idx_charge_station_status_connected ON charge_station_status(connected);
