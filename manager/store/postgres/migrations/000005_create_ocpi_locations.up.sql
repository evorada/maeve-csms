-- OCPI registrations
CREATE TABLE ocpi_registrations (
    id BIGSERIAL PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    status VARCHAR(50) NOT NULL,
    token VARCHAR(255) NOT NULL,
    url VARCHAR(1024) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    CONSTRAINT ocpi_country_party_unique UNIQUE (country_code, party_id)
);

-- Locations
CREATE TABLE locations (
    id VARCHAR(48) PRIMARY KEY,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    location_data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_locations_country_party ON locations(country_code, party_id);
