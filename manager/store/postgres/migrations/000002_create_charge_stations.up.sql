-- Charge station authentication
CREATE TABLE charge_stations (
    charge_station_id VARCHAR(48) PRIMARY KEY,
    security_profile INT NOT NULL,
    base64_sha256_password VARCHAR(255),
    invalid_username_allowed BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station settings
CREATE TABLE charge_station_settings (
    charge_station_id VARCHAR(48) PRIMARY KEY REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    settings JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station runtime details
CREATE TABLE charge_station_runtime (
    charge_station_id VARCHAR(48) PRIMARY KEY REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    ocpp_version VARCHAR(10) NOT NULL,
    vendor VARCHAR(255),
    model VARCHAR(255),
    serial_number VARCHAR(255),
    firmware_version VARCHAR(255),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Charge station certificates to install
CREATE TABLE charge_station_certificates (
    id BIGSERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    certificate_type VARCHAR(50) NOT NULL,
    certificate TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cs_certificates_station_id ON charge_station_certificates(charge_station_id);

-- Charge station trigger messages
CREATE TABLE charge_station_triggers (
    id BIGSERIAL PRIMARY KEY,
    charge_station_id VARCHAR(48) NOT NULL REFERENCES charge_stations(charge_station_id) ON DELETE CASCADE,
    message_type VARCHAR(100) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cs_triggers_station_id ON charge_station_triggers(charge_station_id);
