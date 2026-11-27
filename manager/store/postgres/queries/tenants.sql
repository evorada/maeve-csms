-- The path column is an ltree, which sqlc cannot type: it is always read as text and
-- written through a text cast. The hostnames of a tenant are read with the tenant, in
-- the same statement.

-- name: GetTenant :one
SELECT t.org_id, t.parent_id, t.path::text AS path, t.slug, t.org_key, t.parties,
       t.entitlements_version, t.entitlements, t.config_version, t.config, t.status,
       t.created_at, t.updated_at,
       ARRAY(SELECT h.hostname FROM tenant_hostnames h
             WHERE h.org_id = t.org_id ORDER BY h.hostname)::text[] AS hostnames
FROM tenants t
WHERE t.org_id = $1;

-- name: GetTenantByOrgKey :one
SELECT t.org_id, t.parent_id, t.path::text AS path, t.slug, t.org_key, t.parties,
       t.entitlements_version, t.entitlements, t.config_version, t.config, t.status,
       t.created_at, t.updated_at,
       ARRAY(SELECT h.hostname FROM tenant_hostnames h
             WHERE h.org_id = t.org_id ORDER BY h.hostname)::text[] AS hostnames
FROM tenants t
WHERE t.org_key = $1;

-- name: GetTenantByHost :one
SELECT t.org_id, t.parent_id, t.path::text AS path, t.slug, t.org_key, t.parties,
       t.entitlements_version, t.entitlements, t.config_version, t.config, t.status,
       t.created_at, t.updated_at,
       ARRAY(SELECT h.hostname FROM tenant_hostnames h
             WHERE h.org_id = t.org_id ORDER BY h.hostname)::text[] AS hostnames
FROM tenants t
JOIN tenant_hostnames th USING (org_id)
WHERE th.hostname = $1;

-- name: ListTenants :many
SELECT t.org_id, t.parent_id, t.path::text AS path, t.slug, t.org_key, t.parties,
       t.entitlements_version, t.entitlements, t.config_version, t.config, t.status,
       t.created_at, t.updated_at,
       ARRAY(SELECT h.hostname FROM tenant_hostnames h
             WHERE h.org_id = t.org_id ORDER BY h.hostname)::text[] AS hostnames
FROM tenants t
WHERE sqlc.narg(after_org_id)::uuid IS NULL OR t.org_id > sqlc.narg(after_org_id)::uuid
ORDER BY t.org_id
LIMIT sqlc.arg(page_size);

-- name: ListSubtreeOrgIds :many
SELECT org_id FROM tenants
WHERE org_id = sqlc.arg(org_id) OR path ~ (sqlc.arg(lquery)::text)::lquery
ORDER BY org_id;

-- name: GetTenantIdentityForUpdate :one
SELECT slug, org_key FROM tenants WHERE org_id = $1 FOR UPDATE;

-- name: InsertTenant :exec
INSERT INTO tenants (
    org_id, parent_id, path, slug, org_key, parties,
    entitlements_version, entitlements, config_version, config
) VALUES (
    sqlc.arg(org_id), sqlc.narg(parent_id), sqlc.arg(path)::text::ltree, sqlc.arg(slug),
    sqlc.arg(org_key), sqlc.arg(parties),
    sqlc.arg(entitlements_version), sqlc.arg(entitlements), sqlc.arg(config_version), sqlc.arg(config)
);

-- name: UpdateTenant :exec
-- The entitlement and config documents are only replaced by a document of the same or
-- a newer version. The status, the slug and the org key are never changed here.
UPDATE tenants SET
    parent_id = sqlc.narg(parent_id),
    path = sqlc.arg(path)::text::ltree,
    parties = sqlc.arg(parties),
    entitlements = CASE WHEN sqlc.arg(entitlements_version)::bigint >= entitlements_version
                        THEN sqlc.arg(entitlements)::jsonb ELSE entitlements END,
    entitlements_version = GREATEST(entitlements_version, sqlc.arg(entitlements_version)::bigint),
    config = CASE WHEN sqlc.arg(config_version)::bigint >= config_version
                  THEN sqlc.arg(config)::jsonb ELSE config END,
    config_version = GREATEST(config_version, sqlc.arg(config_version)::bigint),
    updated_at = NOW()
WHERE org_id = sqlc.arg(org_id);

-- name: DeleteTenantHostnames :exec
DELETE FROM tenant_hostnames WHERE org_id = $1;

-- name: InsertTenantHostname :exec
INSERT INTO tenant_hostnames (hostname, org_id) VALUES ($1, $2);

-- name: SetTenantStatus :execrows
UPDATE tenants SET status = $2, updated_at = NOW() WHERE org_id = $1;
