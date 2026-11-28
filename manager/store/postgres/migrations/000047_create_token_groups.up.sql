-- Token groups of an org: a group can give its tokens a tariff of their own (the override
-- tariff), and with prefix_match the UIDs of its tokens are prefixes, so that every
-- presented UID that starts with one of them belongs to the group. They are not the OCPI
-- group of a token (tokens.group_id), which its issuer sets.
CREATE TABLE IF NOT EXISTS token_groups (
    id                  UUID PRIMARY KEY,
    org_id              UUID NOT NULL,
    name                TEXT NOT NULL,
    prefix_match        BOOLEAN NOT NULL DEFAULT false,
    override_tariff_ref UUID REFERENCES tariffs(id) ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT token_groups_name_unique UNIQUE (org_id, name)
);
CREATE INDEX IF NOT EXISTS idx_token_groups_org ON token_groups(org_id, id);
CREATE INDEX IF NOT EXISTS idx_token_groups_override_tariff ON token_groups(override_tariff_ref) WHERE override_tariff_ref IS NOT NULL;

-- A token is in one group at most; it leaves the group when the group is deleted.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS token_group_id UUID REFERENCES token_groups(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tokens_token_group ON tokens(token_group_id) WHERE token_group_id IS NOT NULL;

-- The tariff of the connectors of a location that have none of their own.
ALTER TABLE asset_locations ADD COLUMN IF NOT EXISTS default_tariff_ref UUID REFERENCES tariffs(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS idx_asset_locations_default_tariff ON asset_locations(default_tariff_ref) WHERE default_tariff_ref IS NOT NULL;

-- The tariff version that a transaction is charged with, chosen when it starts, and
-- where it came from (tariff_source). A transaction without a tariff has only the
-- source. The id is not a foreign key: a transaction outlives its tariff.
ALTER TABLE transactions
    ADD COLUMN IF NOT EXISTS tariff_id      UUID,
    ADD COLUMN IF NOT EXISTS tariff_version INT,
    ADD COLUMN IF NOT EXISTS tariff_source  TEXT;
