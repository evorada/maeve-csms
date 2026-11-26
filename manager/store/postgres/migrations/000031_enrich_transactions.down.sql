-- Only works while transaction ids are still unique across charge stations and the
-- meter readings still fit in an INT.
DROP SEQUENCE IF EXISTS ocpp16_transaction_id_seq;
DROP INDEX IF EXISTS idx_transactions_active_connector;
CREATE INDEX IF NOT EXISTS idx_transactions_station_id ON transactions (charge_station_id);
DROP INDEX IF EXISTS idx_transaction_meter_values_tx;
CREATE INDEX IF NOT EXISTS idx_meter_values_transaction_id ON transaction_meter_values (transaction_id);
ALTER TABLE transaction_meter_values DROP CONSTRAINT IF EXISTS transaction_meter_values_transaction_fkey;
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS transactions_pkey;
ALTER TABLE transactions ADD CONSTRAINT transactions_pkey PRIMARY KEY (id);
ALTER TABLE transaction_meter_values
    ADD CONSTRAINT transaction_meter_values_transaction_id_fkey
    FOREIGN KEY (transaction_id) REFERENCES transactions (id) ON DELETE CASCADE;
ALTER TABLE transaction_meter_values DROP COLUMN IF EXISTS charge_station_id;
ALTER TABLE transactions
    DROP COLUMN IF EXISTS reservation_id,
    DROP COLUMN IF EXISTS id_token_group,
    DROP COLUMN IF EXISTS energy_wh,
    DROP COLUMN IF EXISTS connector_id,
    DROP COLUMN IF EXISTS evse_id;
UPDATE transactions SET meter_start = 0 WHERE meter_start IS NULL;
ALTER TABLE transactions
    ALTER COLUMN meter_start SET NOT NULL,
    ALTER COLUMN meter_start TYPE INT,
    ALTER COLUMN meter_stop TYPE INT;
