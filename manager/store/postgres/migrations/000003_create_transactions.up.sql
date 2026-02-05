CREATE TABLE transactions (
    id VARCHAR(36) PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL,
    token_uid VARCHAR(36) NOT NULL,
    token_type VARCHAR(50) NOT NULL,
    meter_start INT NOT NULL,
    meter_stop INT,
    start_timestamp TIMESTAMP NOT NULL,
    stop_timestamp TIMESTAMP,
    stopped_reason VARCHAR(100),
    updated_seq_no INT NOT NULL DEFAULT 0,
    offline BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transactions_station_id ON transactions(charge_station_id);
CREATE INDEX idx_transactions_token_uid ON transactions(token_uid);
CREATE INDEX idx_transactions_start_time ON transactions(start_timestamp);
CREATE INDEX idx_transactions_stop_time ON transactions(stop_timestamp);

-- Meter values for transactions
CREATE TABLE transaction_meter_values (
    id BIGSERIAL PRIMARY KEY,
    transaction_id VARCHAR(36) NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    timestamp TIMESTAMP NOT NULL,
    sampled_values JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_meter_values_transaction_id ON transaction_meter_values(transaction_id);
CREATE INDEX idx_meter_values_timestamp ON transaction_meter_values(timestamp);
