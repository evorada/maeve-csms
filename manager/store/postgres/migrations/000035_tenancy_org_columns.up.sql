-- 1. Station keys of up to 80 characters: "{org_key}.{station id}" with an org key of
--    up to 8 characters and a station id of up to 48, plus margin. Widening a VARCHAR
--    does not rewrite the table. The station columns of the other tables are TEXT or
--    VARCHAR(255) already.
ALTER TABLE charge_stations                      ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_settings              ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_runtime               ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_certificates          ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_triggers              ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE transactions                         ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE transaction_meter_values             ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_status                ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE connector_status                     ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE remote_start_transaction_requests    ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE remote_stop_transaction_requests     ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_certificate_queries   ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_certificate_deletions ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE variable_monitoring                  ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE charge_station_event                 ALTER COLUMN charge_station_id TYPE VARCHAR(80);
ALTER TABLE device_report                        ALTER COLUMN charge_station_id TYPE VARCHAR(80);

-- 2. The org that owns a row. The rows that are already there get the legacy org
--    (the nil UUID), which the manager remaps to its default org when it starts. The
--    default is dropped again: a new row must say which org it belongs to.
ALTER TABLE charge_stations    ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE locations          ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE tokens             ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE transactions       ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE reservations       ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE charging_profiles  ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE ocpi_registrations ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE ocpi_parties       ADD COLUMN org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE charge_stations    ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE locations          ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE tokens             ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE transactions       ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE reservations       ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE charging_profiles  ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE ocpi_registrations ALTER COLUMN org_id DROP DEFAULT;
ALTER TABLE ocpi_parties       ALTER COLUMN org_id DROP DEFAULT;

CREATE INDEX idx_charge_stations_org   ON charge_stations(org_id);
CREATE INDEX idx_locations_org         ON locations(org_id);
CREATE INDEX idx_transactions_org      ON transactions(org_id, start_timestamp);
CREATE INDEX idx_reservations_org      ON reservations(org_id);
CREATE INDEX idx_charging_profiles_org ON charging_profiles(org_id);
