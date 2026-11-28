-- name: CreateTariff :one
INSERT INTO tariffs (id, org_id, party_ref, ocpi_id, name, current_version)
VALUES ($1, $2, $3, $4, $5, 1)
RETURNING *;

-- name: CreateTariffVersion :one
INSERT INTO tariff_versions (
    tariff_id, version, currency, type, tariff_alt_text, tariff_alt_url,
    min_price_excl_vat, min_price_incl_vat, max_price_excl_vat, max_price_incl_vat,
    start_date_time, end_date_time, elements, energy_mix, idle_detection, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
RETURNING *;

-- A tariff moves to a new version only when it still has the version that the caller
-- saw and is not deleted: no row is changed otherwise. The row stays locked until the
-- transaction ends, so that two new versions of one tariff are made one after the other.
-- name: BumpTariffCurrentVersion :execrows
UPDATE tariffs SET current_version = @new_version, updated_at = NOW()
WHERE id = @id AND current_version = @expected_version AND deleted_at IS NULL;

-- A deleted tariff takes no new name: no row is changed.
-- name: RenameTariff :execrows
UPDATE tariffs SET name = @name, updated_at = NOW()
WHERE id = @id AND deleted_at IS NULL;

-- The tariff reads return the tariff with its current version.

-- name: GetTariff :one
SELECT sqlc.embed(t), sqlc.embed(v) FROM tariffs t
JOIN tariff_versions v ON v.tariff_id = t.id AND v.version = t.current_version
WHERE t.id = $1;

-- name: GetTariffByOcpiId :one
SELECT sqlc.embed(t), sqlc.embed(v) FROM tariffs t
JOIN tariff_versions v ON v.tariff_id = t.id AND v.version = t.current_version
WHERE t.party_ref = $1 AND t.ocpi_id = $2;

-- name: GetTariffVersion :one
SELECT * FROM tariff_versions WHERE tariff_id = $1 AND version = $2;

-- The query is a LIKE pattern: the store escapes %, _ and \ in it. The type is the type
-- of the current version.
-- name: ListTariffs :many
SELECT sqlc.embed(t), sqlc.embed(v) FROM tariffs t
JOIN tariff_versions v ON v.tariff_id = t.id AND v.version = t.current_version
WHERE t.org_id = ANY(sqlc.arg('org_ids')::uuid[])
  AND (sqlc.narg('party_ref')::text IS NULL OR t.party_ref = sqlc.narg('party_ref')::text)
  AND (sqlc.narg('type')::text IS NULL OR v.type = sqlc.narg('type')::text)
  AND (sqlc.narg('query')::text IS NULL
       OR t.name ILIKE '%' || sqlc.narg('query')::text || '%'
       OR t.ocpi_id ILIKE '%' || sqlc.narg('query')::text || '%')
  AND (sqlc.arg('include_deleted')::boolean OR t.deleted_at IS NULL)
  AND t.id > sqlc.arg('after_id')::uuid
ORDER BY t.id
LIMIT sqlc.arg('row_limit');

-- name: ListTariffVersions :many
SELECT * FROM tariff_versions WHERE tariff_id = $1 ORDER BY version DESC;

-- The records that refer to a tariff: the connectors that use it as their tariff or as
-- their ad hoc tariff, the locations that have it as their default tariff and the token
-- groups that have it as their override tariff.
-- name: CountTariffReferences :one
SELECT
    (SELECT count(*) FROM asset_connectors c
     WHERE c.tariff_ref = sqlc.arg('tariff_id')::uuid OR c.adhoc_tariff_ref = sqlc.arg('tariff_id')::uuid)::bigint AS connectors,
    (SELECT count(*) FROM asset_locations l WHERE l.default_tariff_ref = sqlc.arg('tariff_id')::uuid)::bigint AS locations,
    (SELECT count(*) FROM token_groups g WHERE g.override_tariff_ref = sqlc.arg('tariff_id')::uuid)::bigint AS token_groups;

-- name: SoftDeleteTariff :exec
UPDATE tariffs SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;
