DROP INDEX IF EXISTS idx_charge_station_status_seen;

ALTER TABLE charge_station_status
    DROP COLUMN IF EXISTS connection_id,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS last_disconnected_at,
    DROP COLUMN IF EXISTS last_connected_at;
