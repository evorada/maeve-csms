-- Registrations (by token)
-- name: GetOcpiRegistration :one
SELECT * FROM ocpi_registrations
WHERE token = $1
LIMIT 1;

-- name: SetOcpiRegistration :one
INSERT INTO ocpi_registrations (token, status, org_id)
VALUES ($1, $2, $3)
ON CONFLICT (token) DO UPDATE
SET status = EXCLUDED.status,
    org_id = EXCLUDED.org_id,
    updated_at = NOW()
RETURNING *;

-- name: DeleteOcpiRegistration :exec
DELETE FROM ocpi_registrations
WHERE token = $1;

-- Party Details (by role + country_code + party_id)
-- name: GetOcpiParty :one
SELECT * FROM ocpi_parties
WHERE role = $1 AND country_code = $2 AND party_id = $3
LIMIT 1;

-- name: SetOcpiParty :one
INSERT INTO ocpi_parties (role, country_code, party_id, url, token, org_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (role, country_code, party_id) DO UPDATE
SET url = EXCLUDED.url,
    token = EXCLUDED.token,
    org_id = EXCLUDED.org_id,
    updated_at = NOW()
RETURNING *;

-- name: ListOcpiPartiesForRole :many
SELECT * FROM ocpi_parties
WHERE role = $1
ORDER BY created_at DESC;
