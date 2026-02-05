-- OCPI registrations (by token)
CREATE TABLE ocpi_registrations (
    token VARCHAR(255) PRIMARY KEY,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- OCPI party details (by role + country_code + party_id)
CREATE TABLE ocpi_parties (
    id BIGSERIAL PRIMARY KEY,
    role VARCHAR(50) NOT NULL,
    country_code VARCHAR(2) NOT NULL,
    party_id VARCHAR(3) NOT NULL,
    url VARCHAR(1024) NOT NULL,
    token VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    CONSTRAINT ocpi_party_unique UNIQUE (role, country_code, party_id)
);

CREATE INDEX idx_ocpi_parties_role ON ocpi_parties(role);

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
