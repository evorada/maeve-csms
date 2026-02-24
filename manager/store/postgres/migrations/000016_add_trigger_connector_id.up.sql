-- Add connector_id to charge_station_triggers
ALTER TABLE charge_station_triggers
    ADD COLUMN connector_id INTEGER;
