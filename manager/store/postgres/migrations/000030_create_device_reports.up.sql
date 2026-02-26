CREATE TABLE IF NOT EXISTS device_report (
    id SERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL,
    request_id INTEGER NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL,
    report_type TEXT,
    report_data JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_device_report_cs FOREIGN KEY (charge_station_id)
        REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_device_report_cs ON device_report(charge_station_id);
CREATE INDEX IF NOT EXISTS idx_device_report_generated ON device_report(charge_station_id, generated_at DESC);
