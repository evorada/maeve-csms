-- name: GetTokenAnyOrg :one
SELECT * FROM tokens
WHERE uid = $1
ORDER BY valid DESC, last_updated DESC, id DESC
LIMIT 1;

-- name: GetTokenForOrg :one
SELECT * FROM tokens
WHERE org_id = $1 AND uid = $2
ORDER BY valid DESC, last_updated DESC, id DESC
LIMIT 1;

-- name: ListTokens :many
SELECT * FROM tokens
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListTokensForOrgs :many
SELECT * FROM tokens
WHERE org_id = ANY(sqlc.arg(org_ids)::uuid[])
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: UpsertToken :one
INSERT INTO tokens (
    country_code, party_id, type, uid, contract_id,
    visual_number, issuer, group_id, valid, language_code,
    cache_mode, last_updated, expires_at, org_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT ON CONSTRAINT tokens_org_party_uid_unique DO UPDATE SET
    type = EXCLUDED.type,
    contract_id = EXCLUDED.contract_id,
    visual_number = EXCLUDED.visual_number,
    issuer = EXCLUDED.issuer,
    group_id = EXCLUDED.group_id,
    valid = EXCLUDED.valid,
    language_code = EXCLUDED.language_code,
    cache_mode = EXCLUDED.cache_mode,
    last_updated = EXCLUDED.last_updated,
    expires_at = EXCLUDED.expires_at,
    updated_at = NOW()
RETURNING *;

-- name: DeleteTokenForOrg :exec
DELETE FROM tokens
WHERE org_id = $1 AND country_code = $2 AND party_id = $3 AND uid = $4;
