-- The org of every row is lost: after a new up migration all rows belong to the
-- legacy org again.
DROP INDEX IF EXISTS idx_charging_profiles_org;
DROP INDEX IF EXISTS idx_reservations_org;
DROP INDEX IF EXISTS idx_transactions_org;
DROP INDEX IF EXISTS idx_locations_org;
DROP INDEX IF EXISTS idx_charge_stations_org;

ALTER TABLE ocpi_parties       DROP COLUMN IF EXISTS org_id;
ALTER TABLE ocpi_registrations DROP COLUMN IF EXISTS org_id;
ALTER TABLE charging_profiles  DROP COLUMN IF EXISTS org_id;
ALTER TABLE reservations       DROP COLUMN IF EXISTS org_id;
ALTER TABLE transactions       DROP COLUMN IF EXISTS org_id;
ALTER TABLE tokens             DROP COLUMN IF EXISTS org_id;
ALTER TABLE locations          DROP COLUMN IF EXISTS org_id;
ALTER TABLE charge_stations    DROP COLUMN IF EXISTS org_id;

-- Back to the old widths. This fails ("value too long") while a station key that is
-- longer than the old width is stored: such rows have to be removed by hand first,
-- the migration does not truncate or delete them.
ALTER TABLE device_report                        ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_event                 ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE variable_monitoring                  ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_certificate_deletions ALTER COLUMN charge_station_id TYPE VARCHAR(28);
ALTER TABLE charge_station_certificate_queries   ALTER COLUMN charge_station_id TYPE VARCHAR(28);
ALTER TABLE remote_stop_transaction_requests     ALTER COLUMN charge_station_id TYPE VARCHAR(28);
ALTER TABLE remote_start_transaction_requests    ALTER COLUMN charge_station_id TYPE VARCHAR(28);
ALTER TABLE connector_status                     ALTER COLUMN charge_station_id TYPE VARCHAR(36);
ALTER TABLE charge_station_status                ALTER COLUMN charge_station_id TYPE VARCHAR(36);
ALTER TABLE transaction_meter_values             ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE transactions                         ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_triggers              ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_certificates          ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_runtime               ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_station_settings              ALTER COLUMN charge_station_id TYPE VARCHAR(48);
ALTER TABLE charge_stations                      ALTER COLUMN charge_station_id TYPE VARCHAR(48);
