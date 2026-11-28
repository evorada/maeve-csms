-- The token groups, the default tariffs of the locations and the tariffs of the
-- transactions are lost.
ALTER TABLE transactions
    DROP COLUMN IF EXISTS tariff_source,
    DROP COLUMN IF EXISTS tariff_version,
    DROP COLUMN IF EXISTS tariff_id;

DROP INDEX IF EXISTS idx_asset_locations_default_tariff;
ALTER TABLE asset_locations DROP COLUMN IF EXISTS default_tariff_ref;

DROP INDEX IF EXISTS idx_tokens_token_group;
ALTER TABLE tokens DROP COLUMN IF EXISTS token_group_id;

DROP TABLE IF EXISTS token_groups;
