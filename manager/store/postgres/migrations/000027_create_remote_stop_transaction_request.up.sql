CREATE TABLE IF NOT EXISTS remote_stop_transaction_requests (
    charge_station_id VARCHAR(28) PRIMARY KEY,
    transaction_id VARCHAR(36) NOT NULL,
    status VARCHAR(20) NOT NULL,
    send_after TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_type VARCHAR(10) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
