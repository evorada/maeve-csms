-- Auth
-- name: GetChargeStationAuth :one
SELECT * FROM charge_stations WHERE charge_station_id = $1;

-- name: SetChargeStationAuth :one
INSERT INTO charge_stations (
    charge_station_id, security_profile, base64_sha256_password, invalid_username_allowed
) VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE
SET security_profile = EXCLUDED.security_profile,
    base64_sha256_password = EXCLUDED.base64_sha256_password,
    invalid_username_allowed = EXCLUDED.invalid_username_allowed,
    updated_at = NOW()
RETURNING *;

-- Settings
-- name: GetChargeStationSettings :one
SELECT * FROM charge_station_settings WHERE charge_station_id = $1;

-- name: SetChargeStationSettings :one
INSERT INTO charge_station_settings (charge_station_id, settings)
VALUES ($1, $2)
ON CONFLICT (charge_station_id) DO UPDATE
SET settings = EXCLUDED.settings, updated_at = NOW()
RETURNING *;

-- name: ListChargeStationSettings :many
SELECT * FROM charge_station_settings
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: DeleteChargeStationSettings :exec
DELETE FROM charge_station_settings WHERE charge_station_id = $1;

-- Runtime
-- name: GetChargeStationRuntime :one
SELECT * FROM charge_station_runtime WHERE charge_station_id = $1;

-- name: SetChargeStationRuntime :one
INSERT INTO charge_station_runtime (charge_station_id, ocpp_version, vendor, model, serial_number, firmware_version)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET ocpp_version = EXCLUDED.ocpp_version,
    vendor = EXCLUDED.vendor,
    model = EXCLUDED.model,
    serial_number = EXCLUDED.serial_number,
    firmware_version = EXCLUDED.firmware_version,
    updated_at = NOW()
RETURNING *;

-- Certificates
-- name: GetChargeStationCertificates :many
SELECT * FROM charge_station_certificates 
WHERE charge_station_id = $1
ORDER BY created_at DESC;

-- name: AddChargeStationCertificate :one
INSERT INTO charge_station_certificates (charge_station_id, certificate_id, certificate_type, certificate, certificate_installation_status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateChargeStationCertificate :one
UPDATE charge_station_certificates
SET certificate = $3,
    certificate_installation_status = $4,
    send_after = $5
WHERE charge_station_id = $1 AND certificate_id = $2
RETURNING *;

-- name: DeleteChargeStationCertificates :exec
DELETE FROM charge_station_certificates WHERE charge_station_id = $1;

-- name: ListChargeStationCertificates :many
SELECT DISTINCT ON (charge_station_id) charge_station_id, certificate_id, certificate_type, certificate, certificate_installation_status, send_after, created_at
FROM charge_station_certificates
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC, created_at DESC
LIMIT $2;

-- Triggers
-- name: GetChargeStationTrigger :one
SELECT * FROM charge_station_triggers
WHERE charge_station_id = $1;

-- name: SetChargeStationTrigger :one
INSERT INTO charge_station_triggers (charge_station_id, message_type, connector_id, trigger_status, send_after)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (charge_station_id) DO UPDATE
SET message_type = EXCLUDED.message_type,
    connector_id = EXCLUDED.connector_id,
    trigger_status = EXCLUDED.trigger_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationTrigger :exec
DELETE FROM charge_station_triggers WHERE charge_station_id = $1;

-- name: ListChargeStationTriggers :many
SELECT * FROM charge_station_triggers
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Data Transfer
-- name: GetChargeStationDataTransfer :one
SELECT * FROM charge_station_data_transfer
WHERE charge_station_id = $1;

-- name: SetChargeStationDataTransfer :one
INSERT INTO charge_station_data_transfer (charge_station_id, vendor_id, message_id, data, status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET vendor_id = EXCLUDED.vendor_id,
    message_id = EXCLUDED.message_id,
    data = EXCLUDED.data,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationDataTransfer :exec
DELETE FROM charge_station_data_transfer WHERE charge_station_id = $1;

-- name: ListChargeStationDataTransfers :many
SELECT * FROM charge_station_data_transfer
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Clear Cache
-- name: GetChargeStationClearCache :one
SELECT * FROM charge_station_clear_cache
WHERE charge_station_id = $1;

-- name: SetChargeStationClearCache :one
INSERT INTO charge_station_clear_cache (charge_station_id, status, send_after)
VALUES ($1, $2, $3)
ON CONFLICT (charge_station_id) DO UPDATE
SET status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationClearCache :exec
DELETE FROM charge_station_clear_cache WHERE charge_station_id = $1;

-- name: ListChargeStationClearCaches :many
SELECT * FROM charge_station_clear_cache
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- Change Availability
-- name: GetChargeStationChangeAvailability :one
SELECT * FROM charge_station_change_availability
WHERE charge_station_id = $1;

-- name: SetChargeStationChangeAvailability :one
INSERT INTO charge_station_change_availability (charge_station_id, connector_id, evse_id, availability_type, status, send_after)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (charge_station_id) DO UPDATE
SET connector_id = EXCLUDED.connector_id,
    evse_id = EXCLUDED.evse_id,
    availability_type = EXCLUDED.availability_type,
    status = EXCLUDED.status,
    send_after = EXCLUDED.send_after,
    updated_at = NOW()
RETURNING *;

-- name: DeleteChargeStationChangeAvailability :exec
DELETE FROM charge_station_change_availability WHERE charge_station_id = $1;

-- name: ListChargeStationChangeAvailabilities :many
SELECT * FROM charge_station_change_availability
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: SetChargeStationCertificateQuery :one
INSERT INTO charge_station_certificate_queries (charge_station_id, certificate_type, query_status, send_after)
VALUES ($1, $2, $3, $4)
ON CONFLICT (charge_station_id) DO UPDATE SET
    certificate_type = EXCLUDED.certificate_type,
    query_status = EXCLUDED.query_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationCertificateQuery :exec
DELETE FROM charge_station_certificate_queries WHERE charge_station_id = $1;

-- name: LookupChargeStationCertificateQuery :one
SELECT * FROM charge_station_certificate_queries WHERE charge_station_id = $1;

-- name: ListChargeStationCertificateQueries :many
SELECT * FROM charge_station_certificate_queries
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;

-- name: SetChargeStationCertificateDeletion :one
INSERT INTO charge_station_certificate_deletions (charge_station_id, hash_algorithm, issuer_name_hash, issuer_key_hash, serial_number, deletion_status, send_after)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (charge_station_id) DO UPDATE SET
    hash_algorithm = EXCLUDED.hash_algorithm,
    issuer_name_hash = EXCLUDED.issuer_name_hash,
    issuer_key_hash = EXCLUDED.issuer_key_hash,
    serial_number = EXCLUDED.serial_number,
    deletion_status = EXCLUDED.deletion_status,
    send_after = EXCLUDED.send_after
RETURNING *;

-- name: DeleteChargeStationCertificateDeletion :exec
DELETE FROM charge_station_certificate_deletions WHERE charge_station_id = $1;

-- name: LookupChargeStationCertificateDeletion :one
SELECT * FROM charge_station_certificate_deletions WHERE charge_station_id = $1;

-- name: ListChargeStationCertificateDeletions :many
SELECT * FROM charge_station_certificate_deletions
WHERE charge_station_id > $1
ORDER BY charge_station_id ASC
LIMIT $2;
