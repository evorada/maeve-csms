CREATE TABLE IF NOT EXISTS charge_station_certificate_queries (
    charge_station_id VARCHAR(28) PRIMARY KEY,
    certificate_type TEXT,
    query_status VARCHAR(20) NOT NULL,
    send_after TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS charge_station_certificate_deletions (
    charge_station_id VARCHAR(28) PRIMARY KEY,
    hash_algorithm VARCHAR(10) NOT NULL,
    issuer_name_hash VARCHAR(128) NOT NULL,
    issuer_key_hash VARCHAR(128) NOT NULL,
    serial_number VARCHAR(40) NOT NULL,
    deletion_status VARCHAR(20) NOT NULL,
    send_after TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
