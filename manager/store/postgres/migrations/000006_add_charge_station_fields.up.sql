-- Add missing fields to charge_station_certificates
ALTER TABLE charge_station_certificates
    ADD COLUMN certificate_id VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN certificate_installation_status VARCHAR(50) NOT NULL DEFAULT 'Pending',
    ADD COLUMN send_after TIMESTAMP NOT NULL DEFAULT NOW();

-- Add missing fields to charge_station_triggers  
ALTER TABLE charge_station_triggers
    ADD COLUMN trigger_status VARCHAR(50) NOT NULL DEFAULT 'Pending',
    ADD COLUMN send_after TIMESTAMP NOT NULL DEFAULT NOW();

-- Drop the defaults after adding the columns (they were just for existing rows)
ALTER TABLE charge_station_certificates
    ALTER COLUMN certificate_id DROP DEFAULT,
    ALTER COLUMN certificate_installation_status DROP DEFAULT,
    ALTER COLUMN send_after DROP DEFAULT;

ALTER TABLE charge_station_triggers
    ALTER COLUMN trigger_status DROP DEFAULT,
    ALTER COLUMN send_after DROP DEFAULT;

-- Make charge_station_triggers unique per station (only one trigger per station)
-- First, remove duplicates if any exist (keep most recent)
DELETE FROM charge_station_triggers a USING charge_station_triggers b
WHERE a.id < b.id AND a.charge_station_id = b.charge_station_id;

-- Add unique constraint
ALTER TABLE charge_station_triggers
    DROP CONSTRAINT IF EXISTS charge_station_triggers_station_unique,
    ADD CONSTRAINT charge_station_triggers_station_unique UNIQUE (charge_station_id);
