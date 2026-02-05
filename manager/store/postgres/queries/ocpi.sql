-- name: GetOcpiRegistration :one
SELECT * FROM ocpi_registrations
WHERE country_code = $1 AND party_id = $2
LIMIT 1;

-- name: ListOcpiRegistrations :many
SELECT * FROM ocpi_registrations
ORDER BY created_at DESC;

-- name: SetOcpiRegistration :one
INSERT INTO ocpi_registrations (
    country_code,
    party_id,
    status,
    token,
    url
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (country_code, party_id) DO UPDATE
SET status = EXCLUDED.status,
    token = EXCLUDED.token,
    url = EXCLUDED.url,
    updated_at = NOW()
RETURNING *;

-- name: DeleteOcpiRegistration :exec
DELETE FROM ocpi_registrations
WHERE country_code = $1 AND party_id = $2;
