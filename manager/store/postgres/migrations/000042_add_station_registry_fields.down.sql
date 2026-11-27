ALTER TABLE charge_station_runtime
    DROP COLUMN IF EXISTS last_boot_at,
    DROP COLUMN IF EXISTS meter_type,
    DROP COLUMN IF EXISTS meter_serial_number,
    DROP COLUMN IF EXISTS imsi,
    DROP COLUMN IF EXISTS iccid;

DROP INDEX IF EXISTS idx_charge_stations_location;
DROP INDEX IF EXISTS idx_charge_stations_org_state;

ALTER TABLE charge_stations
    DROP CONSTRAINT IF EXISTS charge_stations_state_check,
    DROP COLUMN IF EXISTS state_reason,
    DROP COLUMN IF EXISTS state_changed_at,
    DROP COLUMN IF EXISTS state,
    DROP COLUMN IF EXISTS allow_any_token,
    DROP COLUMN IF EXISTS max_capacity_kw,
    DROP COLUMN IF EXISTS display_name,
    DROP COLUMN IF EXISTS location_id;
