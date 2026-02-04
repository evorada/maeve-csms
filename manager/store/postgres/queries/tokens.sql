-- name: GetToken :one
SELECT * FROM tokens
WHERE uid = $1 LIMIT 1;

-- name: ListTokens :many
SELECT * FROM tokens
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CreateToken :one
INSERT INTO tokens (
    country_code, party_id, type, uid, contract_id,
    visual_number, issuer, group_id, valid, language_code,
    cache_mode, last_updated
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: UpdateToken :one
UPDATE tokens
SET 
    country_code = $2,
    party_id = $3,
    type = $4,
    contract_id = $5,
    visual_number = $6,
    issuer = $7,
    group_id = $8,
    valid = $9,
    language_code = $10,
    cache_mode = $11,
    last_updated = $12,
    updated_at = NOW()
WHERE uid = $1
RETURNING *;

-- name: DeleteToken :exec
DELETE FROM tokens WHERE uid = $1;
