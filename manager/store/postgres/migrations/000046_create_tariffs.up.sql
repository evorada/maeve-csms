-- Tariffs in the OCPI 2.2.1 model. A tariff has a name and versions. A version holds
-- what a session is charged (currency, elements with their price components and
-- restrictions, limits) and is never changed after it is stored: a change of the tariff
-- is a new version, and current_version says which one is in use. A deleted tariff
-- keeps its row (deleted_at) and its versions.
CREATE TABLE IF NOT EXISTS tariffs (
    id              UUID PRIMARY KEY,
    org_id          UUID NOT NULL,
    party_ref       TEXT NOT NULL,
    ocpi_id         VARCHAR(36) NOT NULL,
    name            TEXT NOT NULL,
    current_version INT NOT NULL CHECK (current_version > 0),
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tariffs_ocpi_id_unique UNIQUE (party_ref, ocpi_id)
);
CREATE INDEX IF NOT EXISTS idx_tariffs_org ON tariffs(org_id, id);

-- The versions go with their tariff only when the row of the tariff is removed, which
-- nothing but the purge of a tenant does.
CREATE TABLE IF NOT EXISTS tariff_versions (
    tariff_id          UUID NOT NULL REFERENCES tariffs(id) ON DELETE CASCADE,
    version            INT NOT NULL CHECK (version > 0),
    currency           CHAR(3) NOT NULL,
    type               TEXT CHECK (type IN ('AD_HOC_PAYMENT', 'PROFILE_CHEAP', 'PROFILE_FAST', 'PROFILE_GREEN', 'REGULAR')),
    tariff_alt_text    JSONB NOT NULL DEFAULT '[]',
    tariff_alt_url     TEXT,
    min_price_excl_vat NUMERIC(14,4),
    min_price_incl_vat NUMERIC(14,4),
    max_price_excl_vat NUMERIC(14,4),
    max_price_incl_vat NUMERIC(14,4),
    start_date_time    TIMESTAMPTZ,
    end_date_time      TIMESTAMPTZ,
    elements           JSONB NOT NULL,
    energy_mix         JSONB,
    idle_detection     JSONB,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         TEXT NOT NULL,
    PRIMARY KEY (tariff_id, version),
    -- a price has both of its amounts, or it is not there
    CONSTRAINT tariff_versions_min_price_check CHECK ((min_price_excl_vat IS NULL) = (min_price_incl_vat IS NULL)),
    CONSTRAINT tariff_versions_max_price_check CHECK ((max_price_excl_vat IS NULL) = (max_price_incl_vat IS NULL))
);

CREATE OR REPLACE FUNCTION tariff_versions_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'tariff_versions rows are immutable';
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tariff_versions_no_update ON tariff_versions;
CREATE TRIGGER tariff_versions_no_update BEFORE UPDATE ON tariff_versions
    FOR EACH ROW EXECUTE FUNCTION tariff_versions_immutable();

-- The tariff references of the connectors could hold any UUID until now. A reference
-- that names no tariff is cleared (with a new version of the connector), as the foreign
-- keys would refuse it.
UPDATE asset_connectors c SET
    tariff_ref = (SELECT t.id FROM tariffs t WHERE t.id = c.tariff_ref),
    adhoc_tariff_ref = (SELECT t.id FROM tariffs t WHERE t.id = c.adhoc_tariff_ref),
    version = c.version + 1,
    updated_at = NOW()
WHERE (c.tariff_ref IS NOT NULL AND NOT EXISTS (SELECT 1 FROM tariffs t WHERE t.id = c.tariff_ref))
   OR (c.adhoc_tariff_ref IS NOT NULL AND NOT EXISTS (SELECT 1 FROM tariffs t WHERE t.id = c.adhoc_tariff_ref));

ALTER TABLE asset_connectors
    ADD CONSTRAINT asset_connectors_tariff_fk FOREIGN KEY (tariff_ref) REFERENCES tariffs(id) ON DELETE RESTRICT,
    ADD CONSTRAINT asset_connectors_adhoc_tariff_fk FOREIGN KEY (adhoc_tariff_ref) REFERENCES tariffs(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_asset_connectors_tariff ON asset_connectors(tariff_ref) WHERE tariff_ref IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_asset_connectors_adhoc_tariff ON asset_connectors(adhoc_tariff_ref) WHERE adhoc_tariff_ref IS NOT NULL;
