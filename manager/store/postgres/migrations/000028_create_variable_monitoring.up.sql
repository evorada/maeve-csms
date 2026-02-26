CREATE TABLE IF NOT EXISTS variable_monitoring (
    id SERIAL PRIMARY KEY,
    charge_station_id TEXT NOT NULL,
    component_name TEXT NOT NULL,
    component_instance TEXT,
    variable_name TEXT NOT NULL,
    variable_instance TEXT,
    monitor_type TEXT NOT NULL,
    value DOUBLE PRECISION NOT NULL,
    severity INTEGER NOT NULL DEFAULT 0,
    transaction BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_variable_monitoring_cs FOREIGN KEY (charge_station_id)
        REFERENCES charge_station(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_variable_monitoring_cs ON variable_monitoring(charge_station_id);
