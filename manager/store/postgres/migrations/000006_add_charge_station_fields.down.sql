-- Remove unique constraint from charge_station_triggers
ALTER TABLE charge_station_triggers
    DROP CONSTRAINT IF EXISTS charge_station_triggers_station_unique;

-- Remove added fields from charge_station_triggers
ALTER TABLE charge_station_triggers
    DROP COLUMN IF EXISTS send_after,
    DROP COLUMN IF EXISTS trigger_status;

-- Remove added fields from charge_station_certificates
ALTER TABLE charge_station_certificates
    DROP COLUMN IF EXISTS send_after,
    DROP COLUMN IF EXISTS certificate_installation_status,
    DROP COLUMN IF EXISTS certificate_id;
