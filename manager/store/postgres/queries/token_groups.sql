-- name: CreateTokenGroup :one
INSERT INTO token_groups (id, org_id, name, prefix_match, override_tariff_ref)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- An update of a group that does not exist returns no row.
-- name: UpdateTokenGroup :one
UPDATE token_groups SET
    name = @name,
    prefix_match = @prefix_match,
    override_tariff_ref = @override_tariff_ref,
    updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: GetTokenGroup :one
SELECT * FROM token_groups WHERE id = $1;

-- name: ListTokenGroups :many
SELECT * FROM token_groups
WHERE org_id = ANY(sqlc.arg('org_ids')::uuid[])
  AND id > sqlc.arg('after_id')::uuid
ORDER BY id
LIMIT sqlc.arg('row_limit');

-- The tokens of the group leave it (ON DELETE SET NULL).
-- name: DeleteTokenGroup :exec
DELETE FROM token_groups WHERE id = $1;

-- The tokens of the org with the UID, of every party, go into the group (NULL: into no
-- group). No row is changed when the group is not a group of the org.
-- name: SetTokenGroup :execrows
UPDATE tokens SET token_group_id = sqlc.narg('token_group_id')::uuid, updated_at = NOW()
WHERE org_id = sqlc.arg('org_id')::uuid AND uid = sqlc.arg('uid')::text
  AND (sqlc.narg('token_group_id')::uuid IS NULL
       OR EXISTS (SELECT 1 FROM token_groups g
                  WHERE g.id = sqlc.narg('token_group_id')::uuid AND g.org_id = sqlc.arg('org_id')::uuid));

-- The tokens of a group in the order in which they were stored.
-- name: ListTokenGroupMembers :many
SELECT * FROM tokens
WHERE token_group_id = sqlc.arg('token_group_id')::uuid
  AND id > sqlc.arg('after_id')::bigint
ORDER BY id
LIMIT sqlc.arg('row_limit');

-- The group of the token of the org with exactly the UID.
-- name: FindExactTokenGroup :one
SELECT g.* FROM tokens t
JOIN token_groups g ON g.id = t.token_group_id
WHERE t.org_id = sqlc.arg('org_id')::uuid AND g.org_id = sqlc.arg('org_id')::uuid
  AND t.uid = sqlc.arg('uid')::text
ORDER BY g.id
LIMIT 1;

-- The group with prefix_match that has the token with the longest UID that the presented
-- UID starts with, ignoring case. The UID of the token is matched as plain text: %, _ and
-- \ in it have no meaning. The C collation makes the order of two UIDs of the same length
-- the order of their bytes on every server.
-- name: FindPrefixTokenGroup :one
SELECT g.* FROM tokens t
JOIN token_groups g ON g.id = t.token_group_id
WHERE t.org_id = sqlc.arg('org_id')::uuid AND g.org_id = sqlc.arg('org_id')::uuid
  AND g.prefix_match
  AND t.uid <> ''
  AND upper(sqlc.arg('uid')::text) LIKE replace(replace(replace(upper(t.uid), '\', '\\'), '%', '\%'), '_', '\_') || '%'
ORDER BY length(t.uid) DESC, t.uid COLLATE "C", g.id
LIMIT 1;
