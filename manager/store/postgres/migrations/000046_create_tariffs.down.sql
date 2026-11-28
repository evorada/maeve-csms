-- The tariffs and their versions are lost. The connectors keep the ids in their tariff
-- references, which name nothing any more: a new up migration clears them.
DROP INDEX IF EXISTS idx_asset_connectors_adhoc_tariff;
DROP INDEX IF EXISTS idx_asset_connectors_tariff;
ALTER TABLE asset_connectors
    DROP CONSTRAINT IF EXISTS asset_connectors_adhoc_tariff_fk,
    DROP CONSTRAINT IF EXISTS asset_connectors_tariff_fk;
DROP TRIGGER IF EXISTS tariff_versions_no_update ON tariff_versions;
DROP FUNCTION IF EXISTS tariff_versions_immutable();
DROP TABLE IF EXISTS tariff_versions;
DROP TABLE IF EXISTS tariffs;
