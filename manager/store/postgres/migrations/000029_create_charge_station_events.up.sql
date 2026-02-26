CREATE TABLE IF NOT EXISTS charge_station_event (
    id SERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    event_type TEXT NOT NULL,
    tech_code TEXT,
    tech_info TEXT,
    event_data TEXT,
    component_id TEXT,
    variable_id TEXT,
    cleared BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_cs_event_cs FOREIGN KEY (charge_station_id)
        REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cs_event_cs ON charge_station_event(charge_station_id);
CREATE INDEX IF NOT EXISTS idx_cs_event_timestamp ON charge_station_event(charge_station_id, timestamp DESC);
