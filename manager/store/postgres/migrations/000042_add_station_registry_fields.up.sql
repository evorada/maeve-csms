-- The registry fields of a charge station. The stations that are already there were
-- registered by an operator: they become active and keep authorising as before
-- (allow_any_token false).
ALTER TABLE charge_stations
    ADD COLUMN IF NOT EXISTS location_id      UUID REFERENCES asset_locations(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS display_name     TEXT,
    ADD COLUMN IF NOT EXISTS max_capacity_kw  NUMERIC(10,3),
    ADD COLUMN IF NOT EXISTS allow_any_token  BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS state            TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS state_changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS state_reason     TEXT,
    ADD CONSTRAINT charge_stations_state_check CHECK (state IN ('pending', 'active', 'decommissioned'));

CREATE INDEX IF NOT EXISTS idx_charge_stations_org_state ON charge_stations(org_id, state);
CREATE INDEX IF NOT EXISTS idx_charge_stations_location ON charge_stations(location_id);

-- What the station reported at its last boot, next to its vendor, model, serial number
-- and firmware version. The lengths are those of OCPP.
ALTER TABLE charge_station_runtime
    ADD COLUMN IF NOT EXISTS iccid               VARCHAR(20),
    ADD COLUMN IF NOT EXISTS imsi                VARCHAR(20),
    ADD COLUMN IF NOT EXISTS meter_serial_number VARCHAR(25),
    ADD COLUMN IF NOT EXISTS meter_type          VARCHAR(25),
    ADD COLUMN IF NOT EXISTS last_boot_at        TIMESTAMPTZ;
