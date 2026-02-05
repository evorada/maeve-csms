-- name: GetCertificate :one
SELECT * FROM certificates
WHERE certificate_hash = $1
LIMIT 1;

-- name: ListCertificates :many
SELECT * FROM certificates
ORDER BY created_at DESC;

-- name: SetCertificate :one
INSERT INTO certificates (
    certificate_hash,
    certificate_type,
    certificate_data
) VALUES ($1, $2, $3)
ON CONFLICT (certificate_hash) DO UPDATE
SET certificate_type = EXCLUDED.certificate_type,
    certificate_data = EXCLUDED.certificate_data
RETURNING *;

-- name: DeleteCertificate :exec
DELETE FROM certificates
WHERE certificate_hash = $1;
