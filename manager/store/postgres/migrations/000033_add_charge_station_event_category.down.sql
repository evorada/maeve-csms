DROP INDEX IF EXISTS idx_cs_event_category;
ALTER TABLE charge_station_event DROP COLUMN IF EXISTS category;
