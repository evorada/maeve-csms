-- Standalone meter values (not transaction-associated)
-- Received via MeterValues messages
CREATE TABLE meter_values (
    id BIGSERIAL PRIMARY KEY,
    charge_station_id VARCHAR(255) NOT NULL,
    evse_id INT NOT NULL,
    transaction_id VARCHAR(36), -- Optional, may be NULL if not associated with a transaction
    timestamp TIMESTAMP NOT NULL,
    sampled_values JSONB NOT NULL,
    received_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Index for querying by charge station and EVSE
CREATE INDEX idx_standalone_meter_values_station_evse ON meter_values(charge_station_id, evse_id);
-- Index for querying by timestamp
CREATE INDEX idx_standalone_meter_values_timestamp ON meter_values(timestamp);
-- Index for querying by transaction (when present)
CREATE INDEX idx_standalone_meter_values_transaction ON meter_values(transaction_id) WHERE transaction_id IS NOT NULL;
