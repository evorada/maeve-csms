CREATE TABLE IF NOT EXISTS remote_start_transaction_requests (
    charge_station_id VARCHAR(28) PRIMARY KEY,
    id_tag VARCHAR(20) NOT NULL,
    connector_id INT,
    charging_profile TEXT,
    status VARCHAR(20) NOT NULL,
    send_after TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    request_type VARCHAR(10) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
