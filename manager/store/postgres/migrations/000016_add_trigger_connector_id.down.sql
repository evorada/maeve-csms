-- Remove connector_id from charge_station_triggers
ALTER TABLE charge_station_triggers
    DROP COLUMN connector_id;
