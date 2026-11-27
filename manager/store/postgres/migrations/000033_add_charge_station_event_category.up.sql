-- Tells security events and log status changes apart from device model events.
-- Events that were stored before get the category 'device'.
ALTER TABLE charge_station_event ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'device';
CREATE INDEX IF NOT EXISTS idx_cs_event_category ON charge_station_event (charge_station_id, category, timestamp DESC);
