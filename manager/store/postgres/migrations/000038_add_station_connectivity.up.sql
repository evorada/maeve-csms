-- When the websocket of a charge station opened and closed, the id of that socket and
-- the last sign of life of the station. charge_station_id is already VARCHAR(80)
-- (000035).
ALTER TABLE charge_station_status
    ADD COLUMN IF NOT EXISTS last_connected_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_disconnected_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_seen_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS connection_id        TEXT;

CREATE INDEX IF NOT EXISTS idx_charge_station_status_seen
    ON charge_station_status (last_seen_at) WHERE connected;
