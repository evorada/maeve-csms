-- Native asset model: locations, the EVSEs of a charge station at a location, and the
-- connectors of an EVSE. The tables are prefixed asset_ because locations is the table
-- of the OCPI location documents.
CREATE TABLE IF NOT EXISTS asset_locations (
    id                   UUID PRIMARY KEY,
    org_id               UUID NOT NULL,
    party_ref            TEXT NOT NULL,
    ocpi_id              VARCHAR(36) NOT NULL,
    name                 TEXT,
    address              TEXT NOT NULL,
    city                 TEXT NOT NULL,
    postal_code          TEXT,
    state                TEXT,
    country              CHAR(3) NOT NULL,
    latitude             VARCHAR(12) NOT NULL,
    longitude            VARCHAR(12) NOT NULL,
    time_zone            TEXT NOT NULL,
    parking_type         TEXT,
    publish              BOOLEAN NOT NULL DEFAULT true,
    opening_times        JSONB,
    facilities           TEXT[] NOT NULL DEFAULT '{}',
    charging_when_closed BOOLEAN,
    external_refs        TEXT[] NOT NULL DEFAULT '{}',
    version              BIGINT NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_locations_ocpi_id_unique UNIQUE (party_ref, ocpi_id)
);
CREATE INDEX IF NOT EXISTS idx_asset_locations_org ON asset_locations(org_id, id);

CREATE TABLE IF NOT EXISTS asset_evses (
    id                 UUID PRIMARY KEY,
    org_id             UUID NOT NULL,
    location_id        UUID NOT NULL REFERENCES asset_locations(id) ON DELETE RESTRICT,
    station_key        VARCHAR(80) NOT NULL REFERENCES charge_stations(charge_station_id) ON DELETE RESTRICT,
    ocpp_evse_id       INT NOT NULL CHECK (ocpp_evse_id > 0),
    evse_id            VARCHAR(48),
    physical_reference VARCHAR(16),
    status             TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'PLANNED', 'REMOVED')),
    version            BIGINT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_evses_ocpp_unique UNIQUE (station_key, ocpp_evse_id)
);
-- eMI3 EVSE ids are unique across all orgs.
CREATE UNIQUE INDEX IF NOT EXISTS asset_evses_evse_id_unique ON asset_evses(evse_id) WHERE evse_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_asset_evses_location ON asset_evses(location_id);
CREATE INDEX IF NOT EXISTS idx_asset_evses_org ON asset_evses(org_id);

-- evse_uid is asset_evses.id, not the eMI3 asset_evses.evse_id. The tariff references
-- get their foreign keys with the tariffs table.
CREATE TABLE IF NOT EXISTS asset_connectors (
    id                 UUID PRIMARY KEY,
    org_id             UUID NOT NULL,
    evse_uid           UUID NOT NULL REFERENCES asset_evses(id) ON DELETE CASCADE,
    ocpp_connector_id  INT NOT NULL CHECK (ocpp_connector_id > 0),
    ocpi_id            VARCHAR(36) NOT NULL,
    standard           TEXT NOT NULL,
    format             TEXT NOT NULL CHECK (format IN ('SOCKET', 'CABLE')),
    power_type         TEXT NOT NULL,
    max_voltage        INT NOT NULL,
    max_amperage       INT NOT NULL,
    max_electric_power INT,
    tariff_ref         UUID,
    adhoc_tariff_ref   UUID,
    version            BIGINT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT asset_connectors_ocpp_unique UNIQUE (evse_uid, ocpp_connector_id),
    CONSTRAINT asset_connectors_ocpi_unique UNIQUE (evse_uid, ocpi_id)
);
CREATE INDEX IF NOT EXISTS idx_asset_connectors_org ON asset_connectors(org_id);
