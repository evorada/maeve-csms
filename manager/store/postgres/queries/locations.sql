-- name: GetLocation :one
SELECT * FROM locations
WHERE id = $1
LIMIT 1;

-- name: ListLocations :many
SELECT * FROM locations
WHERE country_code = $1 AND party_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListAllLocations :many
SELECT * FROM locations
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: SetLocation :one
INSERT INTO locations (
    id,
    country_code,
    party_id,
    location_data
) VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE
SET country_code = EXCLUDED.country_code,
    party_id = EXCLUDED.party_id,
    location_data = EXCLUDED.location_data,
    updated_at = NOW()
RETURNING *;

-- name: DeleteLocation :exec
DELETE FROM locations
WHERE id = $1;
