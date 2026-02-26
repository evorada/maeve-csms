CREATE TABLE IF NOT EXISTS connector_status (
    charge_station_id VARCHAR(36) NOT NULL,
    connector_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT 'NoError',
    info TEXT,
    timestamp TIMESTAMP WITH TIME ZONE,
    vendor_error_code TEXT,
    vendor_id TEXT,
    current_transaction_id TEXT,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (charge_station_id, connector_id)
);

CREATE INDEX idx_connector_status_updated ON connector_status(updated_at);
CREATE INDEX idx_connector_status_station ON connector_status(charge_station_id);
CREATE INDEX idx_connector_status_status ON connector_status(status);
